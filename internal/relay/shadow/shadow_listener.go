// shadow_listener.go — 职责: 影子端独立监听 + B 接入编排（M-S2 接线）。
//
// 为什么独立监听（walkthrough/15 硬缺口 1）：relay 端口每次 accept 都开新上游
// （interceptor.go handleClient），第二个客户端接 relay 端口 = 第二个上游会话 =
// 同账号重复 join ⇒ 被踢。影子端必须有自己的监听，accept 到的连接**不**开上游，
// 而是作为 Viewer 挂到既有会话上。
//
// 编排时序（单会话绑定；多会话留 seam 不做）：
//
//	accept B → FakeServer.ServeLogin（B 进 play 入口）
//	  → conn.AttachViewer(B)（挂上即开始 fanout —— 先于回灌，保证无缝衔接）
//	  → 挂 CustomPayload 屏蔽（S2C 拦截）+ 上行闸门（C2S）
//	  → Replayer.Replay(recorder.Snapshot())（回灌世界基准 + 已积压增量）
//	  → B 处于 LIVE 镜像：之后 fanout 自然供帧
//
// 并发纪律：同一会话同时只允许一条影子腿（本期 1:N 中的 N=1）。
package shadow

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"nexus/internal/relay"
	"nexus/internal/telemetry"
)

// Attachment 是影子接入编排的输入：一个已 LIVE 的主会话 + 影子语义组件。
// 由 service 层装配（service 持有 JoinService 的会话状态）。
type Attachment struct {
	// Conn 是主会话的 relay 连接（白端 A 所在）。
	Conn *relay.Conn
	// Recorder 是主会话的 S2C 录制器（Seam-1 已在录制）。
	Recorder *Recorder
	// Gate 是上行闸门（C2S 归属判定 + KeepAlive 兜底）。可 nil：纯观战且不兜底。
	Gate *Gate
	// PayloadFilter 是 CustomPayload 屏蔽器。nil = 不屏蔽。
	PayloadFilter *CustomPayloadFilter
	// AnchorPacketID 是录制锚点（763 JoinGame=0x28 等，由适配包提供）。
	AnchorPacketID int32
	// KeepAliveIDs 是 (S2C, C2S) KeepAlive 包号；<0 关闭兜底。
	KeepAliveIDs [2]int32
	// Log
	Log *telemetry.Logger
}

// ShadowListener 是影子端独立监听器。
type ShadowListener struct {
	attach func() (*Attachment, error)

	// log 在 accept 阶段就开始记录（此时还没有 Attachment，拿不到它的 Log）。
	// 由构造方注入；nil 时静默（测试与无日志场景）。
	log Logger

	mu     sync.Mutex
	ln     net.Listener
	closed bool
	viewer *relay.Viewer // 当前影子腿（本期单腿）
	// uplinkSwallowed 是被吞掉的 B 上行帧数（纯观战留痕，诊断用）。
	uplinkSwallowed int
}

// Logger 是本包所需的最小日志能力，避免反向依赖 telemetry。
type Logger interface {
	Info(module, message string, kv ...any)
	Warn(module, message string, kv ...any)
}

// NewShadowListener 构造。attach 在每次 B 接入时被调用，
// 返回当前应绑定的主会话组件；返回错误则 B 被拒绝。
func NewShadowListener(attach func() (*Attachment, error)) *ShadowListener {
	return &ShadowListener{attach: attach}
}

// SetLogger 注入日志器（可选；nil 表示静默）。必须在 Listen 之前调用。
func (s *ShadowListener) SetLogger(log Logger) {
	s.mu.Lock()
	s.log = log
	s.mu.Unlock()
}

// Serve 阻塞监听直到 Stop。每 accept 一个 B 走一次编排；本期同时只服务一条。
//
// 注意：handleB 是同步调用（不是 go）—— 一个连上却不发握手的 B 会让本循环
// 停止接受新连接，最长 HandshakeTimeout（默认 30s）后才恢复。
func (s *ShadowListener) Serve() error {
	for {
		s.mu.Lock()
		ln := s.ln
		closed := s.closed
		log := s.log
		s.mu.Unlock()
		if closed || ln == nil {
			return nil
		}
		conn, err := ln.Accept()
		if err != nil {
			if closed {
				return nil
			}
			return fmt.Errorf("shadow: accept: %w", err)
		}
		if log != nil {
			log.Info("shadow", "影子端收到接入", "remote", conn.RemoteAddr().String())
		}
		s.handleB(conn)
	}
}

