// session_lifecycle.go — 会话生命周期管理：停止单个/全部、列出快照。
//
// 与 join_game.go 的分界：那边管"一个会话怎么起、怎么跑"，
// 这边管"已经跑起来的会话怎么收场、怎么被看见"。
// 多代理下这两件事的调用模式完全不同（起是互斥的，停/看是并发的），
// 拆开也让 join_game.go 保持在行数禁线内。
package service

import (
	"errors"
	"sort"
	"time"

	"nexus/internal/authsock"
	"nexus/internal/clientcontrol"
	"nexus/internal/session"
)

// Stop 停止指定会话（幂等）。多代理下每次只停一个，其余不受影响。
func (s *JoinService) Stop(sessionID string) error {
	s.mu.Lock()
	run := s.runs[sessionID]
	delete(s.runs, sessionID)
	if s.mirrorSession == sessionID {
		s.mirrorSession = ""
	}
	// control 由 mirror_launch.go 在锁内写入，这里也在锁内取出，
	// 避免依赖「谁先拿到锁」的时序推断。
	var control *clientcontrol.Listener
	var auth *authsock.Listener
	if run != nil {
		control = run.control
		run.control = nil
		auth = run.auth
		run.auth = nil
	}
	s.mu.Unlock()
	if run == nil {
		return ErrNoSession
	}
	// 逐字段判空：run 的组件是随装配进度逐步到位的（启动中途失败的会话
	// 可能只有部分字段），StopAll 会遍历全部 run，任一项为 nil 都不能 panic。
	if run.cancel != nil {
		run.cancel()
	}
	if run.interceptor != nil {
		run.interceptor.Stop()
	}
	if control != nil {
		if err := control.Close(); err != nil {
			s.log.Warn("service", "控制通道关闭失败", "err", err.Error())
		}
	}
	if auth != nil {
		if err := auth.Close(); err != nil {
			s.log.Warn("service", "鉴权通道关闭失败", "err", err.Error())
		}
	}
	// 只有白端会话才拉起过镜像客户端；仅代理会话调它是空转。
	if run.launchMirror {
		if err := s.launcher.Stop(); err != nil {
			s.log.Warn("service", "镜像客户端停止失败", "err", err.Error())
		}
	}
	if run.manager != nil {
		if err := run.manager.Transition(session.StageClosed); err != nil {
			s.log.Warn("service", "阶段迁移告警", "err", err.Error())
		}
	}
	s.events.Publish("session.stop", "ok", map[string]any{
		"sessionID":  run.connID,
		"reason":     "user_stop",
		"durationMs": time.Since(run.startedAt).Milliseconds(),
	})
	return nil
}

// StopAll 停止全部会话（应用退出用）。逐个 Stop，单个失败不阻断其余。
func (s *JoinService) StopAll() error {
	s.mu.Lock()
	ids := make([]string, 0, len(s.runs))
	for id := range s.runs {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	var firstErr error
	for _, id := range ids {
		if err := s.Stop(id); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// StopAccountSessions 停止指定账号启动的全部代理会话。
// 会话可能刚好被其他入口同时停止，此时 ErrNoSession 视为已完成清理。
func (s *JoinService) StopAccountSessions(accountID string) error {
	s.mu.Lock()
	ids := make([]string, 0)
	for id, run := range s.runs {
		if run != nil && run.accountID == accountID {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()

	var firstErr error
	for _, id := range ids {
		if err := s.Stop(id); err != nil && !errors.Is(err, ErrNoSession) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ListSessions 返回全部运行中会话的快照（代理管理列表数据源）。
// 按启动顺序返回，前端无需再排序。
func (s *JoinService) ListSessions() []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SessionInfo, 0, len(s.runs))
	for _, run := range s.runs {
		info := SessionInfo{
			SessionID:    run.connID,
			ServerID:     run.serverID,
			AccountID:    run.accountID,
			RoleName:     run.roleName,
			LaunchMirror: run.launchMirror,
			StartedAt:    run.startedAt,
		}
		// 同样逐字段判空：装配中的会话可能还没有 interceptor/manager。
		if run.interceptor != nil {
			info.ListenAddr = run.interceptor.Addr().String()
		}
		if run.manager != nil {
			info.Stage = string(run.manager.Stage())
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out
}
