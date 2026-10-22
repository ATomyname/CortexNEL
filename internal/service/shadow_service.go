// shadow_service.go — 职责: 影子端监听的生命周期编排（service 层对 shadow 包的唯一接缝）。
//
// StartShadow 在会话 LIVE 前后任何时刻都可调用：监听独立端口，B 接入时才真正
// 挂到主会话连接上（此时若主会话还没进 play，回灌会被空快照拒绝，B 收到失败 ——
// 主会话不受影响）。Stop 随会话 Stop 一并触发。
package service

import (
	"fmt"

	"nexus/internal/relay"
	"nexus/internal/relay/shadow"
)

// StartShadow 启动影子端监听（独立端口，不复用 relay 端口 —— 见 walkthrough/15）。
// 返回实际监听地址。无白端会话时报错；重复调用返回已监听地址（幂等）。
//
// 影子端挂在白端会话上（mirrorRun）：它围绕白端的官方 mod 上报设计，
// 仅代理会话没有可复用的上报面。
func (s *JoinService) StartShadow(preferredPort int) (string, error) {
	run := s.mirrorRun()
	if run == nil {
		return "", ErrNoSession
	}
	if run.shadow == nil {
		// 会话在影子端关闭（系统设置）下启动：不装配录制组件，回灌无源可放。
		// 设置仅对新会话生效，故提示重新进服而不是当场翻开关。
		return "", fmt.Errorf("影子端未启用：请先在系统设置中开启影子端，再重新进服（设置仅对新会话生效）")
	}
	rt := run.shadow

	// 入口留痕：这条链路的失败点集中在「监听是否真的起来」，
	// 先记下参数与幂等判定结果，后续每步都有对应日志可对照。
	s.log.Info("service", "开始开启影子端", "preferredPort", preferredPort)

	// 幂等：已在监听则直接返回地址。
	// listener 初值为 nil（首次调用尚未构造），必须先判空再取地址 ——
	// 直接在 nil 上调用会 panic，而 Wails 绑定的 panic 会让前端 Promise
	// 永不 settle，表现为「点了开启一直转圈」（2026-09-17 实测故障）。
	if rt.listener != nil {
		if addr := rt.listener.AddrIfListening(); addr != "" {
			s.log.Info("service", "影子端已在监听，幂等返回", "addr", addr)
			return addr, nil
		}
	}

	// attach 回调：B 接入时由 shadow 包调用，取白端会话的连接。
	newListener := shadow.NewShadowListener(func() (*shadow.Attachment, error) {
		conn := s.activeConn()
		if conn == nil {
			return nil, fmt.Errorf("影子接入被拒: 当前无活动会话连接")
		}
		return &shadow.Attachment{
			Conn:           conn,
			Recorder:       rt.recorder,
			Gate:           rt.gate,
			PayloadFilter:  rt.filter,
			AnchorPacketID: rt.anchorID,
			KeepAliveIDs:   rt.kaIDs,
			Log:            s.log,
		}, nil
	})
	// 先构造再写回：写回后 StopShadow/ShadowAddr 才能看到这个实例。
	// 注入日志器：accept 阶段还没有 Attachment，拿不到它的 Log ——
	// 「影子端收到接入」这一行只能由监听器自己记。
	newListener.SetLogger(s.log)
	rt.listener = newListener

	bindHost := "127.0.0.1"
	s.mu.Lock()
	if s.allowLAN {
		bindHost = "0.0.0.0"
	}
	s.mu.Unlock()

	addr := fmt.Sprintf("%s:%d", bindHost, preferredPort)
	listenAddr, err := newListener.Listen(addr)
	if err != nil {
		// 首选端口被占 → 系统分配。这一行必须留痕：否则用户按首选端口去连
		// 会连不上，而日志里看不出实际端口已经变了。
		s.log.Warn("service", "影子端首选端口不可用，回落系统分配端口",
			"preferred", addr, "err", err.Error())
		listenAddr, err = newListener.Listen(fmt.Sprintf("%s:0", bindHost))
		if err != nil {
			s.log.Error("service", "影子端监听失败", "bindHost", bindHost, "err", err.Error())
			return "", fmt.Errorf("影子端监听失败: %w", err)
		}
	}

	go func() {
		if err := newListener.Serve(); err != nil {
			s.log.Warn("service", "影子监听退出", "err", err.Error())
		}
	}()

	s.events.Publish("shadow.start", "ok", map[string]any{
		"listen": listenAddr,
	})
	s.log.Info("service", "影子端已监听（独立端口）", "addr", listenAddr, "bindHost", bindHost)
	return listenAddr, nil
}

