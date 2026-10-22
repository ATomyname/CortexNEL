// custompayload_test.go — B 腿 CustomPayload 屏蔽的契约测试。
//
// 关注两类错误，其中第二类是本文件存在的主要理由：
//  1. 该屏蔽的没屏蔽（B 的 mod 握手漏到上游）；
//  2. **屏蔽静默失效** —— 过滤器装错位置/版本号集合为空时判定恒为放行，
//     程序不报错、日志不响，直到 B 在线被踢才发现。这是最贵的失败模式。
package shadow

import "testing"

// 763 play 态的真实包号组合（与 heypixel 适配包注入的一致）。
func testPayloadIDs() CustomPayloadIDs {
	return CustomPayloadIDs{
		ServerBound: []int32{13, 2}, // play C2S 0x0D / 766 play C2S 0x02
		ClientBound: []int32{24, 2}, // play S2C 0x18 / config S2C 0x02
	}
}

func TestCustomPayloadFilterBlocksInjectedIDs(t *testing.T) {
	f := NewCustomPayloadFilter(testPayloadIDs())

	// 屏蔽：注入的包号两个方向都不得放行。
	for _, id := range []int32{13, 2} {
		if f.AllowServerBound(id) {
			t.Errorf("ServerBound id=%d 应被屏蔽，实际放行", id)
		}
	}
	for _, id := range []int32{24, 2} {
		if f.AllowClientBound(id) {
			t.Errorf("ClientBound id=%d 应被屏蔽，实际放行", id)
		}
	}

	// 放行：未注入的包号必须原样通过（否则正常游戏包被吞，B 画面全黑）。
	for _, id := range []int32{0, 1, 15, 31, 40, 92} {
		if !f.AllowServerBound(id) {
			t.Errorf("ServerBound id=%d 不应被屏蔽", id)
		}
	}
	for _, id := range []int32{0, 1, 15, 28, 31, 92} {
		if !f.AllowClientBound(id) {
			t.Errorf("ClientBound id=%d 不应被屏蔽", id)
		}
	}
}

// TestCustomPayloadFilterDirectionsAreIndependent 两个方向互不影响：
// 只屏蔽 clientbound 时不得连带吞掉 serverbound（方向搞反会让 B 发不出任何动作）。
func TestCustomPayloadFilterDirectionsAreIndependent(t *testing.T) {
	f := NewCustomPayloadFilter(CustomPayloadIDs{ClientBound: []int32{24}})

	if f.AllowClientBound(24) {
		t.Error("ClientBound 24 应被屏蔽")
	}
	if !f.AllowServerBound(24) {
		t.Error("ServerBound 24 不应被屏蔽（该方向未注入）")
	}
}

// TestCustomPayloadFilterIgnoresNegativeSentinels 负数哨兵值不得进集合。
//
// 未核对版本时调用方会传 -1（见 heypixel 的「待核对」标注）。若 -1 被收进集合，
// 它不会命中任何真实包号，看似无害 —— 但它会让 len(set)>0 从而「看起来启用了屏蔽」，
// 掩盖「其实一个真实包号都没注入」的事实。这里要求必须彻底忽略。
func TestCustomPayloadFilterIgnoresNegativeSentinels(t *testing.T) {
	f := NewCustomPayloadFilter(CustomPayloadIDs{
		ServerBound: []int32{-1},
		ClientBound: []int32{-1},
	})
	if !f.AllowServerBound(13) {
		t.Error("仅注入 -1 时不应屏蔽任何真实包号")
	}
	if !f.AllowClientBound(24) {
		t.Error("仅注入 -1 时不应屏蔽任何真实包号")
	}
	if sb, cb := f.Stats(); sb != 0 || cb != 0 {
		t.Errorf("仅注入 -1 时不应产生屏蔽计数，实际 %d/%d", sb, cb)
	}
}

// TestCustomPayloadFilterNilAndEmptyAreTransparent nil / 空集合必须是完全透明的。
// 本包协议中立（P1），默认状态就该是「不屏蔽」。
func TestCustomPayloadFilterNilAndEmptyAreTransparent(t *testing.T) {
	var nilF *CustomPayloadFilter
	if !nilF.AllowServerBound(13) || !nilF.AllowClientBound(24) {
		t.Error("nil 过滤器必须全放行")
	}
	if sb, cb := nilF.Stats(); sb != 0 || cb != 0 {
		t.Errorf("nil 过滤器计数应为 0，实际 %d/%d", sb, cb)
	}

	empty := NewCustomPayloadFilter(CustomPayloadIDs{})
	if !empty.AllowServerBound(13) || !empty.AllowClientBound(24) {
		t.Error("空集合过滤器必须全放行")
	}
	if sb, cb := empty.Stats(); sb != 0 || cb != 0 {
		t.Errorf("空集合过滤器计数应为 0，实际 %d/%d", sb, cb)
	}
}
