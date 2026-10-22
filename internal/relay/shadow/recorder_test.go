package shadow

// recorder_test.go — 职责: 录制器的裁剪语义测试（两段式预算，见 recorder.go）。
// 锚点包号用测试常量：真实锚点由调用方按版本注入。

import (
	"bytes"
	"testing"
)

const testAnchorID int32 = 0x28

// TestRecorderTrimsToAnchor 软预算超限时裁剪必须回退到锚点（丢锚点之前的部分）。
func TestRecorderTrimsToAnchor(t *testing.T) {
	r := NewRecorder(64, testAnchorID)
	for i := 0; i < 3; i++ { // 锚点之前：3×16B
		r.RecordState(FrameStatePlay, 1, bytes.Repeat([]byte{byte(i)}, 16))
	}
	r.RecordState(FrameStatePlay, testAnchorID, bytes.Repeat([]byte{0xAA}, 16)) // 世界重建点
	for i := 0; i < 8; i++ {                                                    // 锚点之后：8×16B
		r.RecordState(FrameStatePlay, 2, bytes.Repeat([]byte{byte(i)}, 16))
	}

	frames := r.Snapshot()
	if len(frames) == 0 {
		t.Fatal("录制为空")
	}
	if frames[0].ID != testAnchorID {
		t.Fatalf("裁剪后首帧 id = %d，期望锚点 %d（应丢锚点之前的部分）", frames[0].ID, testAnchorID)
	}
	if !r.StartsAtAnchor() {
		t.Fatal("StartsAtAnchor 应为 true")
	}
	if r.Truncated() {
		t.Fatal("锚点序列未被截断，truncated 应为 false")
	}
}

// TestRecorderToleratesOvershootWithinHardCap 锚点序列超出软预算但在硬上限内：
// 容忍超出（不截断锚点序列），这是「宁可超预算也要能重建世界」的取舍。
func TestRecorderToleratesOvershootWithinHardCap(t *testing.T) {
	r := NewRecorder(48, testAnchorID)
	r.RecordState(FrameStatePlay, testAnchorID, bytes.Repeat([]byte{0xAA}, 16))
	for i := 0; i < 6; i++ { // 合计 7×16B = 112B > budget 48，但 ≤ hard 192
		r.RecordState(FrameStatePlay, int32(i), bytes.Repeat([]byte{byte(i)}, 16))
	}
	if r.Len() != 7 {
		t.Fatalf("硬上限内不应丢帧：len=%d，期望 7", r.Len())
	}
	if r.Truncated() {
		t.Fatal("硬上限内不应置 truncated")
	}
	if !r.StartsAtAnchor() {
		t.Fatal("StartsAtAnchor 应为 true")
	}
}

// TestRecorderFallsBackToFIFOAboveHardCap 超过硬上限（锚点序列本身不可全保）时
// 退化为 FIFO 并置 truncated。
func TestRecorderFallsBackToFIFOAboveHardCap(t *testing.T) {
	const budget = 16 // hard = 64
	r := NewRecorder(budget, testAnchorID)
	for i := 0; i < 3; i++ {
		r.RecordState(FrameStatePlay, 1, bytes.Repeat([]byte{byte(i)}, 16))
	}
	r.RecordState(FrameStatePlay, testAnchorID, bytes.Repeat([]byte{0xAA}, 16))
	for i := 0; i < 20; i++ {
		r.RecordState(FrameStatePlay, 2, bytes.Repeat([]byte{byte(i)}, 16))
	}

	if !r.Truncated() {
		t.Fatal("超过硬上限必须置 truncated")
	}
	if r.Bytes() > budget*HardCapFactor {
		t.Fatalf("超过硬上限后仍未收敛：%d > %d", r.Bytes(), budget*HardCapFactor)
	}
	frames := r.Snapshot()
	if len(frames) == 0 {
		t.Fatal("录制为空")
	}
	if frames[0].ID != 2 {
		t.Fatalf("FIFO 段首帧 id = %d，期望 2（锚点已被丢弃）", frames[0].ID)
	}
	if r.StartsAtAnchor() {
		t.Fatal("锚点已被丢弃，StartsAtAnchor 应为 false")
	}
}

// TestRecorderWithoutAnchorFallsBackToFIFO 无锚点（anchor<0）时退化为 FIFO 并标注不完整。
func TestRecorderWithoutAnchorFallsBackToFIFO(t *testing.T) {
	const budget = 48 // hard = 192
	r := NewRecorder(budget, -1)
	const total = 20
	for i := 0; i < total; i++ {
		r.RecordState(FrameStatePlay, int32(i), bytes.Repeat([]byte{byte(i)}, 16))
	}

	// 期望值由契约推出，不写死：保留帧数 = hard/帧长，首帧 id = total - 保留数。
	wantKept := budget * HardCapFactor / 16
	frames := r.Snapshot()
	if len(frames) != wantKept {
		t.Fatalf("保留帧数 = %d，期望 %d", len(frames), wantKept)
	}
	if frames[0].ID != int32(total-wantKept) {
		t.Fatalf("FIFO 裁剪后首帧 id = %d，期望 %d", frames[0].ID, total-wantKept)
	}
	if !r.Truncated() {
		t.Fatal("无锚点裁剪必须置 truncated（提示重放可能不完整）")
	}
	if r.Bytes() > budget*HardCapFactor {
		t.Fatalf("未收敛到硬上限内：%d > %d", r.Bytes(), budget*HardCapFactor)
	}
}

