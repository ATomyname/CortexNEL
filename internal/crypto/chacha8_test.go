package crypto

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/chacha20"
)

// TestQuarterRoundRFC8439 用 RFC 8439 §2.2.1 的 quarter-round 向量验证核心运算。
// 该向量与轮数无关，可独立证明 quarterRound / rotl32 正确。
func TestQuarterRoundRFC8439(t *testing.T) {
	s := [16]uint32{0x11111111, 0x01020304, 0x9b8d6f43, 0x01234567}
	quarterRound(&s, 0, 1, 2, 3)

	want := [16]uint32{0xea2a92f4, 0xcb1cf8ce, 0x4581472e, 0x5881c4bb}
	if s != want {
		t.Fatalf("quarterRound mismatch\n got %08x\nwant %08x", s, want)
	}
}

// TestChaChaBlockAgainstXCrypto 用 golang.org/x/crypto/chacha20 作独立预言机验证块函数。
//
// 之所以可行：ChaCha8 与 ChaCha20 仅在 double-round 次数上不同（4 vs 10），
// 状态构造、块函数与末态加法完全一致。因此以 20 轮对齐预言机，即可证明
// 本实现的核心（含 counter 递增、跨块续流）正确；ChaCha8 只是轮数取值不同。
//
// 选预言机而非手写 RFC 向量：x/crypto 已在本模块依赖图中（wails 间接依赖），
// 且是经充分验证的实现，比凭记忆抄向量可靠。
func TestChaChaBlockAgainstXCrypto(t *testing.T) {
	var key [32]byte
	for i := range key {
		key[i] = byte(i)
	}
	var nonce [12]byte
	copy(nonce[:], []byte{0x00, 0x00, 0x00, 0x09, 0x00, 0x00, 0x00, 0x4a, 0x00, 0x00, 0x00, 0x00})

	// 跨多个 64B 块，同时验证 counter 递增与块边界处理。
	plain := make([]byte, 300)
	for i := range plain {
		plain[i] = byte(i * 7)
	}

	oracle, err := chacha20.NewUnauthenticatedCipher(key[:], nonce[:])
	if err != nil {
		t.Fatalf("oracle setup: %v", err)
	}
	want := make([]byte, len(plain))
	oracle.XORKeyStream(want, plain)

	got := make([]byte, len(plain))
	newChaCha(key[:], nonce[:], 0, 10).XORKeyStream(got, plain) // 20 轮 = 10 double-round

	if !bytes.Equal(got, want) {
		t.Fatalf("chacha block mismatch vs x/crypto\n got %x\nwant %x", got, want)
	}
}

// testNonce 是算法测试用的任意 12B nonce（RFC 向量测试自带 nonce，不依赖此值）；
// 协议 nonce（facts §1 ChaChaNonce）由 internal/netease 单源持有。
var testNonce = []byte("0123456789ab")

// TestChaCha8KeyTruncation 验证密钥按 §2 截断到 32B（超长只取前 32B）。
func TestChaCha8KeyTruncation(t *testing.T) {
	long := bytes.Repeat([]byte{0xAB}, 64)
	short := bytes.Repeat([]byte{0xAB}, 32)

	gotLong := make([]byte, 32)
	gotShort := make([]byte, 32)
	NewChaCha8(long, testNonce, 0).XORKeyStream(gotLong, make([]byte, 32))
	NewChaCha8(short, testNonce, 0).XORKeyStream(gotShort, make([]byte, 32))

	if !bytes.Equal(gotLong, gotShort) {
		t.Fatal("key longer than 32B must be truncated, not rejected")
	}
}

// TestChaCha8RoundTrip 验证 ChaCha8 加解密互逆。
func TestChaCha8RoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	plain := []byte("nexus chacha8 round trip payload")

	cipher := make([]byte, len(plain))
	NewChaCha8(key, testNonce, 0).XORKeyStream(cipher, plain)

	if bytes.Equal(cipher, plain) {
		t.Fatal("ciphertext must differ from plaintext")
	}

	got := make([]byte, len(cipher))
	NewChaCha8(key, testNonce, 0).XORKeyStream(got, cipher)

	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip failed\n got %x\nwant %x", got, plain)
	}
}

// TestChaCha8DiffersFromChaCha20 断言 ChaCha8 确实不是 20 轮（防止轮数写死后无人发现）。
func TestChaCha8DiffersFromChaCha20(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, 32)
	src := make([]byte, 64)

	got8 := make([]byte, 64)
	got20 := make([]byte, 64)
	NewChaCha8(key, testNonce, 0).XORKeyStream(got8, src)
	newChaCha(key, testNonce, 0, 10).XORKeyStream(got20, src)

	if bytes.Equal(got8, got20) {
		t.Fatal("ChaCha8 must use 8 rounds, but output equals 20-round ChaCha")
	}
}
