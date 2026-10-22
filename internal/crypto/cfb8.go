package crypto

import (
	"crypto/aes"
	"crypto/cipher"
)

// CFB8 是 CFB-8 模式的流密码：每加密 1 字节，反馈喂密文。
// 事实来源：docs/facts/NETEASE-PROTOCOL.md §2「AES-CFB8（MC 加密腿）」：
// key = iv = 16B secret；CFB-8，反馈喂密文。
//
// 加解密各自持有独立状态流（§7.3 长度前缀本身流经密钥流，
// 因此读路径必须逐字节流式处理，不能按块预取）。
type CFB8 struct {
	block cipher.Block
	// iv 是当前移位寄存器的内容，长度等于块大小（16B）。
	iv []byte
	// tmp 复用块加密输出，避免每字节分配。
	tmp []byte
	// decrypt 区分方向：CFB 的反馈字节永远是密文，而密文在加密时位于 dst、解密时位于 src。
	decrypt bool
}

// NewCFB8Encrypter 构造加密流。secret 16B，同时用作 key 与 iv。
func NewCFB8Encrypter(secret []byte) (*CFB8, error) {
	return newCFB8(secret, false)
}

// NewCFB8Decrypter 构造解密流。secret 16B，同时用作 key 与 iv。
func NewCFB8Decrypter(secret []byte) (*CFB8, error) {
	return newCFB8(secret, true)
}

func newCFB8(secret []byte, decrypt bool) (*CFB8, error) {
	block, err := aes.NewCipher(secret)
	if err != nil {
		return nil, err
	}
	bs := block.BlockSize()
	return &CFB8{
		block:   block,
		iv:      append([]byte(nil), secret[:bs]...),
		tmp:     make([]byte, bs),
		decrypt: decrypt,
	}, nil
}

// XORKeyStream 逐字节处理 src 并写入 dst。
//
// 两个方向都执行 ciphertext[i] = plaintext[i] ^ AES-Encrypt(iv)[0]，
// 随后移位寄存器左移 1 字节并填入**密文字节**（CFB 的定义：反馈喂密文，不喂明文）。
// 密文字节在加密时是本轮产出（dst[i]），在解密时是输入（src[i]）—— 这是唯一的方向差异。
func (c *CFB8) XORKeyStream(dst, src []byte) {
	bs := c.block.BlockSize()
	for i := 0; i < len(src); i++ {
		c.block.Encrypt(c.tmp, c.iv)
		out := src[i] ^ c.tmp[0]

		feedback := out
		if c.decrypt {
			feedback = src[i]
		}
		copy(c.iv, c.iv[1:])
		c.iv[bs-1] = feedback

		dst[i] = out
	}
}
