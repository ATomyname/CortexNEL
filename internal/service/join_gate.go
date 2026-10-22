// join_gate.go — 会话准入与身份分配：决定"能不能再起一个代理"、"它叫什么"。
//
// 与 join_game.go 的分界：那边管"一个会话怎么跑"，这边管"谁有资格起、叫什么"。
//
// 多代理模型（2026-09-21 用户确认）：
//   - 仅代理模式（JoinProxyOnly）可任意多开 —— 每个代理独占自己的 relay 端口，
//     互不影响；
//   - 一键进服（自动拉白端）同时只允许一个 —— 白端要占控制通道端口、要起一个
//     javaw 进程，多开没有意义，因此单独留一个白端位；
//   - 同一服务器、同一账号都允许多个代理，UI 只做管理不做拦截。
package service

import (
	"errors"
	"fmt"
)

// ErrMirrorAlreadyRunning 表示已有一个自动拉起白端的会话在运行。
var ErrMirrorAlreadyRunning = errors.New("已有一个正在自动进服的会话，请先停止它")

// ErrDuplicateRole 表示同一服务器上已有相同角色的代理在运行。
// 同一个角色同时接两条代理没有意义（服务端会把后一条踢掉），
// 且用户看到两个"同名角色"也无法区分哪个是自己的。
var ErrDuplicateRole = errors.New("该服务器上已有相同角色的代理在运行，请先停止它")

// findDuplicateRun 找 (serverID, roleName) 均相同的运行中会话；没有则 nil。
// 调用方需持锁或本函数自持锁（二选一，本函数自持）。
func (s *JoinService) findDuplicateRun(serverID, roleName string) *joinRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, run := range s.runs {
		if run.serverID == serverID && run.roleName == roleName {
			return run
		}
	}
	return nil
}

// acquireJoinSlot 占一个会话席位，返回会话 ID 与释放函数。
//
// sessionID 用自增序号而不是服务器 ID：同一服务器允许起多个代理，
// 用服务器 ID 会撞号，事件与状态会串到同一个键上。
// release 必须由调用方 defer：失败路径不释放会让白端位永久占着。
func (s *JoinService) acquireJoinSlot(launchMirror bool) (sessionID string, release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if launchMirror && s.mirrorSession != "" {
		return "", nil, ErrMirrorAlreadyRunning
	}
	s.sessionSeq++
	id := fmt.Sprintf("sess-%d", s.sessionSeq)
	if launchMirror {
		s.mirrorSession = id
	}
	return id, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if launchMirror && s.mirrorSession == id {
			s.mirrorSession = ""
		}
	}, nil
}

// mirrorRun 返回当前的白端会话（最多一个）；没有则返回 nil。
// 影子端挂在白端会话上——它围绕白端的官方 mod 上报设计。
func (s *JoinService) mirrorRun() *joinRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mirrorSession == "" {
		return nil
	}
	return s.runs[s.mirrorSession]
}

// MirrorSessionStage 返回白端会话的阶段（无白端会话时空串）。
// 供影子面板判断"有没有可挂靠的白端会话"。
func (s *JoinService) MirrorSessionStage() string {
	run := s.mirrorRun()
	if run == nil {
		return ""
	}
	return string(run.manager.Stage())
}
