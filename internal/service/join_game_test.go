package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nexus/internal/account"
	"nexus/internal/netease"
	"nexus/internal/profile"
	"nexus/internal/relay"
	"nexus/internal/relay/shadow"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

func TestShadowFrameStateDoesNotReplayLoginSuccessAsConfiguration(t *testing.T) {
	if got := shadowFrameState(relay.StateLogin, 2); got != shadow.FrameStateUnknown {
		t.Fatalf("LoginSuccess in login state = %v, want unknown", got)
	}
	if got := shadowFrameState(relay.StateConfiguration, 2); got != shadow.FrameStateConfiguration {
		t.Fatalf("configuration packet state = %v, want configuration", got)
	}
}

// collectSessionEvents 订阅 bus 上全部 session.* 事件，返回取样函数。
func collectSessionEvents(t *testing.T, bus *telemetry.Bus) func() []telemetry.Event {
	t.Helper()
	var got []telemetry.Event
	cancel := bus.Subscribe("session.", func(e telemetry.Event) { got = append(got, e) })
	t.Cleanup(cancel)
	return func() []telemetry.Event { return got }
}

// stubAssets 是测试用的资产补齐器：总是返回一个存在的临时客户端根，
// 不触碰网关与本机真实安装 —— 生产路径要求每次进服都按权威清单对账，
// 测试同样不得依赖网易安装的探测回退。
type stubAssets struct{ dir string }

func (s stubAssets) EnsureJoinAssets(context.Context, AssetRequest) (AssetResult, error) {
	return AssetResult{ClientDir: s.dir}, nil
}

