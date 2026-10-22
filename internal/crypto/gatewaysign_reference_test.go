package crypto

import (
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// refCalculateDynamicToken 是参考实现 CalculateDynamicToken 的逐字移植
// （OpenSource-AuthService modules/g79client/crypto.go，E-015），仅作对拍 oracle。
// /authentication-v2 得 401 时用它裁决 shapeToken 是否与参考逐字节一致。
func refCalculateDynamicToken(path, content, token string) string {
	salt := "0eGsBkhl"
	tokenMd5 := fmt.Sprintf("%x", md5.Sum([]byte(token)))
	payload := tokenMd5 + content + salt + strings.TrimSuffix(path, "?")
	payloadMd5 := fmt.Sprintf("%x", md5.Sum([]byte(payload)))
	binaryString := ""
	for _, b := range []byte(payloadMd5) {
		binaryString += fmt.Sprintf("%08b", b)
	}
	binaryString = binaryString[6:] + binaryString[:6]

	transformed := make([]byte, len(payloadMd5))
	copy(transformed, []byte(payloadMd5))
	for i := 0; i < len(transformed); i++ {
		section := binaryString[i*8 : i*8+8]
		reversedByte := byte(0)
		for j := 0; j < 8; j++ {
			if section[7-j] == '1' {
				reversedByte |= (1 << (j & 0x1F))
			}
		}
		transformed[i] = reversedByte ^ transformed[i]
	}
	b64 := base64.StdEncoding.EncodeToString(transformed)
	return strings.ReplaceAll(strings.ReplaceAll(b64[:16], "+", "m"), "/", "o") + "1"
}

// TestGatewaySignMatchesReference 对拍 ComputeGatewayHeaders 与参考
// CalculateDynamicToken：V2 线同输入必须逐字节一致，不一致即 401 根因。
func TestGatewaySignMatchesReference(t *testing.T) {
	cases := []struct{ name, token, path, body string }{
		{"V2 真实形状", "g79-token-abcdef", "/authentication-v2", `{"bit":"64","clientKey":"ck-160B","displayName":"Steve"}`},
		{"短 token 短体", "t", "/authentication-v2", `{}`},
		{"空 token 空体", "", "/authentication-v2", ""},
		{"update 路径", "pe-token-123456", "/authentication/update", `{"entity_id":"3096807021"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ComputeGatewayHeaders("3096807021", c.token, c.path, []byte(c.body)).UserToken
			want := refCalculateDynamicToken(c.path, c.body, c.token)
			if got != want {
				t.Fatalf("签名与参考不一致（=401 根因）:\n got  %q\n want %q\n(token=%q path=%q body=%q)",
					got, want, c.token, c.path, c.body)
			}
		})
	}
}
