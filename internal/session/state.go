// 会话状态机（架构文档 §7.1）：IDLE→ATTACH→TAP→AUTH→MIRROR→LIVE→CLOSED，异常→FAULTED。
// 迁移即发事件（规范 I2）：session.stage {from,to,connID}。
package session

import (
	"errors"
	"fmt"

	"nexus/internal/telemetry"
)

// Stage 是会话阶段。
type Stage string

const (
	StageIdle    Stage = "IDLE"
	StageAttach  Stage = "ATTACH" // 代理已监听
	StageTap     Stage = "TAP"    // 镜像客户端已接入
	StageAuth    Stage = "AUTH"   // 登录阶段（上游已接）
	StageMirror  Stage = "MIRROR" // 加密代答完成（代理持有会话）
	StageLive    Stage = "LIVE"   // 进服（Play 双向转发）
	StageClosed  Stage = "CLOSED"
	StageFaulted Stage = "FAULTED"
)

// ErrIllegalTransition 表示非法迁移。
var ErrIllegalTransition = errors.New("session: 非法阶段迁移")

var transitions = map[Stage][]Stage{
	StageIdle:    {StageAttach, StageClosed},
	StageAttach:  {StageTap, StageClosed, StageFaulted},
	StageTap:     {StageAuth, StageClosed, StageFaulted, StageAttach},
	StageAuth:    {StageMirror, StageClosed, StageFaulted, StageAttach, StageTap},
	StageMirror:  {StageLive, StageClosed, StageFaulted, StageAttach},
	StageLive:    {StageClosed, StageFaulted, StageAttach},
	StageFaulted: {StageClosed, StageAttach, StageTap},
	StageClosed:  {StageAttach, StageIdle, StageTap},
}

// Manager 持有单次会话的阶段状态（状态不外泄，规范 D4）。
type Manager struct {
	events *telemetry.Bus
	log    *telemetry.Logger

	connID string
	stage  Stage
}

// NewManager 构造（初始恒为 IDLE）。
func NewManager(connID string, events *telemetry.Bus, log *telemetry.Logger) *Manager {
	return &Manager{events: events, log: log, connID: connID, stage: StageIdle}
}

// Stage 读取当前阶段。
func (m *Manager) Stage() Stage { return m.stage }

// ConnID 返回会话连接标识。
func (m *Manager) ConnID() string { return m.connID }

// Transition 校验并执行迁移，发布 session.stage。
func (m *Manager) Transition(to Stage) error {
	from := m.stage
	if !allowed(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, from, to)
	}
	m.stage = to
	if m.events != nil {
		m.events.Publish("session.stage", "ok", map[string]any{
			"from":   string(from),
			"to":     string(to),
			"connID": m.connID,
		})
	}
	if m.log != nil {
		m.log.Info("session", "阶段迁移", "from", string(from), "to", string(to))
	}
	return nil
}

func allowed(from, to Stage) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
