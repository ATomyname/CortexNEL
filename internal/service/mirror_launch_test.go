package service

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"nexus/internal/netease"
	"nexus/internal/profile"
	"nexus/internal/session"
	"nexus/internal/telemetry"
)

// TestControlChannelLifecycle 控制通道必须挂到指定会话上，并在关闭后真的不再监听。
// 端口没挂上会话就无人关闭，客户端下次启动会连到一条属于上一次会话的通道。
func TestControlChannelLifecycle(t *testing.T) {
	svc := newTestJoinService(t)
	run := &joinRun{connID: "sess-test"}
	svc.runs = map[string]*joinRun{run.connID: run}

	port := svc.openControlChannel(run, newTestMirrorRequest(), 25565)
	if port <= 0 {
		t.Fatal("应返回可用端口，否则启动参数不会写 launcherport")
	}
	if run.control == nil {
		t.Fatal("通道未挂到会话上，Stop 时无人关闭")
	}
	if err := dialControlPort(port); err != nil {
		t.Fatalf("通道应可连接：%v", err)
	}

	svc.closeControlChannel(run)
	if run.control != nil {
		t.Fatal("关闭后应从会话上摘掉")
	}
	if err := dialControlPort(port); err == nil {
		t.Fatal("关闭后端口仍在监听")
	}
}

// TestOpenControlChannelWithoutSession 会话已不在运行表（已停止）时不得对外宣告端口：
// 客户端拿到一个即将被关掉的端口比拿不到更糟。
func TestOpenControlChannelWithoutSession(t *testing.T) {
	svc := newTestJoinService(t)

	if port := svc.openControlChannel(&joinRun{connID: "sess-gone"}, newTestMirrorRequest(), 25565); port != 0 {
		t.Fatalf("会话已停止时应返回 0，实得 %d", port)
	}
}

// TestCloseControlChannelIsSafeWithoutChannel 未开通道时关闭是空操作，
// 让失败路径可以无条件调用。
func TestCloseControlChannelIsSafeWithoutChannel(t *testing.T) {
	svc := newTestJoinService(t)
	svc.closeControlChannel(&joinRun{})
	svc.closeControlChannel(nil)
}

// TestOpenAuthChannelDirectMode 直连官方服模式下必须开出鉴权代答通道并挂到会话：
// -DlauncherControlPort 指向它，客户端 native authlib 的 join 请求才有人代答。
func TestOpenAuthChannelDirectMode(t *testing.T) {
	svc := newTestJoinService(t)
	run := &joinRun{connID: "sess-auth"}
	svc.runs = map[string]*joinRun{run.connID: run}

	req := newTestMirrorRequest()
	req.serverHost = "pc.example.net"
	req.serverPort = 20001
	req.creds = func() (netease.Credentials, error) { return netease.Credentials{}, nil }

	port := svc.openAuthChannel(run, req)
	if port <= 0 {
		t.Fatal("直连模式应返回鉴权代答端口")
	}
	if run.auth == nil {
		t.Fatal("通道未挂到会话上，Stop 时无人关闭")
	}
	if err := dialControlPort(port); err != nil {
		t.Fatalf("鉴权通道应可连接：%v", err)
	}

	svc.closeAuthChannel(run)
	if run.auth != nil {
		t.Fatal("关闭后应从会话上摘掉")
	}
	if err := dialControlPort(port); err == nil {
		t.Fatal("关闭后端口仍在监听")
	}
}

// TestOpenAuthChannelSkipsRelayMode relay 模式（serverHost 为空）或凭据缺失时
// 不得开鉴权代答通道：-DlauncherControlPort 应回落控制通道，行为与既有链路一致。
func TestOpenAuthChannelSkipsRelayMode(t *testing.T) {
	svc := newTestJoinService(t)
	run := &joinRun{connID: "sess-relay"}
	svc.runs = map[string]*joinRun{run.connID: run}

	// serverHost 为空：relay 模式。
	if port := svc.openAuthChannel(run, newTestMirrorRequest()); port != 0 {
		t.Fatalf("relay 模式不应开鉴权通道，实得端口 %d", port)
	}

	// serverHost 已给但 creds 为 nil：无从代答，同样不开。
	req := newTestMirrorRequest()
	req.serverHost = "pc.example.net"
	req.serverPort = 20001
	if port := svc.openAuthChannel(run, req); port != 0 {
		t.Fatalf("凭据缺失时不应开鉴权通道，实得端口 %d", port)
	}
}

// TestOpenAuthChannelWithoutSession 会话已不在运行表时必须立刻收掉通道：
// 与控制通道同一故障面 —— 留下无人关闭的监听端口。
func TestOpenAuthChannelWithoutSession(t *testing.T) {
	svc := newTestJoinService(t)

	req := newTestMirrorRequest()
	req.serverHost = "pc.example.net"
	req.serverPort = 20001
	req.creds = func() (netease.Credentials, error) { return netease.Credentials{}, nil }

	if port := svc.openAuthChannel(&joinRun{connID: "sess-gone"}, req); port != 0 {
		t.Fatalf("会话已停止时应返回 0，实得 %d", port)
	}
}

