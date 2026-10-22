package crypto

import "testing"

// TestSkip32FTableIsComplete 守卫「置换表必须是完整 256 项」这一约束。
//
// 背景：曾使用去重后仅 246 项的错表，算出的 authId 与真客户端不一致且 init 必拒
// （原记录在已移出仓库的取证目录 E-009-skip32-ftable-fix.txt）。
// 该测试把这一约束固化下来，避免错表回归。
func TestSkip32FTableIsComplete(t *testing.T) {
	if len(skip32FTable) != 256 {
		t.Fatalf("FTable must have 256 entries, got %d", len(skip32FTable))
	}
	seen := make(map[uint8]bool, len(skip32FTable))
	for _, v := range skip32FTable {
		if seen[v] {
			t.Fatalf("FTable is not a permutation: duplicate value 0x%02X", v)
		}
		seen[v] = true
	}
}

// testKey10 是算法测试用的任意 10B 密钥；
// 协议密钥（facts §1 Skip32Key）由 internal/netease 单源持有，真客户端 KAT 随迁至 netease 包。
var testKey10 = []byte("0123456789")

// TestSkip32RoundTrip 验证加解密互逆。
func TestSkip32RoundTrip(t *testing.T) {
	for _, v := range []uint32{0, 1, 2, 933888811, 0xFFFFFFFF, 0x12345678} {
		enc, err := Skip32Encrypt(v, testKey10)
		if err != nil {
			t.Fatalf("encrypt %d: %v", v, err)
		}
		dec, err := Skip32Decrypt(enc, testKey10)
		if err != nil {
			t.Fatalf("decrypt %d: %v", v, err)
		}
		if dec != v {
			t.Fatalf("round trip failed for %d: got %d", v, dec)
		}
	}
}

// TestSkip32RejectsBadKeyLen 验证非 10B 密钥被拒绝（而不是静默算错）。
func TestSkip32RejectsBadKeyLen(t *testing.T) {
	if _, err := Skip32Encrypt(1, []byte("short")); err == nil {
		t.Fatal("key shorter than 10 bytes must be rejected")
	}
	if _, err := Skip32Encrypt(1, []byte("SaintSteve!!")); err == nil {
		t.Fatal("key longer than 10 bytes must be rejected")
	}
}
