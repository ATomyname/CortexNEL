package ecpack

import (
	"bytes"
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
	"nexus/internal/telemetry"
)

// stubProvider 是可控的 Provider 替身。
type stubProvider struct {
	url, hash string
	ok        bool
}

func (s stubProvider) PackURL() (string, string, bool) { return s.url, s.hash, s.ok }

// buildPushPayload 按原版线格式构造 ClientboundResourcePackPushPacket 载荷。
func buildPushPayload(url, hash string, required bool, prompt []byte) []byte {
	w := codec.NewWriter()
	w.Fixed([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	w.String(url)
	w.String(hash)
	w.Bool(required)
	w.Fixed(prompt)
	return w.Bytes()
}

func run(t *testing.T, p relay.Packet, payload []byte) []byte {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// 日志文件在测试收尾前必须先关闭：Windows 下 TempDir 清理会因文件仍被占用而失败。
	t.Cleanup(func() { _ = log.Close() })
	conn := relay.NewConn("c-ec", &relay.Registry{}, nil, log)
	if err := p.Read(conn, codec.NewReader(payload)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := p.Handle(conn); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return w.Bytes()
}

// TestRewriteKeepsShapeAndPrompt 改写后：id/required/prompt 原样，url 与 hash 换成 nexus 的。
func TestRewriteKeepsShapeAndPrompt(t *testing.T) {
	// prompt 用「存在」编码（Optional 的 present=true + 组件字节），验证尾部原样保留。
	prompt := []byte{0x01, 0xAA, 0xBB, 0xCC}
	payload := buildPushPayload("https://ec.example/pack.zip", "c01832553ed4c22690393f72c4a5da1dd18a0cc3", true, prompt)

	prov := stubProvider{url: "http://127.0.0.1:39217/ecpack.zip", hash: "0123456789abcdef0123456789abcdef01234567", ok: true}
	out := run(t, &resourcePackPush{provider: prov}, payload)

	r := codec.NewReader(out)
	id, err := r.Fixed(16)
	if err != nil || !bytes.Equal(id, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}) {
		t.Fatalf("uuid 未原样保留: %v %v", id, err)
	}
	url, err := r.String()
	if err != nil {
		t.Fatal(err)
	}
	if url != prov.url {
		t.Fatalf("url = %q，期望改写成 %q", url, prov.url)
	}
	hash, err := r.String()
	if err != nil {
		t.Fatal(err)
	}
	if hash != prov.hash {
		t.Fatalf("hash = %q，期望 %q", hash, prov.hash)
	}
	req, err := r.Bool()
	if err != nil {
		t.Fatal(err)
	}
	if !req {
		t.Fatal("required 应原样保留为 true")
	}
	tail, err := r.Fixed(r.Remaining())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tail, prompt) {
		t.Fatalf("prompt 尾部字节未原样保留：% X，期望 % X", tail, prompt)
	}
	if r.Remaining() != 0 {
		t.Fatalf("残留 %d 字节", r.Remaining())
	}
}

// TestTolerantOnGarbage 非推送载荷（id 9 撞上的可能是任意包）必须原样透传、不得报错关连接。
func TestTolerantOnGarbage(t *testing.T) {
	payload := []byte{0x00, 0x01, 0x02, 0x03, 0xAA, 0xBB} // 不是推送形状
	out := run(t, &resourcePackPush{provider: stubProvider{ok: true}}, payload)
	if !bytes.Equal(out, payload) {
		t.Fatalf("非推送载荷被改动：% X vs % X", out, payload)
	}
	// 缺 http 的"合法形状"也不应改写。
	p2 := buildPushPayload("ftp://no-http", "x", false, []byte{0x00})
	out2 := run(t, &resourcePackPush{provider: stubProvider{url: "http://127.0.0.1:1/x", hash: "y", ok: true}}, p2)
	if !bytes.Equal(out2, p2) {
		t.Fatal("URL 不含 http 的推送不应改写")
	}
	// hash 非 40 位 sha1 也不应改写（原版允许空 hash，但 EC 实测推送恒带 sha1；
	// 此守卫用于挡 772 play 9 = boss_event 的撞包改写，见
	// TestResourcePackPushGuardsAgainstBossEventCollision）。
	p3 := buildPushPayload("http://ec.example/pack.zip", "deadbeef", false, []byte{0x00})
	out3 := run(t, &resourcePackPush{provider: stubProvider{url: "http://127.0.0.1:1/x", hash: "y", ok: true}}, p3)
	if !bytes.Equal(out3, p3) {
		t.Fatal("hash 非 sha1 的推送不应改写")
	}
}

// TestResourcePackPushGuardsAgainstBossEventCollision 守卫 772 play 9（实为
// boss_event）的撞包透传：url 含 http 但 hash 非 sha1 的载荷必须原样透传。
//
// 2026-10-05 实测事故：boss_event 载荷被本处理器按"uuid|url|hash|bool"误解析并
// 改写后，客户端按 boss_event 解析畸形帧直接崩（"fieldSize is too long!
// Length is 28530, but maximum is 292"）。真推送的 hash 恒为 40 位 sha1。
func TestResourcePackPushGuardsAgainstBossEventCollision(t *testing.T) {
	// boss_event 形状：uuid(16) | varint action | 动作数据 —— 动作数据里
	// 恰好有 "http://" 字样（血条标题含链接）+ 非 sha1 的后续字节。
	boss := codec.NewWriter()
	boss.Fixed([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	boss.VarInt(12)
	boss.Fixed([]byte("http://x.abc"))
	boss.VarInt(40)
	boss.Fixed([]byte("!!!!not-a-sha1-hash-xxxxxxxxxxxxxxxxx"))
	boss.Bool(false)
	boss.Fixed([]byte{0x01, 0x02})

	out := run(t, &resourcePackPush{provider: stubProvider{url: "http://127.0.0.1:1/x", hash: "y", ok: true}}, boss.Bytes())
	if !bytes.Equal(out, boss.Bytes()) {
		t.Fatalf("boss_event 形状载荷必须原样透传：\n got % X\nwant % X", out, boss.Bytes())
	}

	// 对照组：同样形状但 hash 为 40 位 sha1 → 是真推送，必须改写。
	real := codec.NewWriter()
	real.Fixed([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16})
	real.VarInt(12)
	real.Fixed([]byte("http://x.abc"))
	real.VarInt(40)
	real.Fixed([]byte("c01832553ed4c22690393f72c4a5da1dd18a0cc3"))
	real.Bool(false)
	real.Fixed([]byte{0x01, 0x02})
	out2 := run(t, &resourcePackPush{provider: stubProvider{url: "http://127.0.0.1:1/local.zip", hash: "y", ok: true}}, real.Bytes())
	if bytes.Equal(out2, real.Bytes()) {
		t.Fatal("sha1 hash 的真推送应被改写为本地分发")
	}
}

// TestNoProviderKeepsPayload provider 为 nil（或 ok=false）时包必须原样透传，
// 否则会把不该动的服务器资源包也改掉。
func TestNoProviderKeepsPayload(t *testing.T) {
	payload := buildPushPayload("https://other.example/pack.zip", "abc", false, []byte{0x00})
	for _, prov := range []Provider{nil, stubProvider{ok: false}} {
		out := run(t, &resourcePackPush{provider: prov}, payload)
		if !bytes.Equal(out, payload) {
			t.Fatalf("provider=%v 时包被改动：\n got % X\nwant % X", prov, out, payload)
		}
	}
}

// TestPacketsRegisteredOnlyForGivenGameIDs 只对传入的 GameID 注册；
// 未传入的服务器不得出现任何处理器（EC-only 门控）。
func TestPacketsRegisteredOnlyForGivenGameIDs(t *testing.T) {
	prov := stubProvider{ok: true}
	if got := Packets(nil, prov); len(got) != 0 {
		t.Fatalf("空 GameID 集合不应注册处理器，实得 %d 条", len(got))
	}
	if got := Packets([]string{""}, prov); len(got) != 0 {
		t.Fatalf("空串 GameID 不应注册处理器，实得 %d 条", len(got))
	}

	const ecID = "4688093584228529760"
	got := Packets([]string{ecID}, prov)
	// 三条交付处理器 + 一条临时诊断探针（通配 id，确认映射规则后删除）。
	if len(got) != 4 {
		t.Fatalf("EC 应注册 2×play 推送 + configuration 推送 + JoinGame 注入共四条，实得 %d", len(got))
	}
	var sawPlayPush, sawPush9, sawConfigPush, sawInject bool
	for _, e := range got {
		if e.Key.GameID != ecID {
			t.Fatalf("注册键未绑定 EC GameID: %+v", e.Key)
		}
		if len(e.Key.Versions) != 1 || e.Key.Versions[0] != protocol1218 {
			t.Fatalf("应只注册已实证的版本 772，实得 %v", e.Key.Versions)
		}
		if e.Key.Dir != relay.ClientBound {
			t.Fatalf("资源包是 S2C，方向应为 ClientBound：%+v", e.Key)
		}
		switch e.Key.PacketID {
		case idResourcePackPushPlay:
			sawPlayPush = true

		case 9: // play 9 与 configuration 9 是不同注册（常量同值），按状态区分
			if e.Key.State == relay.StatePlay {
				sawPush9 = true
			} else {
				sawConfigPush = true
			}
		case idJoinGamePlay:
			sawInject = true
		default:
			t.Fatalf("出现预期外的包 id：%d", e.Key.PacketID)
		}
	}
	if !sawPlayPush || !sawPush9 || !sawConfigPush || !sawInject {
		t.Fatalf("四条处理器未齐：play=%v push9=%v config=%v inject=%v", sawPlayPush, sawPush9, sawConfigPush, sawInject)
	}

	// 用注册表验证：EC 命中、别的服不命中。
	// 注意按「处理器种类」断言而不是数总数：临时探针是通配注册（PacketID=-1），
	// 会与任何具体 id 的查询同时命中，用总数断言会随探针增删而误报。
	rg := &relay.Registry{}
	rg.Register(got...)
	if n := countKey(rg.Match(relay.StatePlay, relay.ClientBound, idResourcePackPushPlay, ecID, protocol1218), idResourcePackPushPlay); n != 1 {
		t.Fatalf("EC play 态应有 1 条资源包推送处理器，实得 %d", n)
	}
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, idResourcePackPushPlay, "4661334467366178884", protocol1218); len(hits) != 0 {
		t.Fatalf("非 EC 服务器不应命中，实得 %d", len(hits))
	}
	if n := countKey(rg.Match(relay.StateConfiguration, relay.ClientBound, idResourcePackPushConfig, ecID, protocol1218), idResourcePackPushConfig); n != 1 {
		t.Fatalf("EC configuration 态应有 1 条推送处理器，实得 %d", n)
	}
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, idResourcePackPushPlay, ecID, 767); len(hits) != 0 {
		t.Fatalf("未实证版本 767 不应命中，实得 %d", len(hits))
	}
}

// countKey 统计命中里注册键为指定包 id 的条数。
func countKey(hits []relay.Registered, id int32) int {
	n := 0
	for _, h := range hits {
		if h.Key.PacketID == id {
			n++
		}
	}
	return n
}

// TestBuildPushRoundTrips 注入用载荷必须能被同一线格式读回：
// required 恒为 false（true 时玩家拒绝会被踢），prompt 缺省。
func TestBuildPushRoundTrips(t *testing.T) {
	payload, err := buildPush("http://127.0.0.1:39217/ecpack.zip", "550090b9a2a7e3753d6cb88c3cbedcb6b84e9a7f")
	if err != nil {
		t.Fatal(err)
	}
	var p resourcePackPush
	if err := p.Read(nil, codec.NewReader(payload)); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(p.id) != 16 {
		t.Fatalf("uuid 长度 = %d，期望 16", len(p.id))
	}
	if p.url != "http://127.0.0.1:39217/ecpack.zip" {
		t.Fatalf("url = %q", p.url)
	}
	if p.hash != "550090b9a2a7e3753d6cb88c3cbedcb6b84e9a7f" {
		t.Fatalf("hash = %q", p.hash)
	}
	if p.required {
		t.Fatal("注入推送必须 required=false（true 时玩家拒绝会被踢出）")
	}
	if !bytes.Equal(p.tail, []byte{0x00}) {
		t.Fatalf("prompt 应为缺省（0x00），实得 % X", p.tail)
	}

	// 两次注入的 uuid 不同：避免客户端把两条推送当成同一条。
	other, err := buildPush("http://127.0.0.1:39217/ecpack.zip", "")
	if err != nil {
		t.Fatal(err)
	}
	var q resourcePackPush
	if err := q.Read(nil, codec.NewReader(other)); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(q.id, p.id) {
		t.Fatal("两次注入 uuid 相同")
	}

	// hash 超长必须拒绝（原版字段上限 40）。
	if _, err := buildPush("http://127.0.0.1:1/x", string(make([]byte, 41))); err == nil {
		t.Fatal("超长 hash 应被拒绝")
	}
	if _, err := buildPush("", "x"); err == nil {
		t.Fatal("空 URL 应被拒绝")
	}
}

// TestPacketIDsMatchVanilla1218 把「数出来的 id」钉死，防止无声漂移。
//
// 陷阱记录：play 态 clientbound 的第一个包 CLIENTBOUND_BUNDLE 用 withBundlePacket 注册
// 而不是 addPacket，它占 id 0 —— 只数 addPacket 会整体少 1。实测代价：客户端把 73 当
// resource_pack_pop 解析我们的推送，报 "Network Protocol Error"（多出 76 字节）直接断连。
// configuration 列表没有这类偏移（custom_payload=1 与公开协议值吻合），故 Push=9。
func TestPacketIDsMatchVanilla1218(t *testing.T) {
	cases := []struct {
		name string
		got  int32
		want int32
	}{
		{"play JoinGame", idJoinGamePlay, 43},
		{"play ResourcePackPush", idResourcePackPushPlay, 74},
		{"configuration ResourcePackPush", idResourcePackPushConfig, 9},
		{"1.21.8 协议号", protocol1218, 772},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Fatalf("%s = %d，期望 %d（依据原版 1.21.8 源码逐条数出）", c.name, c.got, c.want)
		}
	}
}