// Listen 开始监听（不阻塞），返回实际地址。
func (s *ShadowListener) Listen(addr string) (string, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("shadow: 监听 %s: %w", addr, err)
	}
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	return ln.Addr().String(), nil
}

// AddrIfListening 返回当前监听地址；未监听返回空串（幂等判定的依据）。
func (s *ShadowListener) AddrIfListening() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Stop 关闭监听与当前影子腿（幂等）。
func (s *ShadowListener) Stop() {
	s.mu.Lock()
	s.closed = true
	ln := s.ln
	v := s.viewer
	s.ln = nil
	s.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if v != nil {
		v.Close()
	}
}

// handleB 单条 B 连接的完整接入编排。任何一步失败都断开 B 并留痕，
// 主会话（白端 A）不受影响 —— 这是不变量：影子端永不拖死主链路。
//
// 每一步都留痕：B 接入是本功能唯一的入口，失败原因必须能从日志直接读出
// （2026-09-17 的教训是「转圈时日志一片空白」，定位只能靠读代码）。
func (s *ShadowListener) handleB(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()

	s.mu.Lock()
	stageLog := s.log
	s.mu.Unlock()

	att, err := s.attach()
	if err != nil {
		// attach 失败时 att 可能为 nil，回落到监听器自身的日志器。
		if att != nil && att.Log != nil {
			att.Log.Warn("shadow", "拒绝影子接入", "remote", remote, "err", err.Error())
		} else if stageLog != nil {
			stageLog.Warn("shadow", "拒绝影子接入", "remote", remote, "err", err.Error())
		}
		return
	}
	if att.Log != nil {
		att.Log.Info("shadow", "影子接入已装配，开始伪造登录",
			"remote", remote, "anchor", att.AnchorPacketID,
			"recorded", att.Recorder.Len(), "recorderReady", att.Recorder.HasAnchor(),
			"anchor", att.Recorder.Anchor())
	}

	// 1. 伪造登录（离线形态）。ServeLogin 内含 HandshakeTimeout（默认 30s）：
	//    B 连上却不发握手会在这里超时，日志会明确写出 i/o timeout。
	//
	// 压缩阈值必须与来源会话一致：录制内容来自 A 的真实会话，A 腿在
	// EnableCompression 之后是按压缩格式收发的；影子腿若用不同阈值，
	// 回灌出去的帧在 B 侧会帧边界错乱（表现为 DecoderException）。
	compress := att.Recorder.Compression()
	fs := NewFakeServer(FakeServerConfig{Shape: ShapeOfflineB, Compression: compress})
	att.Log.Info("shadow", "影子登录将按来源会话的压缩阈值协商",
		"remote", remote, "compression", compress)
	res, err := fs.ServeLogin(conn)
	if err != nil {
		// 状态查询（server list ping）是官方客户端连接前的正常一步，
		// 已应答并结束本连接 —— 客户端随后会另开连接做真正的登录。
		// 记 info 而不是 warn：它不是失败。
		if errors.Is(err, errStatusQueryAnswered) {
			att.Log.Info("shadow", "已应答 B 的状态查询（客户端随后会另开连接登录）",
				"remote", remote)
			return
		}
		att.Log.Warn("shadow", "B 登录段失败（B 未按协议发握手或形态不符）",
			"remote", remote, "err", err.Error())
		return
	}
	// FakeServer 登录阶段使用临时读写 deadline 防止握手卡死；进入 LIVE
	// 后该连接是长期会话，必须清除 deadline，否则恰好 10 秒后回灌写入会
	// 失败（Windows 报 i/o timeout，B 侧只看到“连接中断”）。
	if err := conn.SetDeadline(time.Time{}); err != nil {
		att.Log.Warn("shadow", "清除 B 登录阶段 deadline 失败",
			"remote", remote, "err", err.Error())
		return
	}
	att.Log.Info("shadow", "B 登录段完成",
		"remote", remote,
		"name", res.LoginName, "effective", res.Effective,
		"protocol", res.Handshake.ProtocolVersion)

	// 2. 挂影子腿。挂上即开始 fanout（先于回灌 —— 无缝衔接的前提）。
	v := att.Conn.AttachViewer(conn, "shadow-"+res.LoginName)
	defer v.Close()

	// 影子腿的压缩必须与伪造登录时协商的阈值一致，否则回灌出去的帧
	// 在 B 侧会帧边界错乱（B 按未压缩格式解析压缩帧）。
	// 这里必须显式启用：Viewer 是独立于主 client 腿的写路径，
	// dispatch 的 applyPendingClientCompression 只作用于主腿。
	if res.Compression >= 0 {
		v.SetCompression(res.Compression)
	}

	s.mu.Lock()
	s.viewer = v
	s.mu.Unlock()
	att.Log.Info("shadow", "影子腿已挂上，开始回灌",
		"name", res.LoginName, "viewer", v.Name(), "compression", res.Compression)

	// 2.5 B 腿下行策略（packetctl 的 enqueue 分发语义）：回灌期间 fanout 的
	//     增量帧缓冲；回灌完成 + 缓冲放行后**直接入队**（policy 置 nil），
	//     B 进入实时镜像——此后的帧不需要也不应该再缓冲。
	var liveBuf syncBuffer
	v.SetFramePolicy(func(id int32, payload []byte) {
		liveBuf.push(id, payload)
	})

	// 2.6 S2C CustomPayload 屏蔽（B 腿独有；录制与主链路不受影响）。
	if att.PayloadFilter != nil {
		v.SetS2CInterceptor(att.PayloadFilter.AllowClientBound)
	}

	// 3. C2S 上行泵（packetctl：B 上行 readFrame 后 pop，全吞）：
	//    B 是纯观战——LoginAcknowledged / acknowledge_finish_configuration
	//    是对假服务器流的自发应答，注入主会话会污染白端。ack 到达时发信号
	//    让回灌循环在 finish 后继续灌 play 段。
	ackNotify := make(chan struct{})
	bDone := make(chan struct{})
	go s.pumpBUpstream(att, v, ackNotify, bDone)

	// 4. 回灌（packetctl handleMirrorClient 时序）：
	//    config 帧 → finish → 等 B 的 acknowledge → play 快照 → 代际校验。
	//    同时挂与 LIVE 阶段同源的 S2C 屏蔽。
	rep := NewReplayerWithLog(v, att.Log)
	if att.PayloadFilter != nil {
		rep.SetFrameFilter(att.PayloadFilter.AllowClientBound)
	}
	rep.SetFinishConfigHandler(func() {
		// 等 B 的 acknowledge_finish_configuration（最多 10s；Vanilla 语义上
		// ack 必到，超时只是容错）。
		select {
		case <-ackNotify:
			att.Log.Info("shadow", "已收到 B 的 configuration acknowledge，继续回灌 play 段")
		case <-time.After(10 * time.Second):
			att.Log.Warn("shadow", "等待 B 的 configuration acknowledge 超时，继续回灌")
		}
	})
	genAtAttach := att.Recorder.Generation()
	configFrames := att.Recorder.ConfigFrames()
	finishIndex := -1
	for i, f := range configFrames {
		if f.ID == idConfigFinishMarker {
			finishIndex = i
			break
		}
	}
	att.Log.Info("shadow", "影子回灌配置段诊断",
		"configFrames", len(configFrames), "finishIndex", finishIndex,
		"compression", att.Recorder.Compression())
	if res.Handshake.ProtocolVersion >= 764 && finishIndex < 0 {
		att.Log.Warn("shadow", "配置段缺少 finish_configuration，拒绝灌入 play 段",
			"protocol", res.Handshake.ProtocolVersion, "configFrames", len(configFrames))
		return
	}
	n, err := rep.Replay(configFrames, att.Recorder.Snapshot(), att.Recorder)
	if err != nil {
		att.Log.Warn("shadow", "回灌失败，断开影子腿",
			"replayed", n, "hasAnchor", att.Recorder.HasAnchor(),
			"anchor", att.Recorder.Anchor(), "generationAtAttach", genAtAttach,
			"err", err.Error())
		return
	}
	// 代际校验（packetctl：MirrorSession 入列前比对 generation）——回灌期间
	// 白端若重进世界（ResetPlay），快照已失真，B 必须重连而不是留在旧画面。
	if att.Recorder.Generation() != genAtAttach {
		att.Log.Warn("shadow", "世界代际已变（白端重进世界），断开 B 促其重连",
			"generationAtAttach", genAtAttach, "generationNow", att.Recorder.Generation())
		return
	}
	// 回灌完成：把回灌期间缓冲的 LIVE 增量帧按序放行（排在回灌字节之后），
	// 然后解除缓冲策略——此后的 fanout 帧直接进出站队列，B 进入实时镜像。
	// 不解除的话增量帧会永远积压在缓冲里，B 的世界冻结且缓冲无限增长。
	drained := liveBuf.drain(func(id int32, payload []byte) {
		if err := v.WritePacketBlocking(id, payload); err != nil {
			att.Log.Warn("shadow", "缓冲增量放行失败", "packetID", id, "err", err.Error())
		}
	})
	v.SetFramePolicy(nil)
	att.Log.Info("shadow", "回灌完成，影子腿进入 LIVE 镜像",
		"replayed", n, "bufferedLive", drained, "truncated", att.Recorder.Truncated())
	// LIVE 不是接入流程的结束：保持 B 连接存活，直到 B 真正断开或被 Stop 关闭。
	<-bDone
}

