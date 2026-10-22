package crypto

import (
	"bytes"
	"testing"
)

// 结构性验证：facts §5.2 的输出形状（17 字符 = base64(12B) + "1"，'+'→'m'、'/'→'o'）
// 与确定性。facts 未给已知向量，真值核对依赖 L3 真机。
func TestComputeGatewayHeadersShape(t *testing.T) {
	h := ComputeGatewayHeaders("933874987", "some-token", "/item/query/available", []byte(`{"a":1}`))

	if h.UserID != "933874987" {
		t.Fatalf("UserID = %q", h.UserID)
	}
	if len(h.UserToken) != 17 {
		t.Fatalf("UserToken 长度 = %d, want 17", len(h.UserToken))
	}
	if h.UserToken[16] != '1' {
		t.Fatalf("UserToken 末位 = %q, want '1'", h.UserToken[16])
	}
	for i := 0; i < 16; i++ {
		if c := h.UserToken[i]; c == '+' || c == '/' {
			t.Fatalf("UserToken 含未替换字符 %q", c)
		}
	}
}

func TestComputeGatewayHeadersDeterministic(t *testing.T) {
	a := ComputeGatewayHeaders("u1", "tok", "/p", []byte("body"))
	b := ComputeGatewayHeaders("u1", "tok", "/p", []byte("body"))
	if a.UserToken != b.UserToken {
		t.Fatal("相同输入得到不同 token")
	}
	c := ComputeGatewayHeaders("u1", "tok", "/p2", []byte("body"))
	if a.UserToken == c.UserToken {
		t.Fatal("不同 path 得到相同 token")
	}
}

func TestComputeGatewayHeadersNormalizesPath(t *testing.T) {
	withSlash := ComputeGatewayHeaders("u", "t", "/p", nil)
	noSlash := ComputeGatewayHeaders("u", "t", "p", nil)
	if !bytes.Equal([]byte(withSlash.UserToken), []byte(noSlash.UserToken)) {
		t.Fatalf("path 归一化不一致: %q vs %q", withSlash.UserToken, noSlash.UserToken)
	}
}
