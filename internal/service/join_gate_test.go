package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"nexus/internal/launcher"
	"nexus/internal/profile"
	"nexus/internal/telemetry"
)

// gateAssets 让第一次 join 卡在资产补齐处，用来把"启动中"这段窗口拉长，
// 使并发/列表相关的断言稳定可复现。
type gateAssets struct {
	dir     string
	entered chan struct{}
	release chan struct{}
}

func (g gateAssets) EnsureJoinAssets(context.Context, AssetRequest) (AssetResult, error) {
	close(g.entered)
	<-g.release
	return AssetResult{ClientDir: g.dir}, nil
}

// TestProxyOnlyAllowsManyConcurrent 仅代理模式可任意多开：用不同角色
// （同服不同角色、同角色不同服）起三个，应拿到三个不同 sessionID。
func TestProxyOnlyAllowsManyConcurrent(t *testing.T) {
	svc := newEventJoinService(t)

	cases := []struct{ env, server, account string }{
		{"env-1", "srv-1", "acc-1"}, // Steve @ srv-1
		{"env-1", "srv-1", "acc-2"}, // Alex  @ srv-1
		{"env-1", "srv-2", "acc-1"}, // Steve @ srv-2
	}
	ids := map[string]bool{}
	for i, c := range cases {
		h, err := svc.JoinProxyOnly(context.Background(), c.env, c.server, c.account)
		if err != nil {
			t.Fatalf("第 %d 个仅代理会话启动失败: %v", i+1, err)
		}
		if h.SessionID == "" {
			t.Fatal("SessionID 不应为空")
		}
		if ids[h.SessionID] {
			t.Fatalf("sessionID 重复: %s", h.SessionID)
		}
		ids[h.SessionID] = true
		t.Cleanup(func() { _ = svc.Stop(h.SessionID) })
	}

	if got := len(svc.ListSessions()); got != 3 {
		t.Fatalf("ListSessions = %d，期望 3", got)
	}
}

// TestDuplicateRoleOnSameServerRejected 同一服务器 + 同一角色不许多开。
func TestDuplicateRoleOnSameServerRejected(t *testing.T) {
	svc := newEventJoinService(t)
	first, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("首个会话: %v", err)
	}
	t.Cleanup(func() { _ = svc.StopAll() })

	// 同服同角色（acc-1 的角色名 Steve）→ 拒。
	_, err = svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if !errors.Is(err, ErrDuplicateRole) {
		t.Fatalf("同服同角色应被拒，实得 %v", err)
	}

	// 同服不同角色（acc-2 = Alex）→ 放行。
	other, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-2")
	if err != nil {
		t.Fatalf("同服不同角色应放行: %v", err)
	}

	// 同角色不同服（Steve @ srv-2）→ 放行。
	elsewhere, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-2", "acc-1")
	if err != nil {
		t.Fatalf("同角色不同服应放行: %v", err)
	}

	// 停掉冲突的那个之后，原组合可以再起。
	if err := svc.Stop(first.SessionID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	again, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("停掉冲突会话后应可重起: %v", err)
	}
	if again.SessionID == other.SessionID || again.SessionID == elsewhere.SessionID {
		t.Fatal("重起应拿到新 sessionID")
	}
}

// TestProxyOnlyIgnoresJavaMismatch 仅代理模式不得因 Java 大版本被拒。
//
// 这是一次真实故障的护栏（2026-09-21）：MC 1.8.9 需要 Java 8，机器上只有
// Zulu 21，而 CDN 上没有 Java 8 的已实证分发包。Java 检查当时无条件执行，
// 于是资产全部就绪后仍以「进服前置检查失败」拒绝建代理 —— 可仅代理根本不
// 启动客户端，relay 只是监听端口转发字节，与 Java 大版本无关。
//
// 断言在本机任意 Java 版本下都成立；若本机恰好是 Java 8，旧代码也能通过，
// 那时本用例的回归捕获力为零（已在此说明，不静默跳过）。
func TestProxyOnlyIgnoresJavaMismatch(t *testing.T) {
	svc := newEventJoinService(t)
	if err := svc.profiles.SaveServer(profile.Server{
		ID: "srv-189", Name: "老版本服", GameID: "4634308743419409922",
		Host: "127.0.0.1", Port: 1, McVersion: "1.8.9",
	}); err != nil {
		t.Fatal(err)
	}

	h, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-189", "acc-1")
	if err != nil {
		t.Fatalf("仅代理不应因 Java 大版本被拒: %v", err)
	}
	if h.ListenAddr == "" {
		t.Fatal("应返回监听地址")
	}
	t.Cleanup(func() { _ = svc.StopAll() })
}

