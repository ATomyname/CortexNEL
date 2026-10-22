// 启动器 -DToken 参数的取值加密。
//
// 真源：Reference/Fantnel-master/Fantnel-master/Nirvana.WPFLauncher/Utils/Cipher/TokenUtil.cs。
// 客户端用同一把硬编码密钥解密，因此密钥、IV、填充方式与明文布局都由客户端决定，
// 不可改写：8 位随机大写字符 + userToken + 8 位随机大写字符，
// AES-128-CBC / 零填充，输出大写十六进制。
//
// 这层加密只让 token 不以明文出现在进程命令行里；密钥随客户端公开，
// 不构成保密强度，调用方不得据此认为 token 已被保护。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const (
	// launcherTokenKey / launcherTokenIV 是客户端约定的固定密钥与 IV。
	launcherTokenKey = "debbde3548928fab"
	launcherTokenIV  = "afd4c5c5a7c456a1"
	// launcherTokenAffixLen 是明文两端随机填充的长度。
	launcherTokenAffixLen = 8
	// launcherTokenAlphabet 是随机填充的字符集。
	// 真源取大小写混排字符表随机后整体转大写，折叠结果即本表；
	// 折叠带来的字母权重偏斜没有协议含义——这 8 个字符由客户端丢弃，
	// 只有长度与字符范围需要一致。
	launcherTokenAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// EncryptLauncherToken 把网易账号 token 加密为 -DToken 的取值。
// 密钥不可用时返回空串，调用方据此跳过该参数而不是写出半成品。
func EncryptLauncherToken(userToken string) string {
	plain := []byte(randomTokenAffix() + userToken + randomTokenAffix())
	// 零填充：明文长度已是整块时不补（与 .NET PaddingMode.Zeros 行为一致）。
	if pad := (aes.BlockSize - len(plain)%aes.BlockSize) % aes.BlockSize; pad > 0 {
		plain = append(plain, make([]byte, pad)...)
	}

	block, err := aes.NewCipher([]byte(launcherTokenKey))
	if err != nil {
		return ""
	}
	ciphertext := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, []byte(launcherTokenIV)).CryptBlocks(ciphertext, plain)

	return strings.ToUpper(hex.EncodeToString(ciphertext))
}

// randomTokenAffix 生成一段随机填充。
// 随机源不可用时返回定长占位串：这段填充不参与鉴权，取值可预测不构成安全问题，
// 但长度必须正确，否则客户端截取 token 时会错位。
func randomTokenAffix() string {
	buf := make([]byte, launcherTokenAffixLen)
	if _, err := rand.Read(buf); err != nil {
		return strings.Repeat(launcherTokenAlphabet[:1], launcherTokenAffixLen)
	}
	out := make([]byte, launcherTokenAffixLen)
	for i, b := range buf {
		out[i] = launcherTokenAlphabet[int(b)%len(launcherTokenAlphabet)]
	}
	return string(out)
}
