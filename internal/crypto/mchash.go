package crypto

import (
	"crypto/sha1"
	"math/big"
)

// MinecraftAuthDigest 计算 Minecraft Java 规范的两个补码 SHA1 散列字符串（wiki.vg/Protocol_Encryption）。
// 负数以 "-" 开头，去除前导零。
func MinecraftAuthDigest(serverID string, secret, publicKey []byte) string {
	h := sha1.New()
	_, _ = h.Write([]byte(serverID))
	_, _ = h.Write(secret)
	_, _ = h.Write(publicKey)
	sum := h.Sum(nil)

	return FormatMinecraftDigest(sum)
}

// FormatMinecraftDigest 将 20 字节 SHA-1 摘要格式化为 Minecraft 官方 BigInteger.toString(16) 字符串。
func FormatMinecraftDigest(sum []byte) string {
	bi := new(big.Int)
	if len(sum) > 0 && (sum[0]&0x80) != 0 {
		// 负数（最高位为 1）：按补码取反加一求绝对值，前缀 "-"
		neg := make([]byte, len(sum))
		copy(neg, sum)
		carry := true
		for i := len(neg) - 1; i >= 0; i-- {
			neg[i] = ^neg[i]
			if carry {
				neg[i]++
				carry = neg[i] == 0
			}
		}
		bi.SetBytes(neg)
		return "-" + bi.Text(16)
	}
	bi.SetBytes(sum)
	return bi.Text(16)
}
