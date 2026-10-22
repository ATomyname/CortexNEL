// transfer_test.go — 职责: 原版转服拦截契约。
//
// 守卫三件事（2026-10-05 实测故障：EC 高版本转服绕开代理裸连，客户端 DNS 解析
// "<host>\0FORGE" 失败）：
//  1. Transfer 包必须改写为本代理回连地址，真实目标剥离 FML 标记后交给编排层；
//  2. 未装配 RelayAddr 时包原样转发（不比不拦截更糟）；
//  3. 注册表必须按版本命中 772 的 play 0x7a / configuration 0x0b，且不得再命中
//     0x69（772 的 0x69 是 set_title_subtitle，旧表误注册曾让字幕包触发重配置）；
//  4. 握手 intent=3（转服回连）对内按登录、对上游归一为 2 并保留 FML 标记。
package login

import (
	"bytes"
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// encodeTransfer 构造一条 Transfer 包体（host + varint port）。
func encodeTransfer(t *testing.T, host string, port int32) []byte {
	t.Helper()
	w := codec.NewWriter()
	w.String(host)
	w.VarInt(port)
	return w.Bytes()
}

// newTransferConn 构造不带双腿的测试连接（transferPacket.Handle 不触碰腿），
// RelayAddr 由用例按需设置，recorded 捕获 OnTransferRequest 的真实目标。
func newTransferConn(t *testing.T, relayAddr string) (*relay.Conn, *[]string) {
	t.Helper()
	c := relay.NewConn("transfer-test", &relay.Registry{}, nil, testLogger(t))
	c.RelayAddr = relayAddr
	recorded := &[]string{}
	c.OnTransferRequest = func(_ *relay.Conn, addr string) { *recorded = append(*recorded, addr) }
	return c, recorded
}

func TestTransferPacketRewritesToRelayAddr(t *testing.T) {
	c, recorded := newTransferConn(t, "127.0.0.1:25565")
	p := &transferPacket{}
	payload := encodeTransfer(t, "game.example.net\x00FORGE", 25565)
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	cancel, err := p.Handle(c)
	if err != nil || cancel {
		t.Fatalf("Handle = (cancel=%v, err=%v)，应改写并转发", cancel, err)
	}
	// 真实目标必须剥离 FML 标记后递给编排层。
	if len(*recorded) != 1 || (*recorded)[0] != "game.example.net:25565" {
		t.Fatalf("OnTransferRequest = %v，要 game.example.net:25565", *recorded)
	}
	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	// 改写后 host 指向本代理、port 保持 varint 编码。
	want := encodeTransfer(t, "127.0.0.1", 25565)
	if !bytes.Equal(w.Bytes(), want) {
		t.Fatalf("改写输出 = %v，要 %v", w.Bytes(), want)
	}
}

// TestTransferPacketWithoutRelayAddrForwardsUnchanged 未装配 RelayAddr 的路径
// （如影子端 SetLegsForTest 直配的连接）必须原样转发 —— 不改写不能变成吞包。
// 真实目标仍要递给编排层记录。
func TestTransferPacketWithoutRelayAddrForwardsUnchanged(t *testing.T) {
	c, recorded := newTransferConn(t, "")
	p := &transferPacket{}
	payload := encodeTransfer(t, "game.example.net", 25566)
	if err := p.Read(c, codec.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(c); err != nil {
		t.Fatal(err)
	}
	if len(*recorded) != 1 || (*recorded)[0] != "game.example.net:25566" {
		t.Fatalf("未装配 RelayAddr 也必须记录真实目标，实得 %v", *recorded)
	}
	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.Bytes(), payload) {
		t.Fatalf("未装配 RelayAddr 应原样转发，实得 %v", w.Bytes())
	}
}

// TestRegistryMatchesTransferByVersion 用真实注册表验证 772 的命中面：
// play 0x7a 与 configuration 0x0b 命中转服处理器；0x69 不再命中任何处理器
// （它是字幕包，误命中会让 conn.State 整体错位）；start_configuration 按版本
// 命中 0x6f。
func TestRegistryMatchesTransferByVersion(t *testing.T) {
	rg := &relay.Registry{}
	rg.Register(Packets()...)

	const proto772 = int32(772)
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, 0x7a, "", proto772); len(hits) != 1 {
		t.Fatalf("772 play 0x7a 应命中转服处理器 1 个，实得 %d", len(hits))
	}
	if hits := rg.Match(relay.StateConfiguration, relay.ClientBound, 0x0b, "", proto772); len(hits) != 1 {
		t.Fatalf("772 configuration 0x0b 应命中转服处理器 1 个，实得 %d", len(hits))
	}
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, 0x69, "", proto772); len(hits) != 0 {
		t.Fatalf("772 play 0x69（set_title_subtitle）不得命中任何处理器，实得 %d", len(hits))
	}
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, StartConfigurationPacketID(proto772), "", proto772); len(hits) != 1 {
		t.Fatalf("772 play 0x6f 应命中 startConfiguration，实得 %d", len(hits))
	}
	// 低版本（766 = 1.20.5，转服包引入版本）同样命中；763（1.20.1）无此包。
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, 0x73, "", 766); len(hits) != 1 {
		t.Fatalf("766 play 0x73 应命中转服处理器，实得 %d", len(hits))
	}
	if hits := rg.Match(relay.StatePlay, relay.ClientBound, 0x73, "", 763); len(hits) != 0 {
		t.Fatalf("763 无转服包，不应命中，实得 %d", len(hits))
	}
}

func TestHandshakeTransferIntentNormalizesToLogin(t *testing.T) {
	c := relay.NewConn("handshake-test", &relay.Registry{}, nil, testLogger(t))
	p := &handshake{}
	w := codec.NewWriter()
	w.VarInt(772)
	w.String("127.0.0.1")
	w.UShort(25565)
	w.VarInt(3) // ClientIntent.TRANSFER
	if err := p.Read(c, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	cancel, err := p.Handle(c)
	if err != nil || cancel {
		t.Fatalf("Handle = (cancel=%v, err=%v)", cancel, err)
	}
	if c.State() != relay.StateLogin {
		t.Fatalf("转服回连应切到 Login 态，实得 %v", c.State())
	}
	out := codec.NewWriter()
	if err := p.Write(out); err != nil {
		t.Fatal(err)
	}
	// 上游握手 host 必须带 FML 标记（>758 → "\x00FORGE"）、nextState 归一为 2。
	want := codec.NewWriter()
	want.VarInt(772)
	want.String("127.0.0.1\x00FORGE")
	want.UShort(25565)
	want.VarInt(2)
	if !bytes.Equal(out.Bytes(), want.Bytes()) {
		t.Fatalf("上游握手 = %q，要 %q", out.Bytes(), want.Bytes())
	}
}
