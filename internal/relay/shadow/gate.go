// gate.go — 职责: 影子上行闸门（哪一个下游的 C2S 能到达上游）与 KeepAlive 兜底。
//
// 语义（walkthrough/12 §12.5）：
//
//	A_SEND（默认）—— 主客户端（白端）的 C2S 直通；影子端的 C2S 全部吞掉（纯观战）。
//	B_SEND        —— 主客户端的 C2S 全部吞掉；影子端的 C2S 经过滤器后到达上游。
//
// 为什么必须互斥：两个下游同时发包会让上游看到互相矛盾的玩家状态（位置/交互冲突），
// 而服务器只认一条连接 —— 它不是「两个人」，是同一角色在自相矛盾。
//
// 为什么需要 KeepAlive 兜底：上游会周期性发 KeepAlive 并要求回应；接管切换的瞬间
// 或影子端配置不当（如 B 被过滤规则误伤）时可能没人回应 ⇒ 上游判定超时踢人。
// 闸门在「已把 KeepAlive 交给某一侧、但迟迟没收到回应」时代为回一个，保证会话不因
// 切换而断。代答的包号由调用方注入（本包不硬编码版本知识，P1）。
package shadow

import (
	"sync"
	"time"
)

// Sender 表示当前拥有上行权的下游。
type Sender int

const (
	// SenderA 是主客户端（白端）发包。
	SenderA Sender = iota
	// SenderB 是影子端发包。
	SenderB
)

// String 便于日志与事件字段。
func (s Sender) String() string {
	if s == SenderB {
		return "B"
	}
	return "A"
}

// KeepAliveConfig 是 KeepAlive 兜底所需的版本知识（由调用方注入）。
type KeepAliveConfig struct {
	// ClientBoundID 是上游发来的 KeepAlive 包号；<0 表示不启用兜底。
	ClientBoundID int32
	// ServerBoundID 是回给上游的 KeepAlive 包号；<0 表示不启用兜底。
	ServerBoundID int32
	// Timeout 是「已交给下游但未收到回应」的容忍时长；<=0 时用 DefaultKeepAliveTimeout。
	Timeout time.Duration
}

// DefaultKeepAliveTimeout 是 KeepAlive 兜底触发的默认阈值。
const DefaultKeepAliveTimeout = 10 * time.Second

// Filter 判定某条 C2S 是否允许从**影子端**上行；nil = 全放行。
// 典型实现放在服务器适配包里（品牌/FML/自定义通道应吞，原版动作应放行）。
type Filter func(id int32) bool

// Gate 是上行闸门。零值可用（默认 SenderA、无过滤、不启用 KeepAlive 兜底）。
type Gate struct {
	mu     sync.Mutex
	sender Sender
	filter Filter
	ka     KeepAliveConfig
	now    func() time.Time

	// blocker 是 CustomPayload 屏蔽过滤器；nil = 不启用（协议中立：本包默认不屏蔽）。
	blocker *CustomPayloadFilter

	// pendingKA 是「最后一次交给下游的 KeepAlive 包体」与其交付时刻。
	// 只在对应下游**未回应**且超过 Timeout 时使用（见 DueKeepAlive）。
	pendingKA   []byte
	pendingAt   time.Time
	pendingFrom Sender
	hasPending  bool

	// 计数（诊断与测试断言）。
	swallowedA int
	swallowedB int
	fallbacks  int
}

// NewGate 构造闸门。
func NewGate(filter Filter, ka KeepAliveConfig) *Gate {
	if ka.Timeout <= 0 {
		ka.Timeout = DefaultKeepAliveTimeout
	}
	return &Gate{filter: filter, ka: ka, now: time.Now}
}

// Sender 返回当前拥有上行权的下游。
func (g *Gate) Sender() Sender {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sender
}

// SetSender 切换上行权（原子）。返回是否发生了切换。
func (g *Gate) SetSender(s Sender) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sender == s {
		return false
	}
	g.sender = s
	// 切换后上一侧的 KeepAlive 未回应不再算数（新的一侧接手）。
	g.hasPending = false
	return true
}

