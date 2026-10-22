package netease

import (
	"bytes"
	"encoding/binary"
	"testing"

	"nexus/internal/crypto"
)

func TestAuthTokenXOR(t *testing.T) {
	c := Credentials{UserID: 933874987, Token: "0123456789abcdef"}
	got := c.AuthToken()
	if len(got) != 16 {
		t.Fatalf("长度 = %d", len(got))
	}
	for i, b := range []byte("0123456789abcdef") {
		if got[i] != b^TokenKey[i] {
			t.Fatalf("第 %d 字节 = %02x, want %02x", i, got[i], b^TokenKey[i])
		}
	}
	// 长 token 循环 XOR。
	long := Credentials{Token: "0123456789abcdefXYZ"}
	gotLong := long.AuthToken()
	if gotLong[16] != 'X'^TokenKey[0] {
		t.Fatal("循环 XOR 错误")
	}
}

func TestDeriveChaChaKeysTruncation(t *testing.T) {
	// facts §2：c2s=(authToken‖loginSeed)[:32]，s2c=(loginSeed‖authToken)[:32]。
	token := bytes.Repeat([]byte{0x11}, 20)
	seed := bytes.Repeat([]byte{0x22}, 16)
	c2s, s2c := DeriveChaChaKeys(token, seed)
	if len(c2s) != 32 || len(s2c) != 32 {
		t.Fatalf("密钥长度 c2s=%d s2c=%d", len(c2s), len(s2c))
	}
	// facts §2：截断 [:32]——token 20B + seed 前 12B。
	if !bytes.Equal(c2s, append(append([]byte{}, token...), seed[:12]...)) {
		t.Fatal("c2s 组合/截断错误")
	}
	if !bytes.Equal(s2c, append(append([]byte{}, seed...), token[:16]...)) {
		t.Fatal("s2c 组合/截断错误")
	}
}

func TestBuildSignLayout(t *testing.T) {
	// facts §3.5：authId(4B LE) ‖ seed(16B) ‖ 盒子版本 ‖ Channel ‖ CrcSalt ‖ MC 版本 ‖ McVersionSalt。
	seed := bytes.Repeat([]byte{0x33}, 16)
	got := BuildSign(MCVersion, 0x01020304, seed)

	w := 4 + 16 + len(LauncherVersion) + len(Channel) + len(CrcSalt) + len(MCVersion) + len(McVersionSalt)
	if len(got) != w {
		t.Fatalf("长度 = %d, want %d", len(got), w)
	}
	if binary.LittleEndian.Uint32(got[0:4]) != 0x01020304 {
		t.Fatal("authId 不是 LE")
	}
	if !bytes.Equal(got[4:20], seed) {
		t.Fatal("seed 段错位")
	}
	off := 20
	if string(got[off:off+len(LauncherVersion)]) != LauncherVersion {
		t.Fatal("盒子版本段错位")
	}
	off += len(LauncherVersion)
	if string(got[off:off+len(Channel)]) != Channel {
		t.Fatal("Channel 段错位")
	}
	off += len(Channel)
	if string(got[off:off+len(CrcSalt)]) != CrcSalt {
		t.Fatal("CrcSalt 段错位")
	}
	off += len(CrcSalt)
	if string(got[off:off+len(MCVersion)]) != MCVersion {
		t.Fatal("MC 版本段错位")
	}
	off += len(MCVersion)
	if !bytes.Equal(got[off:off+len(McVersionSalt)], McVersionSalt) {
		t.Fatal("McVersionSalt 段错位（必须是 facts 的 01 44 50 A5 ...，而非 Nirvana 旧盐）")
	}
}

func TestDeriveSeed(t *testing.T) {
	// seed = AES-ECB(authToken, loginSeed)：ECB 可逆，解回应得 loginSeed。
	seed := bytes.Repeat([]byte{0x44}, 16)
	token := bytes.Repeat([]byte{0x55}, 16)
	enc, err := DeriveSeed(token, seed)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := crypto.ECBDecrypt(token, enc)
	if err != nil || !bytes.Equal(dec, seed) {
		t.Fatalf("ECB roundtrip: %v", err)
	}
}

// TestSkip32AgainstRealClient 用对真客户端 native 验证过的向量校验 authId 密钥路径。
//
// 向量来源与原取证方法：2026-08-29 用假 JNIEnv 直接调用真客户端 authlib 的导出函数
// Skip32(boolean, byte[], int)，与参考实现逐值比对 —— 即对照真客户端二进制而非对照代码
// （当时记录在 helmd-cases/2026-08-29-yggdrasil-join-crcsalt-internal-netease-/
// evidence/E-010-skip32-verified-against-client.txt；该取证目录后续已从仓库移出）。
//
// 取证文件已不在仓库内，故下列三对向量即为持久化后的事实来源本身：
// 任何对 skip32 算法或 Skip32Key 的改动若使其失败，即为回归，不得改测试值绕过。
// 原位于 internal/crypto；密钥单源归位 netease（facts §1）后随迁至此。
func TestSkip32AgainstRealClient(t *testing.T) {
	cases := []struct {
		name  string
		value uint32
		want  uint32
	}{
		{"zero", 0, 0x4854CDCB},
		{"one", 1, 0xE3B821EB},
		{"userId-933888811", 933888811, 0x466AF40F},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := crypto.Skip32Encrypt(tc.value, Skip32Key)
			if err != nil {
				t.Fatalf("Skip32Encrypt: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Skip32(%d) = 0x%08X, want 0x%08X", tc.value, got, tc.want)
			}
		})
	}
}
