package service

import (
	"net"
	"strings"
	"testing"
	"time"

	"nexus/internal/relay/shadow"
	"nexus/internal/telemetry"
)

// newShadowJoinService 装配带影子组件的会话：影子监听的生命周期测试需要
// joinRun.shadow 非 nil（未装配时 StartShadow 直接返回错误，走不到监听路径），
// 且 StartShadow 会发布 shadow.start/stop 事件 —— 事件总线必须就位，
// 否则 Publish 会锁 nil mutex（生产路径由 NewJoinService 保证非 nil）。
func newShadowJoinService(t *testing.T) *JoinService {
	t.Helper()
	svc := newTestJoinService(t)
	svc.events = telemetry.NewBus()
	// 多代理下影子端挂在白端会话上（mirrorRun），夹具按该形态装配。
	svc.runs = map[string]*joinRun{}
	svc.mirrorSession = "sess-shadow-test"
	svc.runs[svc.mirrorSession] = &joinRun{
		connID: svc.mirrorSession,
		shadow: &shadowRuntime{
			recorder: shadow.NewRecorder(shadow.DefaultBudget, 0x28),
			gate:     shadow.NewGate(nil, shadow.KeepAliveConfig{ClientBoundID: 0x23, ServerBoundID: 0x12}),
			filter:   shadow.NewCustomPayloadFilter(shadow.CustomPayloadIDs{}),
			anchorID: 0x28,
			kaIDs:    [2]int32{0x23, 0x12},
		},
	}
	// Stop 不等待在途 handleB 收敛（监听器既有语义）。测试拨号后若立即结束，
	// handleB 仍在写日志，会与临时目录清理竞争。这里在夹具层注册「停止并等待」，
	// 由 Cleanup 的后进先出保证它先于 newTestJoinService 的 log.Close 执行。
	t.Cleanup(func() {
		svc.StopShadow()
		time.Sleep(50 * time.Millisecond)
	})
	return svc
}

// shadowRunOf 取夹具装配的白端会话（影子端挂靠点）。
func shadowRunOf(svc *JoinService) *joinRun {
	return svc.mirrorRun()
}

// TestStartShadowFirstCallDoesNotPanic 守卫首次开启影子端不 panic。
//
// 这是一次真实故障的护栏（2026-09-17「点了开启一直转圈」）：StartShadow 曾在
// 构造 listener 之前先对它调 AddrIfListening 做幂等判定，而首次调用时
// rt.listener 为 nil —— 在 nil 接收者上调用会 panic。Wails 绑定的 panic 让
// 前端 Promise 永不 settle，界面因此永远转圈，且日志不留任何痕迹。
func TestStartShadowFirstCallDoesNotPanic(t *testing.T) {
	svc := newShadowJoinService(t)
	run := shadowRunOf(svc)

	// 真实初态：listener 尚未构造。
	if run.shadow.listener != nil {
		t.Fatal("前置条件错误：首次开启前 listener 应为 nil")
	}

	// 首次开启必须正常返回地址，不得 panic。
	addr, err := svc.StartShadow(0)
	if err != nil {
		t.Fatalf("首次开启影子端失败: %v", err)
	}
	// defer 而非 t.Cleanup：必须在测试体内先于夹具的 log.Close 收尾，
	// 否则临时目录清理时日志句柄仍被监听 goroutine 持有。

	if addr == "" {
		t.Fatal("应返回实际监听地址")
	}
	if run.shadow.listener == nil {
		t.Fatal("开启后 listener 应已写回会话，否则 StopShadow/ShadowAddr 看不到它")
	}

	// 地址必须真的可连接（绑定成功，而不只是返回了一个字符串）。
	// 拨号会让服务端起一个 handleB goroutine；拨完立即关，并在停止后
	// 等它收敛 —— 否则临时目录清理会与日志写入竞争（Stop 不等待在途连接，
	// 这是监听器的既有语义，不是缺陷）。
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("影子监听地址不可连接 %s: %v", addr, err)
	}
	_ = conn.Close()
}

// TestStartShadowIsIdempotent 重复开启返回同一地址，不得重复监听。
func TestStartShadowIsIdempotent(t *testing.T) {
	svc := newShadowJoinService(t)

	first, err := svc.StartShadow(0)
	if err != nil {
		t.Fatalf("首次开启失败: %v", err)
	}

	second, err := svc.StartShadow(0)
	if err != nil {
		t.Fatalf("重复开启应幂等成功: %v", err)
	}
	if first != second {
		t.Fatalf("重复开启返回不同地址: %q vs %q", first, second)
	}
	if got := svc.ShadowAddr(); got != first {
		t.Fatalf("ShadowAddr = %q，期望 %q", got, first)
	}
}

// TestStopShadowThenRestart 停止后可再次开启，且监听地址可用。
func TestStopShadowThenRestart(t *testing.T) {
	svc := newShadowJoinService(t)

	first, err := svc.StartShadow(0)
	if err != nil {
		t.Fatalf("首次开启失败: %v", err)
	}
	svc.StopShadow()
	if got := svc.ShadowAddr(); got != "" {
		t.Fatalf("停止后 ShadowAddr 应为空，实得 %q", got)
	}
	// 停止后原地址不应再接受连接。
	if err := dialShadowAddr(first); err == nil {
		t.Fatalf("停止后 %s 仍在监听", first)
	}

	// listener 实例仍在（Stop 只关监听），再次开启应重新监听而不是 panic。
	restarted, err := svc.StartShadow(0)
	if err != nil {
		t.Fatalf("重启失败: %v", err)
	}
	if err := dialShadowAddr(restarted); err != nil {
		t.Fatalf("重启后地址不可连接 %s: %v", restarted, err)
	}
}

// TestStartShadowWithoutSession 无白端会话时报错而不是 panic。
func TestStartShadowWithoutSession(t *testing.T) {
	svc := newTestJoinService(t)

	if _, err := svc.StartShadow(0); err == nil {
		t.Fatal("无白端会话时应返回错误")
	} else if !strings.Contains(err.Error(), "会话") {
		t.Fatalf("错误应说明无会话，实得 %v", err)
	}
}

// TestStartShadowWithoutComponents 会话存在但影子组件未装配（影子端在系统设置
// 中关闭，会话启动时未装配录制）时报错而不是 panic，且错误要指向系统设置开关。
func TestStartShadowWithoutComponents(t *testing.T) {
	svc := newTestJoinService(t)
	svc.mirrorSession = "sess-no-shadow"
	svc.runs = map[string]*joinRun{svc.mirrorSession: {connID: svc.mirrorSession}}

	if _, err := svc.StartShadow(0); err == nil {
		t.Fatal("组件未装配时应返回错误")
	} else if !strings.Contains(err.Error(), "影子端未启用") || !strings.Contains(err.Error(), "系统设置") {
		t.Fatalf("错误应指向系统设置中的影子端开关，实得 %v", err)
	}
}

// dialShadowAddr 拨一次影子监听地址，确认它真的在监听。
func dialShadowAddr(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
