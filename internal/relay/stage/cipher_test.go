package stage

import (
	"bytes"
	"testing"
)

func TestCipherPairRoundTrip(t *testing.T) {
	secret := bytes.Repeat([]byte{0x2A}, 16)
	pair, err := NewCipherPair(secret)
	if err != nil {
		t.Fatalf("NewCipherPair: %v", err)
	}
	// 每方向独立密钥流：用独立 pair 模拟对端。
	peer, err := NewCipherPair(secret)
	if err != nil {
		t.Fatalf("peer: %v", err)
	}
	msg := []byte("hello encrypted leg")
	cipherText := append([]byte(nil), msg...)
	pair.TransformWrite(cipherText)
	if bytes.Equal(cipherText, msg) {
		t.Fatal("密文不应等于明文")
	}
	peer.TransformRead(cipherText)
	if !bytes.Equal(cipherText, msg) {
		t.Fatal("对端解密应还原明文")
	}
}
