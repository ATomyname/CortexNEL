package shadow

// leak_probe_test.go — 职责: 内存泄漏动态探针（录制器流量留存量化）。
// 2026-09-23 排查「内存泄漏」：静态与 churn 探针均未发现无界保留点后，
// 量化"会话开着跑流量时录制器到底吃多少内存"——这是影子端的固有成本
// （DefaultBudget 64MiB / 硬上限 ×4，按锚点裁剪），等比缩放到 1MiB 验证语义。

import "testing"

// TestProbeRecorderRetentionUnderTraffic 有锚点（JoinGame 在最前）时持续灌流量：
// 留存随流量增长到软预算、裁剪必须收敛在硬上限内 —— 留存不收敛即为泄漏。
func TestProbeRecorderRetentionUnderTraffic(t *testing.T) {
	const budget = 1 << 20 // 1MiB：等比验证 DefaultBudget(64MiB) 的留存语义
	const anchorID = 1

	r := NewRecorder(budget, anchorID)
	r.SetAnchor(anchorID)
	r.RecordState(FrameStatePlay, anchorID, make([]byte, 1024)) // 世界锚点（JoinGame）

	payload := make([]byte, 1024)
	const frames = 10000 // ~10MiB 流量（软预算的 10 倍）
	for i := 0; i < frames; i++ {
		r.RecordState(FrameStatePlay, 2, payload)
	}

	hard := budget * HardCapFactor
	t.Logf("灌入 ~%d KiB 流量后：录制留存 %d bytes（软预算 %d / 硬上限 %d，帧数 %d）",
		frames, r.Bytes(), budget, hard, r.Len())

	if r.Bytes() > hard {
		t.Fatalf("录制留存未收敛：%d > 硬上限 %d", r.Bytes(), hard)
	}
}
