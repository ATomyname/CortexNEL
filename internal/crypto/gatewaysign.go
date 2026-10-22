// x19 网关请求头签名（facts/NETEASE-PROTOCOL.md §5.2）。
// 返回的 user-id / user-token 头用于 gateway 与 interconn 的全部 HTTP 请求。
package crypto

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// gatewaySalt 是 Compute 头签名的固定盐（facts §1）。
const gatewaySalt = "0eGsBkhl"

// GatewayHeaders 是 x19 网关请求所需的签名头。
type GatewayHeaders struct {
	UserID    string
	UserToken string
}

// signSeed 按通用段序拼接待哈希缓冲（md5hex(token) ‖ body ‖ salt ‖ path）。
func signSeed(tokenMd5Hex string, body []byte, path string) []byte {
	seed := []byte(tokenMd5Hex)
	seed = append(seed, body...)
	seed = append(seed, gatewaySalt...)
	seed = append(seed, path...)
	return seed
}

// ComputeGatewayHeaders 按 facts §5.2 派生网关签名头（通用序）。
// requestPath 必须以 / 开头（与参考实现一致，缺失时自动补）。
func ComputeGatewayHeaders(userID, userToken, requestPath string, body []byte) GatewayHeaders {
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	buf := md5.Sum([]byte(userToken))
	seed := signSeed(hex.EncodeToString(buf[:]), body, requestPath)
	return shapeToken(seed, userID)
}

// shapeToken 对 MD5 摘要做位旋转整形（参考实现 ProcessBinaryBlock）。
func shapeToken(seed []byte, userID string) GatewayHeaders {
	digest := md5.Sum(seed)
	digestHex := hex.EncodeToString(digest[:])
	// 参考实现对 MD5 十六进制字符串的 ASCII 字节逐字节展开 8 bit。
	var bits strings.Builder
	for i := 0; i < len(digestHex); i++ {
		writeByteBits(&bits, digestHex[i])
	}
	rotated := bits.String()[6:] + bits.String()[:6]

	out := []byte(digestHex)
	for block := 0; block*8 < len(rotated); block++ {
		var xor byte
		// 块内低位在前：第 j 位取块内倒序字符（参考实现 ProcessBinaryBlock）。
		for j := 0; j < 8 && block*8+j < len(rotated); j++ {
			if rotated[block*8+7-j] == '1' {
				xor |= 1 << j
			}
		}
		out[block] ^= xor
	}

	token := base64.StdEncoding.EncodeToString(out[:12]) + "1"
	token = strings.ReplaceAll(token, "+", "m")
	token = strings.ReplaceAll(token, "/", "o")

	return GatewayHeaders{UserID: userID, UserToken: token}
}

func writeByteBits(bits *strings.Builder, c byte) {
	for i := 7; i >= 0; i-- {
		bits.WriteByte('0' + (c>>i)&1)
	}
}