// StopShadow 停止影子监听并断开影子腿（幂等；无监听时静默成功）。
func (s *JoinService) StopShadow() {
	run := s.mirrorRun()
	if run == nil || run.shadow == nil || run.shadow.listener == nil {
		return
	}
	run.shadow.listener.Stop()
	s.events.Publish("shadow.stop", "ok", map[string]any{})
	s.log.Info("service", "影子端已停止")
}

// ShadowAddr 返回影子监听地址（未启用返回空串）。
func (s *JoinService) ShadowAddr() string {
	run := s.mirrorRun()
	if run == nil || run.shadow == nil {
		return ""
	}
	return run.shadow.listener.AddrIfListening()
}

// ShadowDiagnostics 是影子面板的状态快照（app 层 DTO 的数据源）。
type ShadowDiagnostics struct {
	Sender         string
	SwallowedA     int
	SwallowedB     int
	Fallbacks      int
	BlockedSB      int
	BlockedCB      int
	RecorderFrames int
	RecorderBytes  int
	RecorderReady  bool
	Truncated      bool
}

// ShadowDiagnostics 返回影子组件的诊断快照（无白端会话/未装配返回 nil）。
func (s *JoinService) ShadowDiagnostics() *ShadowDiagnostics {
	run := s.mirrorRun()
	if run == nil || run.shadow == nil {
		return nil
	}
	rt := run.shadow
	d := &ShadowDiagnostics{Sender: "A"}
	if rt.gate != nil {
		swA, swB, fb := rt.gate.Stats()
		d.Sender = rt.gate.Sender().String()
		d.SwallowedA = swA
		d.SwallowedB = swB
		d.Fallbacks = fb
	}
	if rt.filter != nil {
		d.BlockedSB, d.BlockedCB = rt.filter.Stats()
	}
	if rt.recorder != nil {
		d.RecorderFrames = rt.recorder.Len()
		d.RecorderBytes = rt.recorder.Bytes()
		d.RecorderReady = rt.recorder.HasAnchor()
		d.Truncated = rt.recorder.Truncated()
	}
	return d
}

// SetShadowSender 切换上行权（"A"=白端发包 / "B"=影子端接管）。
func (s *JoinService) SetShadowSender(sender shadow.Sender) {
	run := s.mirrorRun()
	if run == nil || run.shadow == nil || run.shadow.gate == nil {
		return
	}
	if changed := run.shadow.gate.SetSender(sender); changed {
		s.events.Publish("shadow.sender", "ok", map[string]any{
			"sender": sender.String(),
		})
		s.log.Info("service", "影子上行权切换", "sender", sender.String())
	}
}

// activeConn 返回白端会话的 relay 连接（影子 attach 回调用）。
//
// 本函数由影子监听的 Serve goroutine 调用：那里的 panic 会带走整个进程，
// 与「任何一步失败只断 B、不影响白端」的承诺矛盾，因此对 nil 一律返回 nil
// （调用方以「当前无活动会话连接」拒绝 B），而不是解引用。
func (s *JoinService) activeConn() *relay.Conn {
	run := s.mirrorRun()
	if run == nil || run.interceptor == nil {
		return nil
	}
	return run.interceptor.ConnByID(run.connID)
}
