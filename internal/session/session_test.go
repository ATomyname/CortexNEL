package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexus/internal/telemetry"
)

func TestTransitionTable(t *testing.T) {
	bus := telemetry.NewBus()
	logger, err := telemetry.New(bus, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	var stages [][2]string
	bus.Subscribe("session.", func(e telemetry.Event) {
		if e.Name == "session.stage" {
			stages = append(stages, [2]string{
				e.Fields["from"].(string), e.Fields["to"].(string),
			})
		}
	})

	m := NewManager("conn-1", bus, logger)
	for _, to := range []Stage{StageAttach, StageTap, StageAuth, StageMirror, StageLive, StageClosed} {
		if err := m.Transition(to); err != nil {
			t.Fatalf("迁移 %s: %v", to, err)
		}
	}
	if m.Stage() != StageClosed {
		t.Fatalf("终态 = %s", m.Stage())
	}
	if len(stages) != 6 || stages[0] != [2]string{"IDLE", "ATTACH"} {
		t.Fatalf("事件序列 = %v", stages)
	}
}

func TestIllegalTransition(t *testing.T) {
	m := NewManager("conn-1", telemetry.NewBus(), nil)
	if err := m.Transition(StageLive); !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("IDLE→LIVE 应非法: %v", err)
	}
}

// fakeSleeper 记录退避序列（T1：无 sleep 依赖）。
type fakeSleeper struct {
	durations []time.Duration
}

func (f *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	f.durations = append(f.durations, d)
	return nil
}

func TestReconnectBackoffSequence(t *testing.T) {
	sleeper := &fakeSleeper{}
	calls := 0
	err := ReconnectWithBackoff(context.Background(), sleeper, func(context.Context) error {
		calls++
		if calls <= 2 {
			return errors.New("暂时失败")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("第 3 次（上限内）应成功: %v", err)
	}
	// 两次失败 → 两次等待 = 1s→2s。
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(sleeper.durations) != 2 {
		t.Fatalf("等待次数 = %d", len(sleeper.durations))
	}
	for i, d := range want {
		if sleeper.durations[i] != d {
			t.Fatalf("退避[%d] = %v, want %v", i, sleeper.durations[i], d)
		}
	}
}

func TestReconnectGivesUpAfterMax(t *testing.T) {
	sleeper := &fakeSleeper{}
	err := ReconnectWithBackoff(context.Background(), sleeper, func(context.Context) error {
		return errors.New("一直失败")
	})
	if err == nil {
		t.Fatal("应最终失败")
	}
	if len(sleeper.durations) != MaxReconnectAttempts-1 {
		t.Fatalf("等待次数 = %d, want %d", len(sleeper.durations), MaxReconnectAttempts-1)
	}
}

func TestClassifyStatus(t *testing.T) {
	if d := ClassifyStatus(0xff); !d.AllowRelogin {
		t.Fatal("0xff 应允许重登")
	}
	for _, s := range []byte{0x00, 0x11, 0x10, 0x42} {
		if d := ClassifyStatus(s); d.AllowRelogin {
			t.Fatalf("0x%02X 不应允许重登", s)
		}
	}
}
