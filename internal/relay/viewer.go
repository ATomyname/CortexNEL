// viewer.go — 职责: 影子腿（附加的下游输出端，walkthrough/12 §12.3）。
//
// 一条会话可以挂 N 条影子腿（本期内 1 条）：上游的 S2C 帧除发给主客户端外，
// 还按各影子腿自己的策略分流 —— 策略（缓冲直到重放完成 / 直发）由影子端决定，
// 引擎只提供通道（P1：引擎不解释策略）。
//
// 写入必须是**非阻塞**的：分流发生在腿的读取循环里，慢客户端不能拖死主链路。
// 因此每条影子腿自带出站队列与写协程；队列溢出即判定该腿不可用并关闭它。
package relay

import (
	"errors"
	"net"
	"sync"
	"time"

	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
)

// viewerQueue 是每条影子腿的出站队列容量（帧数）。溢出即关闭该腿。
const viewerQueue = 1024

// errViewerOverflow 表示影子腿出站队列溢出（该腿跟不上，已关闭）。
var errViewerOverflow = errors.New("relay: 影子腿出站队列溢出")

// splitPacket 拆出 (包 id, 载荷)（明文帧体：varint id + 载荷）。
func splitPacket(body []byte) (int32, []byte, error) {
	r := codec.NewReader(body)
	id, err := r.VarInt()
	if err != nil {
		return 0, nil, err
	}
	return id, append([]byte(nil), r.Rest()...), nil
}

// Viewer 是一条影子腿：可由影子端直接读/写帧（明文，DR-3），并接收上游 S2C 分流。
type Viewer struct {
	name string
	c    *Conn
	leg  *leg

	mu          sync.Mutex
	policy      func(id int32, payload []byte)
	interceptor func(id int32) bool
	out         chan clientInjection
	closed      chan struct{}
	writerDone  chan struct{} // writeLoop 退出后关闭（Close 等它，保证关腿后无残余写入/日志）
	once        sync.Once
	overflowed  bool
	// flushDeadline 是写操作的最长等待（0 = 不设）。回灌用它做流控观测。
	flushDeadline time.Duration
}

// AttachViewer 把一个已建立的连接作为影子腿挂到会话上。
// 返回的 Viewer 由调用方负责 Close（或由队列溢出/连接断开自动关闭）。
func (c *Conn) AttachViewer(conn net.Conn, name string) *Viewer {
	v := &Viewer{
		name:       name,
		c:          c,
		out:        make(chan clientInjection, viewerQueue),
		closed:     make(chan struct{}),
		writerDone: make(chan struct{}),
	}
	v.leg = &leg{name: name, conn: conn, dir: ClientBound, c: c}

	c.mu.Lock()
	c.viewers = append(c.viewers, v)
	c.mu.Unlock()

	go v.writeLoop()
	return v
}

// Name 返回影子腿名（日志用）。
func (v *Viewer) Name() string { return v.name }

// SetFramePolicy 设置 S2C 分流策略；nil 表示直接写该腿（默认）。
// 策略必须是**非阻塞**的：它在主链路的读取循环里被调用。
func (v *Viewer) SetFramePolicy(f func(id int32, payload []byte)) {
	v.mu.Lock()
	v.policy = f
	v.mu.Unlock()
}

// SetS2CInterceptor 设置 S2C 拦截判定；nil 表示不拦截（默认，P1 协议中立）。
//
// 返回 false 的帧被丢弃，**不会**到达该影子腿，但主客户端与录制 tap 不受影响
// —— 拦截发生在 deliver 内部（分流点），位于 afterForward 之后，
// 所以录制与主链路看到的字节流与拦截前完全一致。
//
// 用途：B 腿屏蔽 CustomPayload（mod 握手对中途接入的旁观者既无用也有害）。
// 拦截必须是**B 腿独有**的决策，因此挂在 Viewer 上而不是 dispatch 上 ——
// 挂在 dispatch 会同时影响 A 腿和录制内容。
func (v *Viewer) SetS2CInterceptor(f func(id int32) bool) {
	v.mu.Lock()
	v.interceptor = f
	v.mu.Unlock()
}

// SetCompression 启用该影子腿的压缩（收到假服务器下发的 SetCompression 之后）。
func (v *Viewer) SetCompression(threshold int32) { v.leg.enableCompression(threshold) }

// WritePacket 直接向该影子腿写一帧（重放用；引擎已带压缩与组帧）。
func (v *Viewer) WritePacket(id int32, payload []byte) error {
	return v.enqueue(clientInjection{id: id, payload: append([]byte(nil), payload...)})
}

// WritePacketBlocking 向该影子腿写一帧，队列满时**等待**而不是关闭该腿。
//
// 专供回灌使用。为什么不能复用 WritePacket：
//   - enqueue 是给主链路 fanout 用的，必须非阻塞（阻塞读取循环 = 白端被拖死）；
//   - 但回灌是一次性批量灌入（实测可达上千帧，含大量 map_chunk 大包），
//     若同样非阻塞入队，队列会在几百帧内溢出并关闭影子腿；
//   - 更关键的是：全速灌入会让 B 的接收缓冲堆积、帧被截断，
//     Netty 因此在解析中途报 DecoderException（客户端表现为"无法连接至服务器"）。
//
// 回灌跑在影子接入的独立 goroutine 里，不持有主链路读取循环，因此这里可以安全等待。
// ctx 取消或腿关闭时立即返回，不会永久阻塞。
func (v *Viewer) WritePacketBlocking(id int32, payload []byte) error {
	inj := clientInjection{id: id, payload: append([]byte(nil), payload...)}
	select {
	case <-v.closed:
		return net.ErrClosed
	default:
	}
	select {
	case v.out <- inj:
		return nil
	case <-v.closed:
		return net.ErrClosed
	}
}