// TestEnsureMirrorJavaAcceptsMatchingJava 大版本匹配时直接放行，不触发部署。
// 自适应：取本机 Java 的实际大版本，再挑一个要求该版本的 MC 版本，
// 因此不依赖机器上装的是哪个 Java，也不碰网络。
func TestEnsureMirrorJavaAcceptsMatchingJava(t *testing.T) {
	java, jerr := launcher.DetectJavaForVersion("1.8.9")
	if jerr != nil || java == "" {
		t.Skip("本机探测不到 Java")
	}
	major, err := launcher.JavaMajor(java)
	if err != nil {
		t.Skipf("读不到本机 Java 版本: %v", err)
	}
	// 按大版本挑一个要求它的 MC 版本（RequiredJavaMajors 的三条分支）。
	mcByMajor := map[int]string{8: "1.8.9", 17: "1.17.1", 21: "1.21.8", 22: "1.21.8"}
	mcVer, ok := mcByMajor[major]
	if !ok {
		t.Skipf("本机 Java %d 不在档位表内", major)
	}

	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	svc := &JoinService{log: log, events: telemetry.NewBus(), runs: map[string]*joinRun{}}

	env := profile.Env{JavaPath: java, ClientDir: t.TempDir()}
	if err := svc.ensureMirrorJava(context.Background(), &env, mcVer); err != nil {
		t.Fatalf("Java %d 匹配 MC %s 应放行: %v", major, mcVer, err)
	}
	if env.JavaPath != java {
		t.Fatalf("匹配时不应改写 JavaPath：%q → %q", java, env.JavaPath)
	}
}

// TestEnsureMirrorJavaDeploysOnMismatch 大版本不匹配时自动从 CDN 部署对应 JRE
// 并放行 —— 这是 jdk8_v64_v1.7z 登记后的新行为（此前只能报错）。
// 41MB 级真实下载，与 jrebundle_test.go 的实下载用例同样门控。
func TestEnsureMirrorJavaDeploysOnMismatch(t *testing.T) {
	if os.Getenv("JOINPROBE_JRE") != "1" {
		t.Skip("需要 JOINPROBE_JRE=1（真实下载 JRE）")
	}
	java, jerr := launcher.DetectJavaForVersion("1.8.9")
	if jerr != nil || java == "" {
		t.Skip("本机探测不到 Java")
	}
	major, err := launcher.JavaMajor(java)
	if err != nil || major == 8 {
		t.Skip("本机不是 Java 8，无法构造不匹配场景")
	}

	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	svc := &JoinService{log: log, events: telemetry.NewBus(), runs: map[string]*joinRun{}}

	// ClientDir 决定 JRE 落点（<ClientDir 的上两级>/jre），用独立临时目录避免污染真实数据。
	clientDir := filepath.Join(t.TempDir(), ".minecraft")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := profile.Env{JavaPath: java, ClientDir: clientDir}
	if err := svc.ensureMirrorJava(context.Background(), &env, "1.8.9"); err != nil {
		t.Fatalf("不匹配时应自动部署 Java 8 并放行: %v", err)
	}
	deployed, err := launcher.JavaMajor(env.JavaPath)
	if err != nil || deployed != 8 {
		t.Fatalf("部署后 JavaPath 应指向 Java 8，实得 %d（err=%v）", deployed, err)
	}
}
func TestDuplicateRoleAppliesToMirrorToo(t *testing.T) {
	svc := newEventJoinService(t)
	mirror, release, err := svc.acquireJoinSlot(true)
	if err != nil {
		t.Fatalf("白端位: %v", err)
	}
	t.Cleanup(release)
	// 手动把白端会话注册进表，模拟它已在跑。
	svc.mu.Lock()
	svc.runs[mirror] = &joinRun{connID: mirror, serverID: "srv-1", accountID: "acc-1", roleName: "Steve", launchMirror: true}
	svc.mu.Unlock()
	t.Cleanup(func() { _ = svc.StopAll() })

	if _, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1"); !errors.Is(err, ErrDuplicateRole) {
		t.Fatalf("白端占用着同服同角色时仅代理应被拒，实得 %v", err)
	}
}