// TestRecorderUnderBudgetKeepsEverything 预算内一帧不丢。
func TestRecorderUnderBudgetKeepsEverything(t *testing.T) {
	r := NewRecorder(1024, testAnchorID)
	for i := 0; i < 10; i++ {
		r.RecordState(FrameStatePlay, int32(i), []byte("x"))
	}
	if r.Len() != 10 || r.Truncated() {
		t.Fatalf("预算内不应裁剪：len=%d truncated=%v", r.Len(), r.Truncated())
	}
	if r.Bytes() != 10 {
		t.Fatalf("Bytes = %d，期望 10", r.Bytes())
	}
}

func TestRecorderResetConfigurationDropsPreviousCycle(t *testing.T) {
	r := NewRecorder(1024, testAnchorID)
	r.RecordState(FrameStateConfiguration, 1, []byte("old"))
	r.ResetConfiguration()
	r.RecordState(FrameStateConfiguration, 2, []byte("new"))
	frames := r.ConfigFrames()
	if len(frames) != 1 || frames[0].ID != 2 {
		t.Fatalf("config frames after reset = %#v, want only current cycle", frames)
	}
}

// TestRecorderSnapshotIsDeepCopy 快照必须深拷贝：外部改写不得影响录制内容
// （引擎会复用底层读缓冲，不拷贝会让历史帧被后续帧改写）。
func TestRecorderSnapshotIsDeepCopy(t *testing.T) {
	r := NewRecorder(1024, testAnchorID)
	payload := []byte("hello")
	r.RecordState(FrameStatePlay, 7, payload)

	payload[0] = 'X'         // 模拟引擎复用缓冲
	snap := r.Snapshot()     // 快照
	snap[0].Payload[1] = 'Y' // 模拟调用方改写快照

	got := r.Snapshot()[0].Payload
	if string(got) != "hello" {
		t.Fatalf("录制内容被外部改写：%q", got)
	}
}

// TestRecorderSetAnchorUnblocksReplay 运行期更正锚点后，此前录到的锚点帧
// 必须被识别出来，使回灌准入成立。
//
// 这是 2026-09-17 故障的直接护栏：会话构造时协议版本还未知，只能用通配版本
// （VersionAny=-1）查表 → anchor=-1 → anchorIdx 恒为 -1 → HasAnchor 恒 false
// → 影子端回灌必然被拒（日志表现为 recorderReady=False 与
// "快照不以锚点开头"）。握手回调里用真实版本 SetAnchor 后必须转为可回灌。
func TestRecorderSetAnchorUnblocksReplay(t *testing.T) {
	// 构造期拿不到版本：沿用工位的通配语义（-1 表示未知）。
	r := NewRecorder(1024, -1)
	if r.HasAnchor() {
		t.Fatal("锚点未知时 HasAnchor 应为 false")
	}

	// 录制进行中：登录段帧 + 真实世界锚点帧 + 后续增量。
	r.RecordState(FrameStatePlay, 1, []byte("login-ish"))
	r.RecordState(FrameStatePlay, testAnchorID, []byte("joingame"))
	r.RecordState(FrameStatePlay, 2, []byte("chunk"))
	if r.HasAnchor() {
		t.Fatal("未设置锚点前不得声称可回灌")
	}

	// 握手回调：拿到真实协议版本后更正锚点。
	r.SetAnchor(testAnchorID)
	if r.Anchor() != testAnchorID {
		t.Fatalf("Anchor = %d，期望 %d", r.Anchor(), testAnchorID)
	}
	if !r.HasAnchor() {
		t.Fatal("更正锚点后必须识别到已录的锚点帧，否则回灌永远被拒")
	}
}

// TestRecorderHasAnchorFalseWhenAnchorAbsent 锚点在录制开始前就已过去时，
// SetAnchor 不得谎报可回灌（据实保持 false，由调用方决定是否重新录制）。
func TestRecorderHasAnchorFalseWhenAnchorAbsent(t *testing.T) {
	r := NewRecorder(1024, -1)
	r.RecordState(FrameStatePlay, 1, []byte("a"))
	r.RecordState(FrameStatePlay, 2, []byte("b"))

	r.SetAnchor(testAnchorID)
	if r.HasAnchor() {
		t.Fatal("缓冲里没有锚点帧时 HasAnchor 必须为 false，不得谎报可回灌")
	}
}

// TestRecorderSetAnchorNegativeStaysUnready 未知版本（-1）不得被当成"已就绪"。
func TestRecorderSetAnchorNegativeStaysUnready(t *testing.T) {
	r := NewRecorder(1024, -1)
	r.RecordState(FrameStatePlay, testAnchorID, []byte("joingame"))
	r.SetAnchor(-1)
	if r.HasAnchor() {
		t.Fatal("锚点为 -1 时 HasAnchor 必须为 false")
	}
}
