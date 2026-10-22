package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"strings"
	"testing"
)

// TestEncryptLauncherTokenPlaintextLayout 解密回明文，锁定客户端约定的布局：
// 8 位填充 + token + 8 位填充。填充长度错位会让客户端截出错误的 token。
func TestEncryptLauncherTokenPlaintextLayout(t *testing.T) {
	const token = "abc123xyz"

	plain := decryptLauncherToken(t, EncryptLauncherToken(token))
	if len(plain) != launcherTokenAffixLen*2+len(token) {
		t.Fatalf("明文长度 = %d，期望 %d：%q", len(plain), launcherTokenAffixLen*2+len(token), plain)
	}
	if got := plain[launcherTokenAffixLen : launcherTokenAffixLen+len(token)]; got != token {
		t.Fatalf("夹在填充之间的 token = %q，期望 %q", got, token)
	}
	for _, affix := range []string{
		plain[:launcherTokenAffixLen],
		plain[len(plain)-launcherTokenAffixLen:],
	} {
		for _, r := range affix {
			if !strings.ContainsRune(launcherTokenAlphabet, r) {
				t.Fatalf("填充含字符集外字符 %q：%q", r, affix)
			}
		}
	}
}

// TestEncryptLauncherTokenIsRandomized 两次加密同一 token 必须不同：
// 修复前的实现用固定字符串冒充随机填充，密文因此可预测。
func TestEncryptLauncherTokenIsRandomized(t *testing.T) {
	if EncryptLauncherToken("same-token") == EncryptLauncherToken("same-token") {
		t.Fatal("两次加密结果相同，随机填充未生效")
	}
}

// TestEncryptLauncherTokenOutputShape 输出为大写十六进制且按 16 字节整块。
func TestEncryptLauncherTokenOutputShape(t *testing.T) {
	got := EncryptLauncherToken("abc123xyz")
	if got == "" {
		t.Fatal("加密结果为空")
	}
	if len(got)%(aes.BlockSize*2) != 0 {
		t.Fatalf("十六进制长度 = %d，应为 %d 的整数倍", len(got), aes.BlockSize*2)
	}
	if got != strings.ToUpper(got) {
		t.Fatalf("输出应为大写十六进制：%q", got)
	}
}

// decryptLauncherToken 用同一把密钥解密并去掉零填充，模拟客户端侧的解密。
func decryptLauncherToken(t *testing.T, encrypted string) string {
	t.Helper()

	raw, err := hex.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("密文不是合法十六进制：%v", err)
	}
	block, err := aes.NewCipher([]byte(launcherTokenKey))
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(launcherTokenIV)).CryptBlocks(plain, raw)
	return string(trimZeroPadding(plain))
}

// trimZeroPadding 去掉零填充，使断言只关注明文布局。
func trimZeroPadding(b []byte) []byte {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	return b[:end]
}