// TestMirrorAllowsOnlyOne 一键进服（自动拉白端）同时只允许一个：
// 白端要占控制通道端口、要起 javaw，多开没有意义。
//
// 这里只验闸门语义（不起真实客户端）：直接占位/释放，避免夹具环境把
// launchMirror 的真实失败路径混进来。
func TestMirrorAllowsOnlyOne(t *testing.T) {
	svc := newEventJoinService(t)

	first, release, err := svc.acquireJoinSlot(true)
	if err != nil {
		t.Fatalf("首个白端位: %v", err)
	}
	if _, _, err := svc.acquireJoinSlot(true); !errors.Is(err, ErrMirrorAlreadyRunning) {
		t.Fatalf("第二个白端位应被拒，实得 %v", err)
	}

	// 白端占着，仅代理仍可多开（两者互不阻塞）。
	if _, _, err := svc.acquireJoinSlot(false); err != nil {
		t.Fatalf("白端占位时仅代理应可起: %v", err)
	}

	// 释放后位子空出，可以再占。
	release()
	if _, _, err := svc.acquireJoinSlot(true); err != nil {
		t.Fatalf("白端位释放后应可再占: %v", err)
	}
	_ = first
}

// TestMirrorSlotReleasedOnFailedJoin 启动失败也必须释放白端位：
// 否则一次失败后白端位永久占着，用户再也无法一键进服。
func TestMirrorSlotReleasedOnFailedJoin(t *testing.T) {
	svc := newEventJoinService(t)
	// 不存在的服务器档案 → join 在读档案处失败返回。
	if _, err := svc.Join(context.Background(), "env-1", "no-such-server", "acc-1"); err == nil {
		t.Fatal("不存在的服务器档案应报错")
	}
	svc.mu.Lock()
	occupied := svc.mirrorSession != ""
	svc.mu.Unlock()
	if occupied {
		t.Fatal("失败后白端位应已释放")
	}
	if _, _, err := svc.acquireJoinSlot(true); err != nil {
		t.Fatalf("白端位应可再次占用: %v", err)
	}
}

// TestStopOneKeepsOthers 停一个会话不影响其余（多代理的核心语义）。
func TestStopOneKeepsOthers(t *testing.T) {
	svc := newEventJoinService(t)
	first, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("第一个: %v", err)
	}
	// 同服不同角色：满足 (服务器, 角色) 唯一约束的第二条代理。
	second, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-2")
	if err != nil {
		t.Fatalf("第二个: %v", err)
	}
	t.Cleanup(func() { _ = svc.StopAll() })

	if err := svc.Stop(first.SessionID); err != nil {
		t.Fatalf("Stop 第一个: %v", err)
	}

	list := svc.ListSessions()
	if len(list) != 1 {
		t.Fatalf("停一个后应剩 1 个，实得 %d", len(list))
	}
	if list[0].SessionID != second.SessionID {
		t.Fatalf("剩下的应是第二个会话 %s，实得 %s", second.SessionID, list[0].SessionID)
	}
	// 幂等：再停一次同一个返回 ErrNoSession，不影响剩下的。
	if err := svc.Stop(first.SessionID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("重复 Stop 应返回 ErrNoSession，实得 %v", err)
	}
}

// TestStopAllClearsEverything 全停：列表清空、白端位释放。
func TestStopAllClearsEverything(t *testing.T) {
	svc := newEventJoinService(t)
	if _, err := svc.Join(context.Background(), "env-1", "srv-1", "acc-1"); err != nil {
		t.Fatalf("白端: %v", err)
	}
	if _, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-2"); err != nil {
		t.Fatalf("仅代理: %v", err)
	}

	if err := svc.StopAll(); err != nil {
		t.Fatalf("StopAll: %v", err)
	}
	if got := len(svc.ListSessions()); got != 0 {
		t.Fatalf("StopAll 后应无会话，实得 %d", got)
	}
	// 白端位已释放：可以再起一个白端。
	if _, err := svc.Join(context.Background(), "env-1", "srv-1", "acc-1"); err != nil {
		t.Fatalf("StopAll 后白端位应释放: %v", err)
	}
}

// TestSessionIDsUniqueAcrossServers 同一服务器起多个代理不许撞 ID
// （(服务器, 角色) 唯一约束下用不同角色/服务器起，ID 仍须互异）。
func TestSessionIDsUniqueAcrossServers(t *testing.T) {
	svc := newEventJoinService(t)
	combos := []struct{ server, account string }{
		{"srv-1", "acc-1"},
		{"srv-1", "acc-2"},
		{"srv-2", "acc-1"},
		{"srv-2", "acc-2"},
	}
	seen := map[string]bool{}
	for _, c := range combos {
		h, err := svc.JoinProxyOnly(context.Background(), "env-1", c.server, c.account)
		if err != nil {
			t.Fatalf("%s/%s: %v", c.server, c.account, err)
		}
		if seen[h.SessionID] {
			t.Fatalf("sessionID 撞了: %s", h.SessionID)
		}
		seen[h.SessionID] = true
		t.Cleanup(func() { _ = svc.Stop(h.SessionID) })
	}
}

