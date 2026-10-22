package telemetry

import "testing"

func TestBusPrefixSubscribeAndRing(t *testing.T) {
	bus := NewBus()
	var got []string
	cancel := bus.Subscribe("session.", func(e Event) { got = append(got, e.Name) })
	bus.Publish("session.stage", "ok", nil)
	bus.Publish("telemetry.log", "info", nil)
	bus.Publish("session.stop", "ok", nil)
	cancel()
	bus.Publish("session.start", "ok", nil)

	if len(got) != 2 {
		t.Fatalf("前缀订阅收到 %d 条, want 2: %v", len(got), got)
	}
	if len(bus.History()) != 4 {
		t.Fatalf("历史 %d 条, want 4", len(bus.History()))
	}
}

func TestBusRingCapacity(t *testing.T) {
	bus := NewBus()
	for i := 0; i < ringCapacity+10; i++ {
		bus.Publish("t.e", "ok", nil)
	}
	if len(bus.History()) != ringCapacity {
		t.Fatalf("环形历史未封顶: %d", len(bus.History()))
	}
}