// SetKeepAliveIDs 在运行期更正 KeepAlive 包号（客户端协议版本握手后才可知）。
//
// 构造期调用方只能拿到通配版本的 (-1,-1)，使兜底静默失效；握手回调里用真实
// 版本更正后兜底才真正生效。传入 <0 表示该方向不启用兜底。
func (g *Gate) SetKeepAliveIDs(clientBound, serverBound int32) {
	g.mu.Lock()
	g.ka.ClientBoundID = clientBound
	g.ka.ServerBoundID = serverBound
	// 包号变了，此前记录的未决观测不再对应新语义。
	g.hasPending = false
	g.mu.Unlock()
}

// AllowA 判定主客户端的 C2S 帧是否放行（可直接赋给 relay.Conn.C2SGate）。
func (g *Gate) AllowA(id int32) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sender != SenderA {
		g.swallowedA++
		return false
	}
	return true
}

// AllowB 判定影子端的 C2S 帧是否放行（先过 Filter，再判所有权）。
func (g *Gate) AllowB(id int32) bool {
	g.mu.Lock()
	filter := g.filter
	blocker := g.blocker
	owner := g.sender == SenderB
	g.mu.Unlock()

	if !owner {
		g.mu.Lock()
		g.swallowedB++
		g.mu.Unlock()
		return false
	}
	if filter != nil && !filter(id) {
		g.mu.Lock()
		g.swallowedB++
		g.mu.Unlock()
		return false
	}
	// CustomPayload 屏蔽：与内容过滤共用同一吞帧账，但屏蔽侧单独记账，
	// 这样诊断时能分清「闸门挡住了」与「屏蔽挡住了」。
	if blocker != nil && !blocker.AllowServerBound(id) {
		g.mu.Lock()
		g.swallowedB++
		g.mu.Unlock()
		blocker.noteServerBoundBlocked()
		return false
	}
	return true
}

// SetCustomPayloadBlocker 挂上 CustomPayload 屏蔽过滤器。
//
// 为什么放在闸门里而不是让调用方自己包 Filter：所有权判定与内容过滤必须在
// **同一个临界路径**上完成，否则 B 未持上行权时被吞的帧也会被记成「被屏蔽」，
// 让诊断数字失去意义（分不清「闸门挡住了」和「屏蔽挡住了」）。
func (g *Gate) SetCustomPayloadBlocker(f *CustomPayloadFilter) {
	g.mu.Lock()
	g.blocker = f
	g.mu.Unlock()
}

// ObserveKeepAlive 登记「刚把一条上游 KeepAlive 交给了下游」。
// 由 S2C 分流侧调用（hub 收到 server→client 的 KeepAlive 时）。
func (g *Gate) ObserveKeepAlive(id int32, payload []byte) {
	if g.ka.ClientBoundID < 0 || id != g.ka.ClientBoundID {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pendingKA = append([]byte(nil), payload...)
	g.pendingAt = g.now()
	g.pendingFrom = g.sender
	g.hasPending = true
}

// ObserveClientKeepAlive 登记「下游回应了 KeepAlive」：清掉未决状态。
func (g *Gate) ObserveClientKeepAlive(id int32) {
	if g.ka.ServerBoundID < 0 || id != g.ka.ServerBoundID {
		return
	}
	g.mu.Lock()
	g.hasPending = false
	g.mu.Unlock()
}

// DueKeepAlive 返回需要由闸门代为回给上游的 KeepAlive 包体；无则返回 false。
//
// 触发条件：存在未决 KeepAlive 且已超过 Timeout。返回后未决状态被清除
// （一次未决只代答一次，避免持续替下游发包）。
func (g *Gate) DueKeepAlive() ([]byte, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.hasPending || g.ka.ServerBoundID < 0 {
		return nil, false
	}
	if g.now().Sub(g.pendingAt) < g.ka.Timeout {
		return nil, false
	}
	body := g.pendingKA
	g.hasPending = false
	g.fallbacks++
	return append([]byte(nil), body...), true
}

// Stats 返回 (吞掉的主端帧数, 吞掉的影子端帧数, 兜底代答次数)。
func (g *Gate) Stats() (swallowedA, swallowedB, fallbacks int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.swallowedA, g.swallowedB, g.fallbacks
}

// SetClock 注入时钟（规范 T1：时序测试必须确定性）。
func (g *Gate) SetClock(now func() time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now != nil {
		g.now = now
	}
}
