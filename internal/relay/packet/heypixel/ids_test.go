// ids_test.go — 职责: 逐版本包号查表的契约测试。
//
// 为什么必须有：这些查表是「版本知识」的唯一落点，取错不会编译失败、
// 也不会让主链路报错，而是**静默失效**（录制识别不到世界锚点、闸门兜底不生效、
// CustomPayload 屏蔽漏帧）。2026-09-17 的影子端故障正是 766 未登记所致。
package heypixel

import (
	"testing"

	"nexus/internal/relay"
)

// TestRecordAnchorPacketIDKnownVersions 已登记版本必须返回真实锚点号，
// 未登记版本返回 -1（调用方据此退化为纯 FIFO，不误裁剪）。
func TestRecordAnchorPacketIDKnownVersions(t *testing.T) {
	cases := []struct {
		version int32
		want    int32
	}{
		{763, idPlayJoinGameClientbound1201},
		{764, idPlayJoinGameClientbound1201},
		{765, idPlayJoinGameClientbound1201},
		{766, idPlayJoinGameClientbound1206},
		{767, idPlayJoinGameClientbound1206},
		{768, idPlayJoinGameClientbound1206},
	}
	for _, c := range cases {
		if got := RecordAnchorPacketID(c.version); got != c.want {
			t.Errorf("RecordAnchorPacketID(%d) = %#x，期望 %#x", c.version, got, c.want)
		}
	}
	if got := RecordAnchorPacketID(999); got != -1 {
		t.Errorf("未登记版本应返回 -1，实得 %#x", got)
	}
}

// TestRecordAnchorPacketIDNotNegativeForLiveVersions 主链路实测版本不得返回 -1。
//
// 这是 2026-09-17 故障的直接护栏：白端实测 protocol=766，而查表只覆盖 763~765，
// 于是 anchor=-1 → 录制永不识别世界锚点（anchorIdx 恒为 -1）→ recorderReady=False
// → 影子端回灌被拒（"快照不以锚点开头"）。
func TestRecordAnchorPacketIDNotNegativeForLiveVersions(t *testing.T) {
	for _, v := range []int32{763, 764, 765, 766, 767, 768} {
		if got := RecordAnchorPacketID(v); got < 0 {
			t.Errorf("版本 %d 是主链路在用版本，锚点不得为 -1（录制会失去世界基准）", v)
		}
	}
}

// TestKeepAlivePacketIDsKnownVersions KeepAlive 查表按版本分段。
func TestKeepAlivePacketIDsKnownVersions(t *testing.T) {
	cases := []struct {
		version     int32
		clientbound int32
		serverbound int32
	}{
		{763, idPlayKeepAliveClientbound, idPlayKeepAliveServerbound},
		{765, idPlayKeepAliveClientbound, idPlayKeepAliveServerbound},
		{766, idPlayKeepAliveClientbound766, idPlayKeepAliveServerbound766},
		{768, idPlayKeepAliveClientbound766, idPlayKeepAliveServerbound766},
	}
	for _, c := range cases {
		gotC, gotS := KeepAlivePacketIDs(c.version)
		if gotC != c.clientbound || gotS != c.serverbound {
			t.Errorf("KeepAlivePacketIDs(%d) = (%#x,%#x)，期望 (%#x,%#x)",
				c.version, gotC, gotS, c.clientbound, c.serverbound)
		}
	}
	if c, s := KeepAlivePacketIDs(999); c != -1 || s != -1 {
		t.Errorf("未登记版本应返回 (-1,-1)，实得 (%#x,%#x)", c, s)
	}
}

// TestVersionTablesAreDistinct 766 与 763 的包号确实不同 —— 防止有人
// 把两段查表「合并简化」成一个常量（那会让其中一个版本静默错位）。
func TestVersionTablesAreDistinct(t *testing.T) {
	if idPlayJoinGameClientbound1201 == idPlayJoinGameClientbound1206 {
		t.Error("763 与 766 的 JoinGame 号不应相同（真源分别为 0x28 / 0x2b）")
	}
	if idPlayKeepAliveClientbound == idPlayKeepAliveClientbound766 {
		t.Error("763 与 766 的 KeepAlive S2C 号不应相同（真源分别为 0x23 / 0x26）")
	}
	if idPlayKeepAliveServerbound == idPlayKeepAliveServerbound766 {
		t.Error("763 与 766 的 KeepAlive C2S 号不应相同（真源分别为 0x12 / 0x18）")
	}
}

// TestPluginMessageIDsMatchRegisteredHandlers CustomPayload 的对外取值必须
// 与注册表里实际登记的包号一致（S2C 与 C2S 双向）—— 影子端过滤器与审计
// 都按这个取值工作，不一致会让屏蔽/审计静默失效（真包未被匹配而直通）。
//
// C2S 断言是一次真实勘误的护栏（2026-09-17）：PlayC2SPluginMessageID 曾恒返回
// 763 的 0x0D，而主链路实测是 766 —— B 腿的 C2S 屏蔽因此整月静默失效；
// 注册表里 766 的 C2S 处理器也注册在错误包号（configuration 态的 2）上，永不命中。
func TestPluginMessageIDsMatchRegisteredHandlers(t *testing.T) {
	registeredS2C := map[int32]bool{}
	registeredC2S := map[int32]bool{}
	for _, e := range Packets() {
		if e.Key.State != relay.StatePlay {
			continue
		}
		switch e.Key.Dir {
		case relay.ClientBound:
			registeredS2C[e.Key.PacketID] = true
		case relay.ServerBound:
			registeredC2S[e.Key.PacketID] = true
		}
	}
	if got := PlayS2CPluginMessageID(); !registeredS2C[got] {
		t.Errorf("PlayS2CPluginMessageID() = %d，但注册表未登记该 S2C 包号；"+
			"影子端屏蔽与 s2cevent 捕获会静默失效（已登记：%v）", got, registeredS2C)
	}
	if got := PlayC2SPluginMessageID(); !registeredC2S[got] {
		t.Errorf("PlayC2SPluginMessageID() = %d，但注册表未登记该 C2S 包号；"+
			"B 腿的 C2S 屏蔽会静默失效（已登记：%v）", got, registeredC2S)
	}
	// 766 的取值必须与真源一致（PrismarineJS 1.20.5 play 表）。
	if got := PlayC2SPluginMessageID(); got != 18 { // 0x12
		t.Errorf("PlayC2SPluginMessageID() = %d，期望 18（0x12）；错值会让屏蔽按错误版本工作", got)
	}
}