// newEventJoinService 装配最小可用编排：真实档案管理器 + 内存 token 账号 + 空注册表
// （事件测试不进协议握手，引擎不会被触达）。
func newEventJoinService(t *testing.T) *JoinService {
	t.Helper()
	root := t.TempDir()
	paths, err := store.NewPaths(root)
	if err != nil {
		t.Fatal(err)
	}
	log, err := telemetry.New(nil, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	bus := telemetry.NewBus()
	accounts := account.NewManager(paths, account.NewTokenStore(paths))
	if err := accounts.Upsert(&account.Account{ID: "acc-1", Label: "测试", UserID: 933874987, RoleName: "Steve"}, "tok", false); err != nil {
		t.Fatal(err)
	}
	// 第二个账号（不同角色名）：多代理测试需要"同服不同角色"来验证
	// 只有 (服务器, 角色) 都相同才被拒。
	if err := accounts.Upsert(&account.Account{ID: "acc-2", Label: "测试2", UserID: 933874988, RoleName: "Alex"}, "tok", false); err != nil {
		t.Fatal(err)
	}
	profiles := profile.NewManager(paths)
	if err := profiles.SaveServer(profile.Server{ID: "srv-1", Name: "测试服", GameID: "4661334467366178884", Host: "127.0.0.1", Port: 1}); err != nil {
		t.Fatal(err)
	}
	// 第二个服务器：验证"同角色不同服"不被误拒。
	if err := profiles.SaveServer(profile.Server{ID: "srv-2", Name: "测试服2", GameID: "4661334467366178885", Host: "127.0.0.1", Port: 2}); err != nil {
		t.Fatal(err)
	}
	svc := NewJoinService(accounts, profiles, NewMirrorLauncher(log), &relay.Registry{}, bus, log)
	svc.SetAssetProvider(stubAssets{dir: t.TempDir()})
	return svc
}

// TestJoinPublishesLifecycleEvents 启动即发 session.start，用户停止发 session.stop
// （CLOSED 迁移 + durationMs），生命周期路径无 lost 事件。
func TestJoinPublishesLifecycleEvents(t *testing.T) {
	svc := newEventJoinService(t)
	sample := collectSessionEvents(t, svc.events)

	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	if handle.ListenAddr == "" {
		t.Fatal("应返回监听地址")
	}
	if got := svc.Stop(handle.SessionID); got != nil {
		t.Fatalf("Stop 失败: %v", got)
	}

	names := map[string]bool{}
	for _, e := range sample() {
		names[e.Name] = true
	}
	if !names["session.start"] {
		t.Fatalf("缺少 session.start，实得 %v", eventNames(sample()))
	}
	if !names["session.stop"] {
		t.Fatalf("缺少 session.stop，实得 %v", eventNames(sample()))
	}
	if names["session.upstream.lost"] || names["session.client.lost"] {
		t.Fatalf("无腿断开时不应有 lost 事件：%v", eventNames(sample()))
	}

	var stop telemetry.Event
	for _, e := range sample() {
		if e.Name == "session.stop" {
			stop = e
		}
	}
	if stop.Fields["sessionID"] == nil || stop.Fields["sessionID"] == "" {
		t.Fatalf("stop.sessionID 缺失: %v", stop.Fields)
	}
	if stop.Fields["reason"] != "user_stop" {
		t.Fatalf("stop.reason = %v", stop.Fields["reason"])
	}
	if stop.Fields["durationMs"].(int64) < 0 {
		t.Fatalf("stop.durationMs = %v", stop.Fields["durationMs"])
	}
}

// TestAcceptanceExposesShadowSeams 守卫会话装配挂上的两条影子缝：
// Seam-1 录制 tap 必须非 nil，Seam-2 上行闸门对白端帧必须放行。
//
// 这是一次真实回归的护栏（2026-09-17）：装配曾把 C2SGate 挂成 Gate.AllowB，
// 而闸门默认 sender = SenderA、未接入影子端时无人切换，AllowB 因而恒 false ——
// 白端连握手包都被吞掉，表现为所有服务器都进不去。当时 31 个包全绿，
// 因为既有测试只覆盖 AllowA/AllowB 各自的行为，没有覆盖「装配实际挂了哪个」。
// 本测试从 Accept 取真实 Acceptance，因此挂错必然变红。
// 注意：影子缝只在系统设置开启影子端时装配（TestAcceptanceWithoutShadowHasNoSeams
// 守卫关闭态），故本测试必须先 SetShadowEnabled(true)。
func TestAcceptanceExposesShadowSeams(t *testing.T) {
	svc := newEventJoinService(t)
	svc.SetShadowEnabled(true)
	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	// 取装配后真实存活的 interceptor（join() 把它存进 joinRun）。
	svc.mu.Lock()
	run := svc.runs[handle.SessionID]
	svc.mu.Unlock()
	if run == nil || run.interceptor == nil || run.interceptor.Accept == nil {
		t.Fatal("会话启动后 Accept 应已装配")
	}
	acc, err := run.interceptor.Accept(nil)
	if err != nil {
		t.Fatalf("Accept 失败: %v", err)
	}

	if acc.OnClientBoundFrameState == nil {
		t.Fatal("Seam-1 录制 tap 未装配")
	}
	if acc.C2SGate == nil {
		t.Fatal("Seam-2 上行闸门未装配")
	}

	// 白端的第一个上行帧就是握手包（id=0）；装配出来的闸门必须放行它。
	const handshakePacketID int32 = 0
	if !acc.C2SGate(handshakePacketID) {
		t.Fatal("白端握手包被闸门吞掉：主链路的 C2SGate 必须对白端帧放行" +
			"（挂 Gate.AllowA 而非 AllowB —— AllowB 在 sender=A 时恒 false）")
	}
	for _, id := range []int32{5, 13, 18} {
		if !acc.C2SGate(id) {
			t.Fatalf("白端 C2S 帧 id=%d 被闸门吞掉", id)
		}
	}
}

// TestAcceptanceWithoutShadowHasNoSeams 守卫影子端关闭（系统设置默认）的会话
// 不得装配任何影子缝：Seam-1 录制 tap 与 Seam-2 上行闸门都必须为 nil，
// joinRun.shadow 不得持有录制器 —— 不用影子端就不录制，零录制开销。
// （2026-09-23 用户反馈：明明不用影子端还在录制。）
func TestAcceptanceWithoutShadowHasNoSeams(t *testing.T) {
	svc := newEventJoinService(t)
	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	svc.mu.Lock()
	run := svc.runs[handle.SessionID]
	svc.mu.Unlock()
	if run == nil || run.interceptor == nil || run.interceptor.Accept == nil {
		t.Fatal("会话启动后 Accept 应已装配")
	}
	acc, err := run.interceptor.Accept(nil)
	if err != nil {
		t.Fatalf("Accept 失败: %v", err)
	}

	if acc.OnClientBoundFrameState != nil {
		t.Fatal("影子端关闭时 Seam-1 录制 tap 不得装配（不录制）")
	}
	if acc.C2SGate != nil {
		t.Fatal("影子端关闭时 Seam-2 上行闸门不得装配")
	}
	if run.shadow != nil {
		t.Fatal("影子端关闭时 joinRun.shadow 应为 nil（无录制器/闸门/屏蔽器）")
	}
}

// countingAssets 记录 EnsureJoinAssets 调用次数，用于钉住仅代理路径零资产补齐。
type countingAssets struct{ calls int }

func (c *countingAssets) EnsureJoinAssets(context.Context, AssetRequest) (AssetResult, error) {
	c.calls++
	return AssetResult{ClientDir: "stub-client"}, nil
}

// TestJoinProxyOnlyDoesNotProvisionAssets 仅代理不得触发资产补齐：客户端是用户
// 自备的，只起代理不该替他下载客户端本体/组件（2026-09-23 确认语义）。
// 防回归：该路径曾无条件前置 EnsureJoinAssets，全新机器上点"仅代理"会把
// 750MB 级客户端本体与该服组件全拉下来才放行。
func TestJoinProxyOnlyDoesNotProvisionAssets(t *testing.T) {
	svc := newEventJoinService(t)
	assets := &countingAssets{}
	svc.SetAssetProvider(assets)

	handle, err := svc.JoinProxyOnly(context.Background(), "env-1", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("仅代理不应依赖资产补齐: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	if assets.calls != 0 {
		t.Fatalf("仅代理会话调用了资产补齐 %d 次，应为 0（用户自备客户端，不该替他下载）", assets.calls)
	}
}

// TestStopWithoutSessionPublishesNothing 无会话时 Stop 返回 ErrNoSession 且不发事件。
func TestStopWithoutSessionPublishesNothing(t *testing.T) {
	svc := newEventJoinService(t)
	sample := collectSessionEvents(t, svc.events)

	if err := svc.Stop("sess-does-not-exist"); err == nil || !strings.Contains(err.Error(), "无会话") {
		t.Fatalf("应返回 ErrNoSession，实得 %v", err)
	}
	if got := sample(); len(got) != 0 {
		t.Fatalf("无会话不应发事件：%v", eventNames(got))
	}
}

// TestStartEventFields 启动事件字段契约（EVENTS.md §1）：connID/serverID/gameID，
// 且晚于 ATTACH 迁移发布。
func TestStartEventFields(t *testing.T) {
	svc := newEventJoinService(t)
	sample := collectSessionEvents(t, svc.events)

	handle, err := svc.JoinProxyOnly(context.Background(), "", "srv-1", "acc-1")
	if err != nil {
		t.Fatalf("JoinProxyOnly 失败: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop(handle.SessionID) })

	var start telemetry.Event
	var sawAttach bool
	for _, e := range sample() {
		switch e.Name {
		case "session.stage":
			if e.Fields["to"] == "ATTACH" {
				sawAttach = true
			}
		case "session.start":
			start = e
			if !sawAttach {
				t.Fatal("session.start 应晚于 ATTACH 迁移发布")
			}
		}
	}
	if start.Name == "" {
		t.Fatal("缺少 session.start")
	}
	if start.Result != "ok" {
		t.Fatalf("start.result = %q", start.Result)
	}
	if start.Fields["serverID"] != "srv-1" || start.Fields["gameID"] != "4661334467366178884" {
		t.Fatalf("start 字段不符：%v", start.Fields)
	}
}

func eventNames(events []telemetry.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Name)
	}
	return out
}

// fakeJoiner 按预定错误序列应答（序列耗尽后成功），验证 mirrorJoiner 的 0xff 处置。
type fakeJoiner struct {
	errs  []error
	calls int
}

func (f *fakeJoiner) OnEncryptionRequest(*relay.Conn, relay.EncryptionRequestData) ([]byte, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return nil, f.errs[i]
	}
	return []byte("secret"), nil
}

func newRejoinFixture(t *testing.T, inner relay.Joiner, relogin func() bool) (*mirrorJoiner, func() []telemetry.Event) {
	t.Helper()
	bus := telemetry.NewBus()
	var got []telemetry.Event
	cancel := bus.Subscribe("session.", func(e telemetry.Event) { got = append(got, e) })
	t.Cleanup(cancel)
	j := &mirrorJoiner{inner: inner, onOK: func() {}, relogin: relogin, events: bus, connID: "c-rejoin"}
	return j, func() []telemetry.Event { return got }
}

// TestMirrorJoinerRejoinsOnSessionDead 0xff → 重登成功 → 重试通过，
// 发布一条 result=ok 的 session.rejoin。
func TestMirrorJoinerRejoinsOnSessionDead(t *testing.T) {
	inner := &fakeJoiner{errs: []error{netease.ErrSessionDead}}
	relogins := 0
	j, sample := newRejoinFixture(t, inner, func() bool { relogins++; return true })

	secret, err := j.OnEncryptionRequest(nil, relay.EncryptionRequestData{})
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if string(secret) != "secret" {
		t.Fatalf("secret = %q", secret)
	}
	if relogins != 1 {
		t.Fatalf("重登次数 = %d, 期望 1", relogins)
	}
	if inner.calls != 2 {
		t.Fatalf("join 尝试次数 = %d, 期望 2（原次+重试一次）", inner.calls)
	}
	events := sample()
	if len(events) != 1 || events[0].Name != "session.rejoin" {
		t.Fatalf("应只发一条 session.rejoin：%v", eventNames(events))
	}
	if events[0].Fields["result"] != "ok" || events[0].Fields["reason"] != "0xff" {
		t.Fatalf("rejoin 字段不符：%v", events[0].Fields)
	}
}

// TestMirrorJoinerRejoinFailKeepsOriginalError 重登失败 → result=fail，
// 向上保留原 0xff 错误（S1：不吞错、可翻译），且只尝试一次 join。
func TestMirrorJoinerRejoinFailKeepsOriginalError(t *testing.T) {
	inner := &fakeJoiner{errs: []error{netease.ErrSessionDead}}
	j, sample := newRejoinFixture(t, inner, func() bool { return false })

	_, err := j.OnEncryptionRequest(nil, relay.EncryptionRequestData{})
	if !errors.Is(err, netease.ErrSessionDead) {
		t.Fatalf("应保留原 0xff 错误，实得 %v", err)
	}
	events := sample()
	if len(events) != 1 || events[0].Fields["result"] != "fail" {
		t.Fatalf("应发一条 result=fail 的 rejoin：%v", eventNames(events))
	}
	if inner.calls != 1 {
		t.Fatalf("重登失败不得重试，join 次数 = %d", inner.calls)
	}
}

// TestMirrorJoinerPassesThroughOtherErrors 非 0xff 错误直通，不发 rejoin。
func TestMirrorJoinerPassesThroughOtherErrors(t *testing.T) {
	inner := &fakeJoiner{errs: []error{netease.ErrJoinRejected}}
	j, sample := newRejoinFixture(t, inner, func() bool { t.Fatal("非 0xff 不得触发重登"); return false })

	if _, err := j.OnEncryptionRequest(nil, relay.EncryptionRequestData{}); !errors.Is(err, netease.ErrJoinRejected) {
		t.Fatalf("应直通原错误，实得 %v", err)
	}
	if got := sample(); len(got) != 0 {
		t.Fatalf("非 0xff 不应发事件：%v", eventNames(got))
	}
}

// TestMirrorJoinerSuccessNoRejoin 一次通过不发 rejoin。
func TestMirrorJoinerSuccessNoRejoin(t *testing.T) {
	j, sample := newRejoinFixture(t, &fakeJoiner{}, func() bool { t.Fatal("成功路径不得重登"); return false })
	if _, err := j.OnEncryptionRequest(nil, relay.EncryptionRequestData{}); err != nil {
		t.Fatalf("应成功: %v", err)
	}
	if got := sample(); len(got) != 0 {
		t.Fatalf("成功路径不应发事件：%v", eventNames(got))
	}
}
