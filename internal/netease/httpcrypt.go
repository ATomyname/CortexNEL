// x19 Core HTTP 加密传输层（facts §6.1.2，算法逐行核对 Nirvana HttpUtil）：
// AES-128-CBC；明文零填充到 16 对齐后尾部追加 16B IV（解密端按尾标裁剪）；
// 输出帧 = IV(16B) ‖ 密文 ‖ keyIndex(1B，高 4 位密钥序号)。
package netease

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

// httpEncrypt 加密请求体（facts §6.1.2）。
func httpEncrypt(bodyIn []byte) ([]byte, error) {
	padded := make([]byte, ((len(bodyIn)+16+15)/16)*16)
	copy(padded, bodyIn)
	copy(padded[len(bodyIn):], HttpIV) // 明文尾部 16B IV

	keyIndex := make([]byte, 1)
	if _, err := rand.Read(keyIndex); err != nil {
		return nil, err
	}
	idx := keyIndex[0] & 0x0F

	block, err := aes.NewCipher(HttpAESKeys[idx])
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, HttpIV).CryptBlocks(out, padded)

	result := make([]byte, 0, 16+len(out)+1)
	result = append(result, HttpIV...)
	result = append(result, out...)
	return append(result, idx<<4|0x02), nil
}

// httpDecrypt 解密响应体（facts §6.1.2）。
func httpDecrypt(body []byte) ([]byte, error) {
	if len(body) < 0x12 { // 16B IV + ≥1B 密文 + 1B keyIndex
		return nil, errors.New("netease: http 加密响应过短")
	}
	block, err := aes.NewCipher(HttpAESKeys[(body[len(body)-1]>>4)&0x0F])
	if err != nil {
		return nil, err
	}
	enc := body[16 : len(body)-1]
	if len(enc)%16 != 0 || len(enc) == 0 {
		return nil, errors.New("netease: http 加密响应长度非法")
	}
	out := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, body[:16]).CryptBlocks(out, enc)

	// 尾部裁剪（Nirvana scissor 语义）：从末尾向前数 16 个非零字节后截断。
	scissor, pos := 0, len(out)-1
	for scissor < 16 {
		if pos < 0 {
			return nil, errors.New("netease: http 加密响应尾标非法")
		}
		if out[pos] != 0x00 {
			scissor++
		}
		pos--
	}
	return out[:pos+1], nil
}
