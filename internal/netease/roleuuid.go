// 角色 UUID 派生（facts §2 Skip32 密钥单源）。
// 派生规则对照白名单参考实现 Reference/Fantnel-master/Fantnel-master/
// Nirvana.Cipher/Cipher/Nirvana/Skip32Cipher.cs 的 GenerateRoleUuid。
// 算法实现单源在 internal/crypto，本文件只持有协议约定与密钥装配。
package netease

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"

	"nexus/internal/crypto"
)

// RoleUUID 按网易客户端约定派生角色 UUID，返回 32 位小写 hex（无连字符，
// 即 Minecraft `--uuid` 期望的形式）：
//
//  1. md5(roleName) 得 16B 散列；
//  2. Skip32(userId, Skip32Key) 以小端覆盖写入第 12..15 字节；
//  3. 置 UUID version 4（第 6 字节高半字节 = 4）；
//  4. 置 RFC4122 variant（第 8 字节高两位 = 10）。
func RoleUUID(roleName string, userID uint32) (string, error) {
	sum := md5.Sum([]byte(roleName))
	raw := sum[:]

	encoded, err := crypto.Skip32Encrypt(userID, Skip32Key)
	if err != nil {
		return "", err
	}
	binary.LittleEndian.PutUint32(raw[12:16], encoded)

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw), nil
}
