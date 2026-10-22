package app

import (
	"testing"
	"time"

	"nexus/internal/telemetry"
)

// TestEventPayloadCarriesTimestamp 前端载荷契约（bus.ts StageEvent/LostEvent 的 ts）：
// 生产者未带 ts 时由桥补总线时间戳，保证时间轴可用。
func TestEventPayloadCarriesTimestamp(t *testing.T) {
	ts := time.Date(2026, 9, 6, 10, 0, 0, 0, time.Local)
	got := eventPayload(telemetry.Event{
		TS:     ts,
		Name:   "session.stage",
		Result: "ok",
		Fields: map[string]any{"from": "IDLE", "to": "ATTACH", "connID": "c1"},
	})
	if got["ts"] != ts {
		t.Fatalf("ts = %v, 期望总线时间戳 %v", got["ts"], ts)
	}
	if got["from"] != "IDLE" || got["connID"] != "c1" {
		t.Fatalf("fields 未透传：%v", got)
	}
}

// TestEventPayloadPreservesProducerTS 生产者自带 ts 时（telemetry.log）不得覆盖。
func TestEventPayloadPreservesProducerTS(t *testing.T) {
	producerTS := "2026-09-06T10:00:00+08:00"
	got := eventPayload(telemetry.Event{
		TS:     time.Now(),
		Name:   "telemetry.log",
		Result: "info",
		Fields: map[string]any{"ts": producerTS, "module": "service", "message": "ok"},
	})
	if got["ts"] != producerTS {
		t.Fatalf("生产者 ts 被覆盖：%v", got["ts"])
	}
}

// TestEventPayloadCopiesFields 载荷必须是 Fields 的拷贝：桥不得向总线环形历史
// 持有的同一 map 追加键（与 History 读并发）。
func TestEventPayloadCopiesFields(t *testing.T) {
	fields := map[string]any{"connID": "c1"}
	got := eventPayload(telemetry.Event{TS: time.Now(), Fields: fields})
	if _, mutated := fields["ts"]; mutated {
		t.Fatal("原始 Fields 被写入 ts，存在并发写风险")
	}
	if got["connID"] != "c1" {
		t.Fatalf("fields 拷贝缺失 connID：%v", got)
	}
}