// TestListSessionsFields 列表快照必须带上前端展示所需的字段。
func TestListSessionsFields(t *testing.T) {
	svc := newEventJoinService(t)
	h, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly: %v", err)
	}
	t.Cleanup(func() { _ = svc.StopAll() })

	list := svc.ListSessions()
	if len(list) != 1 {
		t.Fatalf("应有一个会话，实得 %d", len(list))
	}
	got := list[0]
	if got.SessionID != h.SessionID {
		t.Fatalf("SessionID = %s，期望 %s", got.SessionID, h.SessionID)
	}
	if got.ServerID != "srv-1" {
		t.Fatalf("ServerID = %s，期望 srv-1", got.ServerID)
	}
	if got.AccountID != "acc-1" {
		t.Fatalf("AccountID = %s，期望 acc-1", got.AccountID)
	}
	if got.RoleName != "Steve" {
		t.Fatalf("RoleName = %s，期望 Steve", got.RoleName)
	}
	if got.Stage == "" {
		t.Fatal("Stage 不应为空")
	}
	if got.ListenAddr == "" {
		t.Fatal("ListenAddr 不应为空")
	}
	if got.LaunchMirror {
		t.Fatal("仅代理会话 LaunchMirror 应为 false")
	}
}

func TestStopAccountSessionsOnlyStopsMatchingAccount(t *testing.T) {
	svc := newEventJoinService(t)
	first, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly acc-1: %v", err)
	}
	other, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-2")
	if err != nil {
		t.Fatalf("JoinProxyOnly acc-2: %v", err)
	}
	t.Cleanup(func() { _ = svc.StopAll() })

	if err := svc.StopAccountSessions("acc-1"); err != nil {
		t.Fatalf("StopAccountSessions: %v", err)
	}
	got := svc.ListSessions()
	if len(got) != 1 || got[0].SessionID != other.SessionID {
		t.Fatalf("remaining sessions = %+v, expected only %s", got, other.SessionID)
	}
	if err := svc.StopAccountSessions("acc-1"); err != nil {
		t.Fatalf("repeated StopAccountSessions should be idempotent: %v", err)
	}
	if err := svc.Stop(first.SessionID); !errors.Is(err, ErrNoSession) {
		t.Fatalf("stopped session should be absent, err = %v", err)
	}
}

// TestMirrorSessionRetainedWhileRunning 白端会话成功启动后，mirrorRun 与 MirrorSessionStage
// 必须在运行期持续有效，直到 Stop 调用才释放（防止影子端等挂靠服务报 ErrNoSession）。
func TestMirrorSessionRetainedWhileRunning(t *testing.T) {
	svc := newEventJoinService(t)
	h, err := svc.Join(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	t.Cleanup(func() { _ = svc.StopAll() })

	// 运行期：白端会话必须被 mirrorRun 索引，阶段不为空
	run := svc.mirrorRun()
	if run == nil {
		t.Fatal("运行期 mirrorRun 不应为 nil")
	}
	if run.connID != h.SessionID {
		t.Fatalf("mirrorRun.connID = %s, 期望 %s", run.connID, h.SessionID)
	}
	if stage := svc.MirrorSessionStage(); stage == "" {
		t.Fatal("运行期 MirrorSessionStage 不应为空")
	}

	// 运行期：不允许重复起第二个白端
	if _, err := svc.Join(context.Background(), "env-1", "srv-2", "acc-2"); !errors.Is(err, ErrMirrorAlreadyRunning) {
		t.Fatalf("运行期再起白端应被拒，实得 %v", err)
	}

	// 停止后：白端位释放，mirrorRun 与 MirrorSessionStage 清空
	if err := svc.Stop(h.SessionID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := svc.mirrorRun(); got != nil {
		t.Fatalf("Stop 后 mirrorRun 应为 nil，实得 %v", got)
	}
	if stage := svc.MirrorSessionStage(); stage != "" {
		t.Fatalf("Stop 后 MirrorSessionStage 应为空，实得 %s", stage)
	}
}
