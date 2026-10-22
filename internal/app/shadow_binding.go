// shadow_binding.go — 职责: 影子端的前端绑定（M-S4）。
// 面板三动作：启动影子端（StartShadow）、状态查询（ShadowStatus）、
// 停止（StopShadow）。上行权切换（SetShadowSender）供后续接管 UI 用，
// 本期前端先不做切换入口（纯观战已可用）。
package app

import (
	"fmt"

	"nexus/internal/relay/shadow"
)

// ShadowStatusDTO 是影子面板的状态快照。
type ShadowStatusDTO struct {
	// Enabled 表示影子端组件是否可用（有活动会话即 true）。
	Enabled bool `json:"enabled"`
	// Listening 表示影子监听是否已开启。
	Listening bool `json:"listening"`
	// Addr 是影子监听地址（未开启为空串）。
	Addr string `json:"addr"`
	// Stage 是当前会话阶段（影子端只在 LIVE 后有可回灌内容）。
	Stage string `json:"stage"`
	// Sender 是当前上行权归属："A"（白端）或 "B"（影子端）。
	Sender string `json:"sender"`
	// GateStats 是闸门计数（诊断）。
	GateStats ShadowGateStatsDTO `json:"gateStats"`
	// RecorderFrames / RecorderBytes 是录制状态（诊断）。
	RecorderFrames int  `json:"recorderFrames"`
	RecorderBytes  int  `json:"recorderBytes"`
	RecorderReady  bool `json:"recorderReady"` // 快照以锚点开头（可完整回灌）
	Truncated      bool `json:"truncated"`
}

// ShadowGateStatsDTO 是上行闸门与屏蔽器的计数。
type ShadowGateStatsDTO struct {
	SwallowedA int `json:"swallowedA"`
	SwallowedB int `json:"swallowedB"`
	Fallbacks  int `json:"fallbacks"`
	BlockedSB  int `json:"blockedSB"`
	BlockedCB  int `json:"blockedCB"`
}

// StartShadow 启动影子端监听。端口缺省 25580（与 relay 端口 25565 区分）。
func (a *App) StartShadow(port int) (ShadowStatusDTO, error) {
	if port <= 0 {
		port = 25580
	}
	addr, err := a.joins.StartShadow(port)
	if err != nil {
		return ShadowStatusDTO{}, err
	}
	_ = addr
	return a.ShadowStatus()
}

// StopShadow 停止影子端监听。
func (a *App) StopShadow() (ShadowStatusDTO, error) {
	a.joins.StopShadow()
	return a.ShadowStatus()
}

// ShadowStatus 返回影子面板状态。
func (a *App) ShadowStatus() (ShadowStatusDTO, error) {
	// 影子端挂在白端会话上；没有白端会话即未启用。
	var stage string
	if run := a.joins.MirrorSessionStage(); run != "" {
		stage = run
	}
	addr := a.joins.ShadowAddr()
	dto := ShadowStatusDTO{
		Enabled:   stage != "",
		Listening: addr != "",
		Addr:      addr,
		Stage:     stage,
	}
	if snap := a.joins.ShadowDiagnostics(); snap != nil {
		dto.Sender = snap.Sender
		dto.GateStats = ShadowGateStatsDTO{
			SwallowedA: snap.SwallowedA,
			SwallowedB: snap.SwallowedB,
			Fallbacks:  snap.Fallbacks,
			BlockedSB:  snap.BlockedSB,
			BlockedCB:  snap.BlockedCB,
		}
		dto.RecorderFrames = snap.RecorderFrames
		dto.RecorderBytes = snap.RecorderBytes
		dto.RecorderReady = snap.RecorderReady
		dto.Truncated = snap.Truncated
	}
	return dto, nil
}

// SetShadowSender 切换上行权（接管开关）："A" 回白端，"B" 交影子端。
func (a *App) SetShadowSender(sender string) (ShadowStatusDTO, error) {
	switch sender {
	case "A":
		a.joins.SetShadowSender(shadow.SenderA)
	case "B":
		a.joins.SetShadowSender(shadow.SenderB)
	default:
		return ShadowStatusDTO{}, fmt.Errorf("未知上行权归属 %q（可选 A / B）", sender)
	}
	return a.ShadowStatus()
}
