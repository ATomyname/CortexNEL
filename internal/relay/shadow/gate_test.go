package shadow

// gate_test.go — 职责: 上行闸门的所有权、过滤与 KeepAlive 兜底语义。

import (
	"testing"
	"time"
)

// fixedClock 返回可控时钟（规范 T1：时序测试确定性）。
func fixedClock(start time.Time) (now func() time.Time, advance func(time.Duration)) {
	cur := start
	return func() time.Time { return cur }, func(d time.Duration) { cur = cur.Add(d) }
}

// TestGateDefaultSenderIsA 默认由白端发包：A 直通、B 全吞。
func TestGateDefaultSenderIsA(t *testing.T) {
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	if g.Sender() != SenderA {
		t.Fatalf("默认 owner = %v，期望 A", g.Sender())
	}
	if !g.AllowA(13) {
		t.Fatal("A_SEND 下白端的帧必须放行")
	}
	if g.AllowB(13) {
		t.Fatal("A_SEND 下影子端的帧必须吞掉")
	}
	swA, swB, _ := g.Stats()
	if swA != 0 || swB != 1 {
		t.Fatalf("计数 = (%d,%d)，期望 (0,1)", swA, swB)
	}
}

// TestGateSwitchIsAtomicAndMutuallyExclusive 切换后 A/B 权限互换。
func TestGateSwitchIsAtomicAndMutuallyExclusive(t *testing.T) {
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	if !g.SetSender(SenderB) {
		t.Fatal("首次切到 B 应返回 true")
	}
	if g.SetSender(SenderB) {
		t.Fatal("重复切到同一侧应返回 false（幂等）")
	}
	if g.AllowA(1) {
		t.Fatal("B_SEND 下白端的帧必须吞掉")
	}
	if !g.AllowB(1) {
		t.Fatal("B_SEND 下影子端的帧必须放行")
	}
	if !g.SetSender(SenderA) {
		t.Fatal("切回 A 应返回 true")
	}
	if !g.AllowA(1) || g.AllowB(1) {
		t.Fatal("切回 A 后权限未互换")
	}
}

// TestGateFilterBlocksShadowFrames 过滤器在 B_SEND 下仍生效（mod 帧不上行）。
func TestGateFilterBlocksShadowFrames(t *testing.T) {
	const modFrameID int32 = 13
	g := NewGate(func(id int32) bool { return id != modFrameID }, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	g.SetSender(SenderB)

	if g.AllowB(modFrameID) {
		t.Fatal("被过滤的帧必须吞掉（brand/mod 通道不得经影子端上行）")
	}
	if !g.AllowB(42) {
		t.Fatal("未过滤的帧必须放行")
	}
	_, swB, _ := g.Stats()
	if swB != 1 {
		t.Fatalf("影子端吞帧计数 = %d，期望 1", swB)
	}
}

// TestGateKeepAliveFallback 未决 KeepAlive 超时后由闸门代答一次。
func TestGateKeepAliveFallback(t *testing.T) {
	now, advance := fixedClock(time.Unix(0, 0))
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: 0x1F, ServerBoundID: 0x0F, Timeout: 10 * time.Second})
	g.SetClock(now)

	payload := []byte{0, 0, 0, 0, 0, 0, 0, 42}
	g.ObserveKeepAlive(0x1F, payload)

	if _, ok := g.DueKeepAlive(); ok {
		t.Fatal("超时前不应代答")
	}
	advance(10 * time.Second)
	body, ok := g.DueKeepAlive()
	if !ok {
		t.Fatal("超时后必须代答")
	}
	if string(body) != string(payload) {
		t.Fatalf("代答包体 = % X，期望 % X", body, payload)
	}
	if _, again := g.DueKeepAlive(); again {
		t.Fatal("一次未决只应代答一次")
	}
	_, _, fb := g.Stats()
	if fb != 1 {
		t.Fatalf("兜底计数 = %d，期望 1", fb)
	}
}

// TestGateClientKeepAliveClearsPending 下游及时回应则不再代答。
func TestGateClientKeepAliveClearsPending(t *testing.T) {
	now, advance := fixedClock(time.Unix(0, 0))
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: 0x1F, ServerBoundID: 0x0F, Timeout: 10 * time.Second})
	g.SetClock(now)

	g.ObserveKeepAlive(0x1F, []byte{1, 2, 3})
	g.ObserveClientKeepAlive(0x0F)
	advance(time.Minute)

	if _, ok := g.DueKeepAlive(); ok {
		t.Fatal("下游已回应，不应代答")
	}
}

// TestGateKeepAliveDisabledWithoutIDs 未注入包号时不启用兜底（安全默认：
// 不知道包号就不要乱发帧，宁可依赖下游正常回应）。
func TestGateKeepAliveDisabledWithoutIDs(t *testing.T) {
	now, advance := fixedClock(time.Unix(0, 0))
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	g.SetClock(now)

	g.ObserveKeepAlive(0x1F, []byte{1})
	advance(time.Minute)
	if _, ok := g.DueKeepAlive(); ok {
		t.Fatal("未注入包号时不得代答")
	}
}

// TestGateSwitchClearsPending 切换所有权后，旧一侧的未决 KeepAlive 不再触发代答。
func TestGateSwitchClearsPending(t *testing.T) {
	now, advance := fixedClock(time.Unix(0, 0))
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: 0x1F, ServerBoundID: 0x0F, Timeout: 10 * time.Second})
	g.SetClock(now)

	g.ObserveKeepAlive(0x1F, []byte{1})
	g.SetSender(SenderB)
	advance(time.Minute)

	if _, ok := g.DueKeepAlive(); ok {
		t.Fatal("切换后旧未决状态应被清除，不得代答")
	}
}
