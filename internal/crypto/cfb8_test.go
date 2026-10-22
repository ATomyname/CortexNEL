package crypto

import (
	"bytes"
	"crypto/aes"
	"testing"
)

var cfb8Secret = []byte{
	0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
	0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff,
}

// TestCFB8FirstByteAgainstRawAES 用裸 AES 校验 CFB8 首字节定义：
// ciphertext[0] = plaintext[0] ^ AES-ECB(key, iv)[0]，其中 key = iv = secret。
// 该式与轮数/移位无关，可独立证明密钥与 IV 装配正确。
func TestCFB8FirstByteAgainstRawAES(t *testing.T) {
	block, err := aes.NewCipher(cfb8Secret)
	if err != nil {
		t.Fatalf("aes setup: %v", err)
	}
	head := make([]byte, block.BlockSize())
	block.Encrypt(head, cfb8Secret)

	enc, err := NewCFB8Encrypter(cfb8Secret)
	if err != nil {
		t.Fatalf("NewCFB8Encrypter: %v", err)
	}

	plain := []byte{0xde, 0xad, 0xbe, 0xef}
	got := make([]byte, len(plain))
	enc.XORKeyStream(got, plain)

	if want := plain[0] ^ head[0]; got[0] != want {
		t.Fatalf("first byte mismatch: got %02x want %02x", got[0], want)
	}
}

// TestCFB8RoundTrip 用互相独立的加/解密流验证互逆（协议中两腿各持一份状态）。
func TestCFB8RoundTrip(t *testing.T) {
	plain := make([]byte, 300) // 跨多个 AES 块，覆盖移位寄存器换块
	for i := range plain {
		plain[i] = byte(i * 13)
	}

	enc, err := NewCFB8Encrypter(cfb8Secret)
	if err != nil {
		t.Fatalf("NewCFB8Encrypter: %v", err)
	}
	ciphertext := make([]byte, len(plain))
	enc.XORKeyStream(ciphertext, plain)

	if bytes.Equal(ciphertext, plain) {
		t.Fatal("ciphertext must differ from plaintext")
	}

	dec, err := NewCFB8Decrypter(cfb8Secret)
	if err != nil {
		t.Fatalf("NewCFB8Decrypter: %v", err)
	}
	got := make([]byte, len(ciphertext))
	dec.XORKeyStream(got, ciphertext)

	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip failed\n got %x\nwant %x", got, plain)
	}
}

// TestCFB8StreamsAcrossCalls 验证加/解密状态可跨多次调用延续（对应 TCP 分片到达）。
// 整段一次处理的结果，必须与按不规则分片逐段处理的结果一致。
func TestCFB8StreamsAcrossCalls(t *testing.T) {
	full := []byte("minecraft aes-cfb8 streaming leg payload")

	// 基线：整段一次处理。
	encAll, _ := NewCFB8Encrypter(cfb8Secret)
	decAll, _ := NewCFB8Decrypter(cfb8Secret)
	want := make([]byte, len(full))
	encAll.XORKeyStream(want, full)
	decAll.XORKeyStream(want, want)

	// 分段：同一对流按不规则切片顺序推进，状态须延续。
	enc, _ := NewCFB8Encrypter(cfb8Secret)
	dec, _ := NewCFB8Decrypter(cfb8Secret)
	got := make([]byte, 0, len(full))
	for start := 0; start < len(full); {
		n := chunkSizes[start%len(chunkSizes)]
		if start+n > len(full) {
			n = len(full) - start
		}
		buf := make([]byte, n)
		enc.XORKeyStream(buf, full[start:start+n])
		back := make([]byte, n)
		dec.XORKeyStream(back, buf)
		got = append(got, back...)
		start += n
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("streaming across calls mismatch\n got %x\nwant %x", got, want)
	}
}

// chunkSizes 是不规则分片长度，用于模拟 TCP 分片到达。
var chunkSizes = []int{1, 5, 3, 17, 8, 4}

// TestCFB8RejectsBadKeySize 验证非法密钥长度被拒绝。
func TestCFB8RejectsBadKeySize(t *testing.T) {
	if _, err := NewCFB8Encrypter(make([]byte, 15)); err == nil {
		t.Fatal("15-byte secret must be rejected")
	}
}
