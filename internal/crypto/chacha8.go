// Package crypto 提供网易接入协议所需的密码学原语。
// 每个原语独立成文件（规范 §5 I4），取值只来自 docs/facts/NETEASE-PROTOCOL.md。
package crypto

import "encoding/binary"

// sigma 是标准 ChaCha 状态常量 "expand 32-byte k"。
// 协议 nonce（facts §1 ChaChaNonce）由 internal/netease 单源持有；本包只实现算法。
var sigma = [4]uint32{0x61707865, 0x3320646e, 0x79622d32, 0x6b206574}

// chachaDoubleRounds 是 ChaCha8 的 double-round 次数。
// 事实来源：docs/facts/NETEASE-PROTOCOL.md §2「8 轮（4 轮 double-round）」。
const chachaDoubleRounds = 4

// ChaCha 是参数化轮数的 ChaCha 流密码。
// 轮数可变是为了让核心块函数能用 RFC 8439 的 20 轮向量验证（见 chacha8_test.go）；
// 协议实现统一经 ChaCha8 使用 4 轮 double-round。
type ChaCha struct {
	state        [16]uint32
	doubleRounds int
}

// NewChaCha8 用 32B key、12B nonce 与初始 counter 构造 ChaCha8（8 轮）。
// 密钥截断规则：copy(key[:32])，即派生串超 32B 时只取前 32B（§2 密钥截断）。
func NewChaCha8(key, nonce []byte, counter uint32) *ChaCha {
	return newChaCha(key, nonce, counter, chachaDoubleRounds)
}

func newChaCha(key, nonce []byte, counter uint32, doubleRounds int) *ChaCha {
	var k [32]byte
	copy(k[:], key)

	c := &ChaCha{doubleRounds: doubleRounds}
	c.state[0] = sigma[0]
	c.state[1] = sigma[1]
	c.state[2] = sigma[2]
	c.state[3] = sigma[3]
	for i := 0; i < 8; i++ {
		c.state[4+i] = binary.LittleEndian.Uint32(k[i*4 : i*4+4])
	}
	c.state[12] = counter
	for i := 0; i < 3; i++ {
		c.state[13+i] = binary.LittleEndian.Uint32(nonce[i*4 : i*4+4])
	}
	return c
}

// XORKeyStream 用密钥流对 src 逐字节异或，结果写入 dst。
// src 与 dst 可指向同一底层数组。
func (c *ChaCha) XORKeyStream(dst, src []byte) {
	var block [64]byte
	var working [16]uint32

	for len(src) > 0 {
		copy(working[:], c.state[:])
		for i := 0; i < c.doubleRounds; i++ {
			doubleRound(&working)
		}
		for i := range working {
			working[i] += c.state[i]
			binary.LittleEndian.PutUint32(block[i*4:i*4+4], working[i])
		}
		c.state[12]++

		n := len(src)
		if n > 64 {
			n = 64
		}
		for i := 0; i < n; i++ {
			dst[i] = src[i] ^ block[i]
		}
		src = src[n:]
		dst = dst[n:]
	}
}

func doubleRound(s *[16]uint32) {
	quarterRound(s, 0, 4, 8, 12)
	quarterRound(s, 1, 5, 9, 13)
	quarterRound(s, 2, 6, 10, 14)
	quarterRound(s, 3, 7, 11, 15)
	quarterRound(s, 0, 5, 10, 15)
	quarterRound(s, 1, 6, 11, 12)
	quarterRound(s, 2, 7, 8, 13)
	quarterRound(s, 3, 4, 9, 14)
}

func quarterRound(s *[16]uint32, a, b, c, d int) {
	s[a] += s[b]
	s[d] = rotl32(s[d]^s[a], 16)
	s[c] += s[d]
	s[b] = rotl32(s[b]^s[c], 12)
	s[a] += s[b]
	s[d] = rotl32(s[d]^s[a], 8)
	s[c] += s[d]
	s[b] = rotl32(s[b]^s[c], 7)
}

func rotl32(v uint32, n uint) uint32 {
	return (v << n) | (v >> (32 - n))
}
