package service

// leak_probe_test.go — 职责: 内存泄漏动态探针（会话 churn 的堆/goroutine 测量）。
// 2026-09-23 排查「内存泄漏」：静态审查未发现无界保留点后，用本探针动态取证。
// 断言只用 goroutine 数（确定性）；堆数字只记录不判定（GC 时机属抖动，T1/T2 纪律）。

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"nexus/internal/account"
)

// heapSnapshot 连做两次 GC 后取存活堆与 goroutine 数。
func heapSnapshot(t *testing.T) (int, uint64) {
	t.Helper()
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return runtime.NumGoroutine(), m.HeapAlloc
}

// runProxyOnce 起一次仅代理会话，拨一个真实连接送一字节后断开，再停止会话 ——
// 走全 handleClient→dispatch→OnDisconnect→Stop 清理路径。
func runProxyOnce(t *testing.T, svc *JoinService) {
	t.Helper()
	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly: %v", err)
	}
	if conn, derr := net.Dial("tcp", handle.ListenAddr); derr == nil {
		_, _ = conn.Write([]byte{0x00})
		_ = conn.Close()
	}
	if err := svc.Stop(handle.SessionID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestProbeSessionChurnNoLeak 会话反复开/关后，存活堆与 goroutine 数不得线性增长。
// 每轮都走完整的连接建立/断开/清理路径；预热一轮排除惰性初始化干扰。
func TestProbeSessionChurnNoLeak(t *testing.T) {
	svc := newEventJoinService(t)

	runProxyOnce(t, svc) // 预热
	time.Sleep(300 * time.Millisecond)
	g0, h0 := heapSnapshot(t)

	const churn = 30
	for i := 0; i < churn; i++ {
		runProxyOnce(t, svc)
	}
	time.Sleep(300 * time.Millisecond)
	g1, h1 := heapSnapshot(t)

	t.Logf("churn×%d: goroutines %d→%d, live heap %d→%d (Δ %+d bytes)",
		churn, g0, g1, h0, h1, int64(h1)-int64(h0))

	if g1 > g0+5 {
		t.Fatalf("goroutine 泄漏嫌疑：预热后 %d，churn×%d 后 %d", g0, churn, g1)
	}
}

// TestProbePerSessionRetainedHeap 会话保持开启时的留存堆成本（对照影子录制器开/关）。
// 每会话不跑流量：测的是"装配成本"基线；录制器随流量增长的成本见
// internal/relay/shadow 的预算测试（DefaultBudget 64MiB / 硬上限 ×4）。
// 并发会话要求（服务器, 角色）互不相同（准入拒绝同服同角色），故先补开账号。
func TestProbePerSessionRetainedHeap(t *testing.T) {
	const sessions = 5
	for _, shadowOn := range []bool{false, true} {
		svc := newEventJoinService(t)
		svc.SetShadowEnabled(shadowOn)

		accs := []string{"acc-1", "acc-2"} // 夹具已有
		for i := len(accs); i < sessions; i++ {
			id := fmt.Sprintf("acc-probe-%d", i)
			if err := svc.accounts.Upsert(&account.Account{
				ID: id, Label: "探针", UserID: int64(900000000 + i), RoleName: fmt.Sprintf("Probe%02d", i),
			}, "tok", false); err != nil {
				t.Fatal(err)
			}
			accs = append(accs, id)
		}

		_, h0 := heapSnapshot(t)
		ids := make([]string, 0, sessions)
		for i := 0; i < sessions; i++ {
			handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", accs[i])
			if err != nil {
				t.Fatalf("JoinProxyOnly #%d: %v", i, err)
			}
			ids = append(ids, handle.SessionID)
		}
		g1, h1 := heapSnapshot(t)
		t.Logf("shadow=%v: %d 会话留存 heap %+d bytes（均摊 %+d bytes/会话），goroutines %d",
			shadowOn, sessions, int64(h1)-int64(h0), (int64(h1)-int64(h0))/sessions, g1)

		for _, id := range ids {
			_ = svc.Stop(id)
		}
	}
}