func TestRelayPort(t *testing.T) {
	cases := []struct {
		addr    string
		want    int
		wantErr bool
	}{
		{"127.0.0.1:25565", 25565, false},
		{"0.0.0.0:0", 0, false},
		{"[::1]:25577", 25577, false},
		{"127.0.0.1", 0, true},
		{"", 0, true},
	}
	for _, c := range cases {
		got, err := relayPort(c.addr)
		if c.wantErr {
			if err == nil {
				t.Fatalf("%q 应报错", c.addr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q 意外报错：%v", c.addr, err)
		}
		if got != c.want {
			t.Fatalf("%q 端口 = %d，期望 %d", c.addr, got, c.want)
		}
	}
}

// TestChildEnvWithNativeLibDirs 原生库目录必须按顺序加到子进程 PATH 最前面，
// 且其余环境变量原样保留。
//
// 这是一次真实故障的护栏（2026-09-22，MC 1.12.2）：Windows 的 LoadLibrary
// 解析被加载 DLL 的依赖时只认应用程序目录、系统目录与 PATH，不认
// java.library.path。lwjgl64.dll 明明就在 -Djava.library.path 指向的
// natives 目录里（与官方启动器那份 SHA256 一致，Windows 直接 LoadLibrary
// 也成功），org.lwjgl.Sys 却报 "no lwjgl64 in java.library.path"，
// 客户端起不来。把 natives 加进 PATH 后该错误消失。
func TestChildEnvWithNativeLibDirs(t *testing.T) {
	// 无目录时不改动环境（nil 让 exec 继承父进程环境）。
	if got := childEnvWithNativeLibDirs(nil); got != nil {
		t.Fatalf("无原生库目录时应返回 nil，实得 %v", got)
	}

	natives := t.TempDir()
	runtimeDir := t.TempDir()
	env := childEnvWithNativeLibDirs([]string{natives, runtimeDir})
	if env == nil {
		t.Fatal("应构造出子进程环境")
	}

	var pathValue string
	other := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathValue = strings.TrimPrefix(kv, "PATH=")
			continue
		}
		other++
	}
	if pathValue == "" {
		t.Fatal("子进程环境缺少 PATH")
	}
	// 顺序必须是 natives 在前、runtime 在后（Paths 按优先级解析）。
	wantPrefix := natives + string(os.PathListSeparator) + runtimeDir + string(os.PathListSeparator)
	if !strings.HasPrefix(pathValue, wantPrefix) {
		t.Fatalf("PATH 前缀 = %q，期望以 %q 开头", pathValue, wantPrefix)
	}
	// 原有 PATH 必须仍在，否则系统命令与后续 DLL 搜索会断。
	if !strings.Contains(pathValue, os.Getenv("PATH")) {
		t.Fatal("原有 PATH 未被保留")
	}
	// 其余环境变量应原样保留（数量不少于父进程）。
	if other < len(os.Environ())-1 {
		t.Fatalf("其余环境变量丢失：实得 %d，父进程有 %d", other, len(os.Environ())-1)
	}
}

func newTestJoinService(t *testing.T) *JoinService {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return &JoinService{log: log}
}

func newTestMirrorRequest() mirrorRequest {
	return mirrorRequest{
		roleName:  "Steve",
		mcVersion: "1.20.1",
		manager:   &session.Manager{},
	}
}

func dialControlPort(port int) error {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(loopbackHost, strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

// TestMirrorLauncherKillsPreviousOnStart 启动前必须终止上一个尚未退出的镜像客户端。
//
// 这是一次真实故障的护栏（2026-09-17）：MirrorLauncher.cmd 是单值字段，
// Start 直接覆盖它会让旧进程失去引用、永远无法回收。旧客户端残留会持续连
// 本机 relay 端口做 server list ping（nextState=1），把当前会话反复打断成
// FAULTED —— 录制因此永远累积不到世界锚点，影子端接入时会话已死。
// 实测日志特征：92 次 state=STATUS 与「当前无活动会话连接」。
func TestMirrorLauncherKillsPreviousOnStart(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("用系统自带的 ping 作长驻进程，仅 Windows 断言参数形状")
	}
	logDir := t.TempDir()
	log, err := telemetry.New(nil, logDir)
	if err != nil {
		t.Fatal(err)
	}
	l := NewMirrorLauncher(log)

	// 单个 Cleanup 内按序收尾：先终止进程、等它真正退出，再关日志。
	// 分成两个 Cleanup 会因后进先出而与 TempDir 清理竞争 ——
	// 日志仍被写入时目录非空会让清理失败（曾表现为约 1/3 概率的 flaky）。
	t.Cleanup(func() {
		_ = l.Stop()
		time.Sleep(100 * time.Millisecond)
		_ = log.Close()
	})

	// 第一个长驻进程（ping -n 60 约 60 秒）。
	first, err := l.Start(profile.LaunchSpec{
		Exe: "ping", Args: []string{"-n", "60", "127.0.0.1"}, WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("首个进程启动失败: %v", err)
	}
	firstPID := l.CurrentPID()
	if firstPID == 0 {
		t.Fatal("首个进程应被记录")
	}

	// 第二个进程启动时必须先杀掉第一个。
	if _, err := l.Start(profile.LaunchSpec{
		Exe: "ping", Args: []string{"-n", "60", "127.0.0.1"}, WorkDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("第二个进程启动失败: %v", err)
	}

	// 第一个进程应已退出（等待 done 通道关闭，最多 3 秒）。
	select {
	case <-first:
	case <-time.After(3 * time.Second):
		t.Fatal("启动新进程时必须终止上一个镜像客户端，否则它会残留并打断会话")
	}

	secondPID := l.CurrentPID()
	if secondPID == 0 || secondPID == firstPID {
		t.Fatalf("当前进程应更新为新 PID：first=%d second=%d", firstPID, secondPID)
	}
}
