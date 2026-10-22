// Conn：单连接双 leg 状态 + 会话回调。状态迁移只经本包方法（规范 D4）。
package relay

import (
	"fmt"
	"net"
	"sync"

	"nexus/internal/telemetry"
)

// Conn 承载镜像客户端（client leg）与上游（server leg）。
// 回调由会话编排层（internal/session）在 Run 前设置。
type Conn struct {
	ID              string
	GameID          string
	UpstreamAddr    string
	LoginName       string
	ProtocolVersion int32

	// OnHandshake 在握手包处理后被调用（TAP→AUTH 迁移由编排层决定）。
	OnHandshake func(c *Conn, nextState State)
	// OnStateChange 在 MC 连接状态迁移后被调用（HANDSHAKE/LOGIN/PLAY…）。
	// 会话阶段事件（session.stage）由会话层统一发布（EVENTS.md §1 单源），
	// 引擎只做回调转发，不直接占用事件名。
	OnStateChange func(c *Conn, from, to State)
	// OnLoginSuccess 在 LoginSuccess 后被调用（AUTH→LIVE）。
	OnLoginSuccess func(c *Conn)
	// OnDisconnect 在任一腿关闭时被调用一次。
	OnDisconnect func(c *Conn, err error)
	// Joiner 认证挂接（§6.6）。
	Joiner Joiner
	// RelayAddr 是客户端可达的本机回连地址（如 "127.0.0.1:25565"），由
	// Interceptor 装配时按客户端实际拨号的本地地址写入。Transfer 处理器用它
	// 改写转服目标，让客户端转服后仍回到本代理；空 = 不改写（只记录）。
	RelayAddr string
	// MirrorClient 标记本连接是镜像客户端（网易白端）会话。白端原生完成
	// 服务器侧事件上报（s2cevent 通道），服务器伪装（伪造上报/重放）只对
	// 仅代理会话（用户自备客户端）有意义 —— 处理器据此门控。false = 仅代理（默认）。
	MirrorClient bool
	// OnTransferRequest 在上游发来 Transfer 包（要求客户端转服）时被调用，
	// addr 是清洗过 FML 标记后的真实目标 "host:port"。由编排层经 Acceptance
	// 提供并记录，供客户端回连时改指新上游；nil = 仅改写包不记录。
	OnTransferRequest func(c *Conn, addr string)

	// OnClientBoundFrame 在每帧**成功转发 client 腿之后**被调用（Seam-1：影子录制 tap）。
	// 收到的 payload 是最终写出字节（含处理器改写）；被 cancel 的包不会触发。
	// 回调只读事实：需要保留时必须自行拷贝，且不得阻塞（它在腿的读取循环里执行）。
	// nil = 未启用（现状行为，零开销）。
	OnClientBoundFrame func(c *Conn, id int32, payload []byte)
	// OnClientBoundFrameState 与 OnClientBoundFrame 相同，但额外给出该包
	// 被处理时的协议态（状态处理器可能在转发前切换 Conn.State）。
	OnClientBoundFrameState func(c *Conn, state State, id int32, payload []byte)
	// C2SGate 在 registry 匹配**之前**被查询（Seam-2：影子上行闸门）。
	// 返回 false 时该 C2S 帧被吞：不转发、不进注册表。引擎只执行回调、不解释策略
	// （策略在 relay/shadow/gate.go）。nil = 全放行（现状行为，零开销）。
	C2SGate func(id int32) bool
	// OnCompression 在 SetCompression 协商完成时被调用（Seam-1 的伴生信息）。
	// 影子端据此用同一阈值重新编码回灌帧，否则帧边界错乱。
	// nil = 不通知（现状行为，零开销）。
	OnCompression func(c *Conn, threshold int32)

	registry *Registry
	events   *telemetry.Bus
	log      *telemetry.Logger

	mu     sync.Mutex
	state  State
	client *leg
	server *leg
	// viewers 是影子腿（walkthrough/12 §12.3）：上游 S2C 除主客户端外按各自的
	// 策略分流给它们；主腿与影子腿的写路径互不影响（各自队列 + 写协程）。
	viewers []*Viewer
	// pendingClientCompression：SetCompression 转发完成后待启用的 client 腿阈值。
	// 该包自身必须以未压缩帧格式转发（客户端处理后才理解压缩帧格式）。
	pendingClientCompression int32
	hasPendingClientComp     bool
	// Client/server read loops can observe configuration finish/ack in either
	// order. Keep the Play transition behind an explicit protocol barrier.
	configFinishSent bool
	configAckPending bool
	closeOnce        sync.Once
	closed           chan struct{}
	// lostLeg/lostErr 由 closeFromLeg 在 closeOnce 内写入（先于 OnDisconnect）：
	// "client"/"server" = 对应腿读取终止；"" = 生命周期或协议路径关闭，无来源事实。
	lostLeg string
	lostErr error
	// pendingInject 是排队待注入客户端腿的包：注入必须排在「当前正在转发的包」之后
	// （资源包推送要等 JoinGame 先到客户端，否则客户端还没进 play 态）。
	pendingInject []clientInjection
	// curPacketID 是 dispatch 正在处理的包 id（只读探针/日志用，随包更新）。
	curPacketID int32
}

// CurrentPacketID 返回 dispatch 当前正在处理的包 id。
//
// 存在的理由：处理器只拿到包体，通配注册（PacketID=-1）时无法从注册键得知真实 id；
// 诊断"到底哪个包带了某段内容"必须知道 id，否则只能靠猜包号。
func (c *Conn) CurrentPacketID() int32 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.curPacketID
}

// clientInjection 是一条待发往客户端腿的包。
type clientInjection struct {
	id      int32
	payload []byte
}

