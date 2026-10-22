package netease

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"testing"
)

// RSA 原始运算 roundtrip 用测试生成的密钥（内嵌密钥的真值核对依赖 L3 真机 oracle）。
func TestRawOpsRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0x5A}, 51) // clientKey(19)+sign(32) 形状

	op, err := RawPrivateOp(key, payload)
	if err != nil {
		t.Fatalf("RawPrivateOp: %v", err)
	}
	if len(op) != 128 {
		t.Fatalf("输出长度 = %d, want 128（1024bit）", len(op))
	}
	dec, err := RawPublicDecrypt(&key.PublicKey, op)
	if err != nil {
		t.Fatalf("RawPublicDecrypt: %v", err)
	}
	if !bytes.Equal(dec, payload) {
		t.Fatal("roundtrip 不一致")
	}
}

func TestRawPublicDecryptRejectsGarbage(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	// 类型 2 填充（EncryptPKCS1v15）不是类型 1：应拒绝。
	ct, err := rsa.EncryptPKCS1v15(rand.Reader, &key.PublicKey, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RawPublicDecrypt(&key.PublicKey, ct); err == nil {
		t.Fatal("类型 2 填充应被拒绝")
	}
}

// 内嵌协议身份必须可解析（解析失败 = 密钥材料转录损坏）。
func TestEmbeddedKeysParse(t *testing.T) {
	priv, err := ClientKey()
	if err != nil {
		t.Fatalf("客户端私钥解析: %v", err)
	}
	if priv.N.BitLen() != 2048 {
		t.Fatalf("客户端私钥位数 = %d", priv.N.BitLen())
	}
	pub, err := ServerPublicKey()
	if err != nil {
		t.Fatalf("服务器公钥解析: %v", err)
	}
	if pub.N.BitLen() != 2048 {
		t.Fatalf("服务器公钥位数 = %d", pub.N.BitLen())
	}
}

func TestUnwrapCookie(t *testing.T) {
	raw := `{"gameid":"x19","login_channel":"netease","app_channel":"netease","sdkuid":"uid1","sessionid":"sess1"}`
	sauth, meta := unwrapCookie(raw)
	if sauth != raw {
		t.Fatalf("expected raw string, got %s", sauth)
	}
	if meta.LoginChannel != "netease" {
		t.Fatalf("expected login_channel netease, got %s", meta.LoginChannel)
	}

	wrapped := `{"sauth_json":"{\"gameid\":\"x19\",\"login_channel\":\"netease\",\"app_channel\":\"netease\"}"}`
	sauth2, meta2 := unwrapCookie(wrapped)
	expectedInner := `{"gameid":"x19","login_channel":"netease","app_channel":"netease"}`
	if sauth2 != expectedInner {
		t.Fatalf("expected inner json %s, got %s", expectedInner, sauth2)
	}
	if meta2.LoginChannel != "netease" {
		t.Fatalf("expected netease, got %s", meta2.LoginChannel)
	}

	c4399 := `{"gameid":"x19","login_channel":"4399","app_channel":"4399"}`
	_, meta3 := unwrapCookie(c4399)
	if meta3.LoginChannel != "4399" {
		t.Fatalf("expected 4399, got %s", meta3.LoginChannel)
	}
}
