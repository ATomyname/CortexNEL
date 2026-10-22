package relay

// leak_probe_test.go — 职责: 内存泄漏动态探针（帧流量 churn 的堆回落测量）。
// 2026-09-23 排查「内存泄漏」：验证热路径（framer→解压→派发→转发）高吞吐后
// 存活堆是否回落到基线 —— 回落=只有 GC 抖动，不回落=存在保留点（再上 pprof）。
// 断言只用 goroutine 数；堆数字只记录不判定（T1/T2 纪律）。

import (
	"io"
	"runtime"
	"testing"
	"time"
)

// heapSnapshot 连做两次 GC 后取存活堆与 goroutine 数（探针辅助）。
func heapSnapshot(t *testing.T) (int, uint64) {
	t.Helper()
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return runtime.NumGoroutine(), m.HeapAlloc
}

// pumpFrames 在独立夹具连接上注入 n 帧 S2C 流量：写 toServer（模拟上游），
// server 腿 readLoop 走 framer→解压→派发→转发 client 腿，toClient 常驻排空
// （net.Pipe 无缓冲，不排空会阻塞写方）。收尾关腿让 readLoop 退出，不留悬挂。
func pumpFrames(t *testing.T, n int, payload []byte) {
	t.Helper()
	s := newSeamConn(t, &Registry{})

	drained := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, s.toClient)
		close(drained)
	}()
	go s.c.server.readLoop()
	go s.c.client.readLoop()

	for i := 0; i < n; i++ {
		if _, err := s.toServer.Write(frameBytes(payload)); err != nil {
			t.Fatalf("写入第 %d 帧失败: %v", i, err)
		}
	}
	_ = s.toClient.Close()
	<-drained
	_ = s.c.server.conn.Close()
	_ = s.c.client.conn.Close()
}

// TestProbeFrameChurnHeapReturns 帧流量 churn 后存活堆应回落；不回落即存在保留点。
// 预热一轮走独立连接，排除惰性初始化与夹具残留干扰。
func TestProbeFrameChurnHeapReturns(t *testing.T) {
	payload := make([]byte, 1024)

	pumpFrames(t, 2000, payload) // 预热
	time.Sleep(300 * time.Millisecond)
	g0, h0 := heapSnapshot(t)

	const frames = 50000
	pumpFrames(t, frames, payload)
	time.Sleep(300 * time.Millisecond)
	g1, h1 := heapSnapshot(t)

	t.Logf("frames×%d(~1KiB/帧): goroutines %d→%d, live heap %d→%d (Δ %+d bytes)",
		frames, g0, g1, h0, h1, int64(h1)-int64(h0))

	if g1 > g0+3 {
		t.Fatalf("goroutine 泄漏嫌疑：泵帧前 %d，泵帧后 %d", g0, g1)
	}
}