// QueueClientInjection 排入一条将在「下一次成功转发之后」发往客户端的包。
//
// 处理器在 Handle 里调用：此时 dispatch 还没把当前包发给客户端，因此直接写会得到
// 相反的顺序。队列由 dispatch 在转发成功后统一 drain（见 drainClientInjections）。
func (c *Conn) QueueClientInjection(id int32, payload []byte) {
	c.mu.Lock()
	c.pendingInject = append(c.pendingInject, clientInjection{id: id, payload: append([]byte(nil), payload...)})
	c.mu.Unlock()
}

// WriteToClient 立即向客户端腿写一帧（不走上游、不排队）。
//
// 与 QueueClientInjection 的区别：那条要等当前包成功转发后才发（用于"排在其后"
// 的注入）；本方法用于**不回上游**的本地应答 —— 典型是状态查询（server list ping），
// 它必须在当前帧被 cancel 的同时把应答写回客户端，否则客户端只会看到连接被关。
//
// 帧格式按 client 腿当前状态编码（明文腿通常无压缩/加密；若该腿已启用压缩，
// 由 leg.writePacket 统一处理）。
func (c *Conn) WriteToClient(id int32, payload []byte) error {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return fmt.Errorf("relay: 客户端腿未装配")
	}
	return client.writePacket(id, payload)
}

// WriteToServer 直接向上游腿写一帧（影子端接管上行时使用）。
// payload 是已解码的包体；server leg 负责重新压缩/加密和组帧。
func (c *Conn) WriteToServer(id int32, payload []byte) error {
	c.mu.Lock()
	server := c.server
	c.mu.Unlock()
	if server == nil {
		return fmt.Errorf("relay: 上游腿未装配")
	}
	return server.writePacket(id, payload)
}

// CloseClientLeg 关闭客户端腿（用于本地应答完毕后结束该连接）。
func (c *Conn) CloseClientLeg() {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client != nil && client.conn != nil {
		_ = client.conn.Close()
	}
}

// drainClientInjections 把排队的注入包依次发往客户端腿。
func (c *Conn) drainClientInjections() {
	c.mu.Lock()
	queued := c.pendingInject
	c.pendingInject = nil
	client := c.client
	c.mu.Unlock()
	if len(queued) == 0 || client == nil {
		return
	}
	for _, inj := range queued {
		if err := client.writePacket(inj.id, inj.payload); err != nil {
			c.Logger().Warn("relay", "注入包发送失败", "connID", c.ID, "packetID", inj.id, "err", err.Error())
			c.Close()
			return
		}
	}
}

// NewConn 构造连接（由 Interceptor 装配双腿后调用）。
func NewConn(id string, registry *Registry, events *telemetry.Bus, log *telemetry.Logger) *Conn {
	return &Conn{
		ID:       id,
		registry: registry,
		events:   events,
		log:      log,
		state:    StateHandshake,
		closed:   make(chan struct{}),
	}
}

// SetLegsForTest 装配双腿（测试专用；生产路径由 Interceptor.handleClient 装配）。
// 供 shadow 包等外部消费者的回环测试使用 —— AttachViewer 只依赖 viewers 列表
// 与 logger，不需要真实 Interceptor 装配流程。
func (c *Conn) SetLegsForTest(client net.Conn, server net.Conn) {
	c.client = &leg{name: "client", conn: client, dir: ServerBound, c: c}
	c.server = &leg{name: "server", conn: server, dir: ClientBound, c: c}
}

// State 读取当前状态。
func (c *Conn) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// setState 迁移状态并回调编排层（事件发布归会话层，EVENTS.md §1）。
func (c *Conn) setState(to State) {
	c.mu.Lock()
	from := c.state
	c.state = to
	c.mu.Unlock()
	if c.OnStateChange != nil {
		c.OnStateChange(c, from, to)
	}
}

// PublishError 发布协议域错误事件（EVENTS.md §4）。
func (c *Conn) PublishError(name string, fields map[string]any) {
	if c.events != nil {
		c.events.Publish(name, "fail", fields)
	}
}

// Logger 返回结构化日志器。
func (c *Conn) Logger() *telemetry.Logger { return c.log }

// Close 关闭双腿（幂等）。生命周期与协议路径的关闭不携带断开来源。
func (c *Conn) Close() {
	c.closeFromLeg("", nil)
}

// closeFromLeg 关闭双腿并记录来源腿（幂等，先到先得）。
// 事件命名仍归编排层：这里只透传事实（哪条腿、什么错误），不发事件。
func (c *Conn) closeFromLeg(legName string, err error) {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.lostLeg = legName
		c.lostErr = err
		c.mu.Unlock()
		close(c.closed)
		if c.client != nil {
			_ = c.client.conn.Close()
		}
		if c.server != nil {
			_ = c.server.conn.Close()
		}
		if c.OnDisconnect != nil {
			c.OnDisconnect(c, err)
		}
	})
}

// LostLeg 返回断开来源腿（"client"/"server"）与读取错误；空串表示生命周期或
// 协议路径的关闭，没有"哪条腿先断"的事实。
func (c *Conn) LostLeg() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lostLeg, c.lostErr
}

// Done 返回关闭信号。
func (c *Conn) Done() <-chan struct{} { return c.closed }

func stateName(s State) string {
	switch s {
	case StateHandshake:
		return "HANDSHAKE"
	case StateStatus:
		return "STATUS"
	case StateLogin:
		return "LOGIN"
	case StatePlay:
		return "PLAY"
	case StateConfiguration:
		return "CONFIGURATION"
	default:
		return "UNKNOWN"
	}
}