// SetFlushDeadline 设置影子腿写操作的最长等待（0 = 不设）。
// 回灌用它把"对端来不及收"变成可观测的缓慢而非静默堆积。
func (v *Viewer) SetFlushDeadline(d time.Duration) {
	v.mu.Lock()
	v.flushDeadline = d
	v.mu.Unlock()
}

// FlushDeadline 返回当前写等待上限（诊断用）。
func (v *Viewer) FlushDeadline() time.Duration {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.flushDeadline
}

// ReadPacket 读出该影子腿发来的一帧（明文）。
// 只允许一个读取者（与主腿同一纪律：每方向唯一读取者）。
func (v *Viewer) ReadPacket() (int32, []byte, error) {
	body, err := readOneFrame(v.leg)
	if err != nil {
		return 0, nil, err
	}
	return splitPacket(body)
}

// Close 关闭该影子腿（幂等）并等待 writeLoop 退出。
//
// 为什么必须等：writeLoop 退出前的最后几次写失败会打日志（telemetry 句柄），
// 若 Close 不等，调用方（会话收尾/测试 TempDir 清理）可能在日志句柄仍被占用时
// 继续走完关闭流程 —— 实测表现为 Windows 上 TempDir RemoveAll 报
// "being used by another process"（约 30% 复现的 flaky 清理竞态）。
//
// 为什么等待只放在这里、不放在 enqueue 的溢出路径：deliver→enqueue 运行在
// 主链路读取循环里，等待 = 慢影子腿拖死白端。溢出路径只触发关闭（fire-and-forget）。
func (v *Viewer) Close() {
	v.once.Do(func() {
		close(v.closed)
		if v.leg != nil && v.leg.conn != nil {
			_ = v.leg.conn.Close()
		}
	})
	if v.writerDone != nil {
		<-v.writerDone
	}
}

// Closed 返回该腿是否已关闭（测试与诊断用）。
func (v *Viewer) Closed() bool {
	select {
	case <-v.closed:
		return true
	default:
		return false
	}
}

// Overflowed 表示该腿因出站队列溢出被判定不可用。
func (v *Viewer) Overflowed() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.overflowed
}

// deliver 由 dispatch 调用：按策略分流一帧 S2C；策略为 nil 时直接入队。
// 队列满 ⇒ 该腿跟不上，关闭它（关闭主链路不是选项：白端必须活着）。
func (v *Viewer) deliver(id int32, payload []byte) {
	v.mu.Lock()
	policy := v.policy
	interceptor := v.interceptor
	v.mu.Unlock()
	// 拦截先于一切分流动作：被丢弃的帧不进队列、不触发策略、不计溢出。
	if interceptor != nil && !interceptor(id) {
		return
	}
	if policy != nil {
		policy(id, payload)
		return
	}
	_ = v.enqueue(clientInjection{id: id, payload: append([]byte(nil), payload...)})
}

func (v *Viewer) enqueue(inj clientInjection) error {
	select {
	case <-v.closed:
		return net.ErrClosed
	default:
	}
	select {
	case v.out <- inj:
		return nil
	default:
		v.mu.Lock()
		v.overflowed = true
		v.mu.Unlock()
		v.c.Logger().Warn("relay", "影子腿出站队列溢出，关闭该腿", "viewer", v.name, "connID", v.c.ID)
		// 只触发关闭、不等待：enqueue 在主链路读取循环里被调用（deliver→enqueue），
		// 阻塞它 = 慢影子腿拖死白端 —— 这正是本结构存在要防的头号事故。
		// 等待写循环退出是外部收尾（Close 的调用方）的职责，见 closeAndWait。
		v.once.Do(func() {
			close(v.closed)
			if v.leg != nil && v.leg.conn != nil {
				_ = v.leg.conn.Close()
			}
		})
		return errViewerOverflow
	}
}

// writeLoop 是该影子腿唯一写者（帧原子性由 leg.writeMu 保证）；退出时关闭 writerDone。
func (v *Viewer) writeLoop() {
	defer close(v.writerDone)
	for {
		select {
		case <-v.closed:
			return
		case inj := <-v.out:
			if err := v.leg.writePacket(inj.id, inj.payload); err != nil {
				v.c.Logger().Info("relay", "影子腿写入失败", "viewer", v.name, "err", err.Error())
				// 只触发关闭、不等自己（Close 会等 writerDone，而 writerDone 要等
				// 本函数 return 才关闭 —— 等自己 = 即死锁）。
				v.once.Do(func() {
					close(v.closed)
					if v.leg != nil && v.leg.conn != nil {
						_ = v.leg.conn.Close()
					}
				})
				return
			}
		}
	}
}

// readOneFrame 从腿读取一帧并做解压（供 ReadPacket 使用）。
func readOneFrame(l *leg) ([]byte, error) {
	buf := make([]byte, 16*1024)
	for {
		body, err := l.framer.Next()
		if err == nil {
			if l.comp != nil {
				body, err = l.comp.Decode(body)
				if err != nil {
					return nil, err
				}
			}
			return body, nil
		}
		if err != stage.ErrNeedMore {
			return nil, err
		}
		n, rerr := l.conn.Read(buf)
		if n > 0 {
			if l.cipher != nil {
				l.cipher.TransformRead(buf[:n])
			}
			l.framer.Push(buf[:n])
		}
		if rerr != nil {
			return nil, rerr
		}
	}
}
