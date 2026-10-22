// 凭据派生与 init 签名（facts §2/§3.3/§3.5/§3.6）。
package netease

import (
	"crypto/sha256"
	"encoding/binary"

	"nexus/internal/crypto"
)

// Credentials 是一次网易会话的账号凭据（token 落盘受 DR-1/P5 约束，本包不持久化）。
type Credentials struct {
	UserID int64  // entity_id（facts §6.1）
	Token  string // x19 token（数十分钟寿命，facts §6.4）
}

// AuthToken = ASCII(token) XOR TokenKey（逐字节循环，facts §2）。
func (c Credentials) AuthToken() []byte {
	token := []byte(c.Token)
	out := make([]byte, len(token))
	for i, b := range token {
		out[i] = b ^ TokenKey[i%len(TokenKey)]
	}
	return out
}

// AuthID = Skip32(int32-BE(userId), "SaintSteve")（facts §2）。
func (c Credentials) AuthID() (int32, error) {
	v, err := crypto.Skip32Encrypt(uint32(c.UserID), Skip32Key)
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}

// DeriveSeed = AES-128-ECB(key=authToken, plain=loginSeed)（facts §2）。
func DeriveSeed(authToken, loginSeed []byte) ([]byte, error) {
	return crypto.ECBEncrypt(authToken, loginSeed)
}

// DeriveChaChaKeys 派生 Yggdrasil 帧加密密钥（facts §2，截断 32B）：
// c2s = (authToken ‖ loginSeed)[:32]，s2c = (loginSeed ‖ authToken)[:32]。
func DeriveChaChaKeys(authToken, loginSeed []byte) (c2s, s2c []byte) {
	combined := make([]byte, 0, len(authToken)+len(loginSeed))
	combined = append(combined, authToken...)
	combined = append(combined, loginSeed...)
	if len(combined) > 32 {
		combined = combined[:32]
	}
	c2s = append([]byte(nil), combined...)
	reverse := make([]byte, 0, len(authToken)+len(loginSeed))
	reverse = append(reverse, loginSeed...)
	reverse = append(reverse, authToken...)
	if len(reverse) > 32 {
		reverse = reverse[:32]
	}
	s2c = reverse
	return c2s, s2c
}

// BuildSign 构造 init 签名输入（facts §3.5 与 Nirvana BuildSign）：
// authId(4B LE) ‖ seed(16B) ‖ 盒子版本 ‖ Channel ‖ CrcSalt ‖ MC 版本 ‖ McVersionSalt。
func BuildSign(mcVersion string, authID int32, seed []byte) []byte {
	ver := mcVersion
	if ver == "" {
		ver = MCVersion
	}
	out := make([]byte, 0, 4+len(seed)+len(LauncherVersion)+len(Channel)+len(CrcSalt)+len(ver)+len(McVersionSalt))
	out = binary.LittleEndian.AppendUint32(out, uint32(authID))
	out = append(out, seed...)
	out = append(out, LauncherVersion...)
	out = append(out, Channel...)
	out = append(out, CrcSalt...)
	out = append(out, ver...)
	out = append(out, McVersionSalt...)
	return out
}

// BuildHashData 构造 join 哈希（facts §3.6 与 Nirvana BuildHashData）：
// sha256(joinStr ‖ int32-LE(userId) ‖ loginSeed)，
// joinStr = 盒子版本 ‖ MC 版本 ‖ timeSec ‖ CrcSalt ‖ modInfo ‖ BootstrapMd5 ‖ DatFileMd5 ‖ "[]"。
func BuildHashData(mcVersion string, userID int64, timeSec int32, loginSeed []byte, modInfo string) []byte {
	ver := mcVersion
	if ver == "" {
		ver = MCVersion
	}
	pair := GetMd5Pair(ver)
	joinStr := LauncherVersion + ver + itoa(int64(timeSec)) + CrcSalt +
		modInfo + pair.BootstrapMd5 + pair.DatFileMd5 + PrcCheck
	buf := make([]byte, 0, len(joinStr)+4+len(loginSeed))
	buf = append(buf, joinStr...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(userID))
	buf = append(buf, loginSeed...)
	sum := sha256.Sum256(buf)
	return sum[:]
}

// itoa 是十进制无格式化转换（避免 fmt 引入）。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var digits [20]byte
	i := len(digits)
	for v > 0 {
		i--
		digits[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
