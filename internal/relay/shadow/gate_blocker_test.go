// gate_blocker_test.go — 闸门与 CustomPayload 屏蔽的接线契约。
//
// 单独成文件的原因：这里测的是**闸门与过滤器的接线**（记账口径、共存语义），
// 不是过滤器自身的判定逻辑（那在 custompayload_test.go）。
// 接线错了的表现是诊断数字骗人，而不是功能立刻坏 —— 需要独立的断言。
package shadow

import "testing"

// TestGateBlockerCountsOnlyOwnedFrames 记账口径：B 未持上行权时被吞的帧
// 记在闸门（swallowedB），不得同时记成「被屏蔽」——
// 否则诊断时分不清「闸门挡住了」和「屏蔽挡住了」。
func TestGateBlockerCountsOnlyOwnedFrames(t *testing.T) {
	f := NewCustomPayloadFilter(testPayloadIDs())
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	g.SetCustomPayloadBlocker(f)

	// 默认 SenderA ⇒ B 未持上行权：即便包号在屏蔽集合里，也只算闸门吞帧。
	if g.AllowB(13) {
		t.Error("B 未持上行权时 id=13 不应放行")
	}
	if sb, _ := f.Stats(); sb != 0 {
		t.Errorf("未被授权的帧不应记入屏蔽计数，实际 blockedSB=%d", sb)
	}
	if _, swB, _ := g.Stats(); swB != 1 {
		t.Errorf("swallowedB = %d，期望 1", swB)
	}

	// 切到 SenderB ⇒ 此时屏蔽才真正生效并记账。
	g.SetSender(SenderB)
	if g.AllowB(13) {
		t.Error("B 持上行权时 CustomPayload id=13 仍应被屏蔽")
	}
	if g.AllowB(2) {
		t.Error("B 持上行权时 CustomPayload id=2 仍应被屏蔽")
	}
	if !g.AllowB(0) {
		t.Error("正常游戏包应放行")
	}
	if sb, _ := f.Stats(); sb != 2 {
		t.Errorf("blockedSB = %d，期望 2", sb)
	}
	// swallowedB 是「未到达上游的帧总数」：1 帧未授权（SenderA 期间）+ 2 帧被屏蔽。
	// 它与 blockedSB 口径不同（后者只记内容屏蔽）——这正是分两个计数器的意义。
	if _, swB, _ := g.Stats(); swB != 3 {
		t.Errorf("swallowedB = %d，期望 3（1 帧未授权 + 2 帧被屏蔽）", swB)
	}
}

// TestGateWithoutBlockerBehavesAsBefore 未挂过滤器时行为与接线前完全一致
// （P1：本包协议中立，不主动屏蔽任何东西）。
func TestGateWithoutBlockerBehavesAsBefore(t *testing.T) {
	g := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	g.SetSender(SenderB)
	if !g.AllowB(13) {
		t.Error("未挂屏蔽过滤器时 id=13 应放行")
	}
	if !g.AllowB(24) {
		t.Error("未挂屏蔽过滤器时 id=24 应放行")
	}
}

// TestGateBlockerCoexistsWithContentFilter 内容过滤器与屏蔽过滤器共存：
// 两者都必须生效，且吞帧计数不重复累加（一个帧只算一次）。
func TestGateBlockerCoexistsWithContentFilter(t *testing.T) {
	// 内容过滤器只放行 id < 20；屏蔽过滤器挡 13 与 2。
	contentFilter := func(id int32) bool { return id < 20 }
	f := NewCustomPayloadFilter(testPayloadIDs())
	g := NewGate(contentFilter, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	g.SetCustomPayloadBlocker(f)
	g.SetSender(SenderB)

	if g.AllowB(13) {
		t.Error("id=13 被屏蔽过滤器挡住，应拒绝")
	}
	if g.AllowB(30) {
		t.Error("id=30 被内容过滤器挡住，应拒绝")
	}
	if !g.AllowB(5) {
		t.Error("id=5 两个过滤器都应放行")
	}

	// 三次判定吞掉两帧；屏蔽计数器只应记 id=13 这一帧。
	if _, swB, _ := g.Stats(); swB != 2 {
		t.Errorf("swallowedB = %d，期望 2", swB)
	}
	if sb, _ := f.Stats(); sb != 1 {
		t.Errorf("blockedSB = %d，期望 1（只有真正命中屏蔽集合的那帧）", sb)
	}
}
