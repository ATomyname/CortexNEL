// loopback 监听与会话接受（架构文档 §6.1）：镜像客户端 → 127.0.0.1:<localPort> → 上游。
package relay

import (
	"context"
	"net"
	"sync"
	"time"

	"nexus/internal/telemetry"
)

// Acceptance 是会话编排层对一次接受的处理结果。
type Acceptance struct {
	ID           string
	UpstreamAddr string
	GameID       string
	LoginName    string
	Joiner       Joiner

	OnHandshake    func(c *Conn, nextState State)
	OnStateChange  func(c *Conn, from, to State)
	OnLoginSuccess func(c *Conn)
	OnDisconnect   func(c *Conn, err error)

	// Seam-1 录制 tap / Seam-2 上行闸门（语义见 Conn 字段文档）。
	OnClientBoundFrame      func(c *Conn, id int32, payload []byte)
	OnClientBoundFrameState func(c *Conn, state State, id int32, payload []byte)
	C2SGate                 func(id int32) bool

	// OnCompression 在 SetCompression 协商完成时回调（threshold < 0 表示未启用）。
	// 影子端需要它：录制内容是解压后的帧，回灌给影子腿时必须按同一阈值重新编码，
	// 否则 B 会按未压缩格式解析压缩帧并报 DecoderException（2026-09-17 实测）。
	OnCompression func(c *Conn, threshold int32)

	// OnTransferRequest 在上游发来 Transfer 包（要求客户端转服）时被调用，
	// addr 是清洗过 FML 标记后的真实目标 "host:port"。编排层记录后，在客户端
	// 回连本监听口时改指新上游（转服跟随）；nil = 不记录，转服跟随失效。
	OnTransferRequest func(c *Conn, addr string)

	// MirrorClient 标记接入的客户端是镜像客户端（网易白端）：服务器伪装类
	// 处理（s2cevent 伪造上报等）对白端不生效，仅代理会话才启用。
	MirrorClient bool
}

// AcceptHook 由 internal/service 提供：校验可服务性并返回会话参数。
// 返回 error 时客户端连接被立即关闭。
type AcceptHook func(client net.Conn) (*Acceptance, error)

// Interceptor 监听 loopback 并装配双腿。
type Interceptor struct {
	ListenAddr string
	Registry   *Registry
	Events     *telemetry.Bus
	Log        *telemetry.Logger
	Accept     AcceptHook

	mu     sync.Mutex
	conns  map[string]*Conn
	ln     net.Listener
	stopCh chan struct{}
}

// NewInterceptor 构造。
func NewInterceptor(listenAddr string, rg *Registry, events *telemetry.Bus, log *telemetry.Logger, accept AcceptHook) *Interceptor {
	return &Interceptor{
		ListenAddr: listenAddr,
		Registry:   rg,
		Events:     events,
		Log:        log,
		Accept:     accept,
		conns:      make(map[string]*Conn),
		stopCh:     make(chan struct{}),
	}
}

// ListenAndServe 阻塞监听直到 ctx 取消或出错。
func (i *Interceptor) ListenAndServe(ctx context.Context) error {
	ln, err := net.Listen("tcp", i.ListenAddr)
	if err != nil {
		return err
	}
	i.mu.Lock()
	i.ln = ln
	i.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	for {
		client, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		select {
		case <-ctx.Done():
			_ = client.Close()
			return nil
		default:
		}
		go i.handleClient(client)
	}
}

// Addr 返回实际监听地址（端口 0 时用于探测）。
func (i *Interceptor) Addr() net.Addr {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ln == nil {
		return nil
	}
	return i.ln.Addr()
}

// Stop 关闭监听与全部活动连接。
func (i *Interceptor) Stop() {
	close(i.stopCh)
	i.mu.Lock()
	ln := i.ln
	conns := i.conns
	i.conns = make(map[string]*Conn)
	i.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		c.Close()
	}
}

// ConnByID 返回指定会话的活动连接（无则 nil）。
// 影子端接入编排需要拿到主会话连接来挂 Viewer（shadow_listener）；
// 读锁查询，不改变生命周期。
func (i *Interceptor) ConnByID(id string) *Conn {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.conns[id]
}

func (i *Interceptor) handleClient(client net.Conn) {
	acc, err := i.Accept(client)
	if err != nil {
		i.Log.Warn("relay", "拒绝连接", "err", err.Error())
		_ = client.Close()
		return
	}

	conn := NewConn(acc.ID, i.Registry, i.Events, i.Log)
	conn.GameID = acc.GameID
	conn.UpstreamAddr = acc.UpstreamAddr
	conn.LoginName = acc.LoginName
	conn.Joiner = acc.Joiner
	// 回连通告地址取客户端实际拨号的本地地址（loopback 会话即 127.0.0.1:<port>，
	// LAN 会话即其所拨的本机 IP）——转服改写后的目标必然可达。
	conn.RelayAddr = client.LocalAddr().String()
	conn.OnTransferRequest = acc.OnTransferRequest
	conn.MirrorClient = acc.MirrorClient
	conn.OnHandshake = acc.OnHandshake
	conn.OnStateChange = acc.OnStateChange
	conn.OnLoginSuccess = acc.OnLoginSuccess
	conn.OnClientBoundFrame = acc.OnClientBoundFrame
	conn.OnClientBoundFrameState = acc.OnClientBoundFrameState
	conn.C2SGate = acc.C2SGate
	conn.OnCompression = acc.OnCompression
	conn.OnDisconnect = func(c *Conn, e error) {
		i.mu.Lock()
		delete(i.conns, c.ID)
		i.mu.Unlock()
		if acc.OnDisconnect != nil {
			acc.OnDisconnect(c, e)
		}
	}

	upstream, err := net.DialTimeout("tcp", acc.UpstreamAddr, 10*time.Second)
	if err != nil {
		i.Log.Warn("relay", "上游连接失败", "addr", acc.UpstreamAddr, "err", err.Error())
		_ = client.Close()
		return
	}

	conn.client = &leg{name: "client", conn: client, dir: ServerBound, c: conn}
	conn.server = &leg{name: "server", conn: upstream, dir: ClientBound, c: conn}

	i.mu.Lock()
	// 同一 connID 已有存活会话时不覆盖：新连接直接丢弃。
	// 覆盖会让正在 LIVE 的会话从 conns 表里消失（影子端/编排层随即失去
	// activeConn），而旧连接本身还在转发——状态与实际彻底脱节。
	// 实测场景：网易白端进服后自发第二次 Forge 登录连接，把 LIVE 会话顶掉
	// （2026-09-18 12:07:57 实测：LIVE→TAP→FAULTED，影子端随之全拒）。
	if existing := i.conns[acc.ID]; existing != nil {
		i.mu.Unlock()
		i.Log.Warn("relay", "拒绝重复装配（会话已存在）",
			"connID", acc.ID, "upstream", acc.UpstreamAddr)
		_ = client.Close()
		_ = upstream.Close()
		return
	}
	i.conns[acc.ID] = conn
	i.mu.Unlock()

	i.Log.Info("relay", "会话装配完成", "connID", acc.ID, "upstream", acc.UpstreamAddr)
	go conn.client.readLoop()
	go conn.server.readLoop()
}