// syncBuffer 是回灌期间的 LIVE 增量缓冲（fanout 策略写入，回灌完成后放行）。
type syncBuffer struct {
	mu     sync.Mutex
	ids    []int32
	bodies [][]byte
}

func (b *syncBuffer) push(id int32, payload []byte) {
	b.mu.Lock()
	b.ids = append(b.ids, id)
	b.bodies = append(b.bodies, append([]byte(nil), payload...))
	b.mu.Unlock()
}

// drain 按序把缓冲帧交给 emit，返回放行帧数；之后缓冲清空。
func (b *syncBuffer) drain(emit func(id int32, payload []byte)) int {
	b.mu.Lock()
	ids, bodies := b.ids, b.bodies
	b.ids, b.bodies = nil, nil
	b.mu.Unlock()
	for i, id := range ids {
		emit(id, bodies[i])
	}
	return len(ids)
}

// pumpBUpstream 是 B 的上行泵（packetctl 语义：readFrame 后 pop，全吞）。
//
// B 是纯观战：它的上行帧（LoginAcknowledged、acknowledge_finish_configuration、
// KeepAlive 应答、动作输入……）一律不进入主会话——其中登录/配置态的应答
// 是对假服务器流的自发回执，注入会污染白端；动作输入则会让上游看到
// "同一角色自相矛盾"的状态。acknowledge 到达时发信号让回灌循环继续。
//
// B 断开即退出。
func (s *ShadowListener) pumpBUpstream(att *Attachment, v *relay.Viewer, ackNotify chan<- struct{}, bDone chan<- struct{}) {
	defer close(bDone)
	notified := false
	for {
		id, payload, err := v.ReadPacket()
		if err != nil {
			return // B 断开
		}
		// config acknowledge（C2S 0x03，空载荷）：B 完成 config 阶段的信号。
		// 只认第一次（其后 play 段的 0x03=animation 是正常游戏包）。
		if !notified && id == 0x03 {
			notified = true
			if att.Log != nil {
				att.Log.Info("shadow", "B 已回 acknowledge_finish_configuration", "packetID", id)
			}
			select {
			case ackNotify <- struct{}{}:
			default:
			}
			// 这是对假服务器的 configuration 回执，不得转发给真实上游。
			s.mu.Lock()
			s.uplinkSwallowed++
			s.mu.Unlock()
			continue
		}
		// 登录/configuration 阶段的回执只属于假服务器，不能注入主会话。
		// 进入 play 后，B_SEND 才把帧交给主会话闸门；默认 A_SEND 仍全部吞掉。
		if notified && att.Gate != nil && att.Gate.AllowB(id) {
			if err := att.Conn.WriteToServer(id, payload); err != nil {
				if att.Log != nil {
					att.Log.Warn("shadow", "B 上行转发失败", "packetID", id, "err", err.Error())
				}
				return
			}
			continue
		}
		// 纯观战：全部吞掉（计数留痕，诊断时可见 B 是否还在发数据）。
		s.mu.Lock()
		s.uplinkSwallowed++
		s.mu.Unlock()
	}
}
