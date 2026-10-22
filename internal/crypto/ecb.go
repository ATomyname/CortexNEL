// ECB 无填充与 PKCS7 填充两类 AES-ECB 原语（使用者各自声明，facts 单源取参）。
// 无填充仅用于 facts §2 的 seed 派生（恰好 16B 块）；PKCS7 仅用于 facts §6.3
// 的 mPay 登录参数加密（16B hex key）。禁止用于一般数据加密（ECB 模式泄露明文模式）。
package crypto

import (
	"crypto/aes"
	"errors"
)

var errECBBlockSize = errors.New("crypto: ECB 输入长度必须是 16B 的整数倍")

// ECBEncrypt 以 key 做 AES-128-ECB 无填充加密。len(plain) 必须是 16 的倍数。
func ECBEncrypt(key, plain []byte) ([]byte, error) {
	return ecbProcess(key, plain, false)
}

// ECBDecrypt 以 key 做 AES-128-ECB 无填充解密。len(cipher) 必须是 16 的倍数。
func ECBDecrypt(key, cipher []byte) ([]byte, error) {
	return ecbProcess(key, cipher, true)
}

// ECBEncryptPKCS7 以 key 做 AES-ECB 加密，PKCS7 填充至块边界（facts §6.3 mPay 参数）。
func ECBEncryptPKCS7(key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := block.BlockSize() - len(plain)%block.BlockSize()
	padded := make([]byte, len(plain)+pad)
	copy(padded, plain)
	for i := len(plain); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	return ecbProcess(key, padded, false)
}

// ECBDecryptPKCS7 解密并去除 PKCS7 填充（facts §6.3）。
func ECBDecryptPKCS7(key, cipher []byte) ([]byte, error) {
	plain, err := ecbProcess(key, cipher, true)
	if err != nil {
		return nil, err
	}
	if len(plain) == 0 {
		return nil, errors.New("crypto: PKCS7 空明文")
	}
	pad := int(plain[len(plain)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(plain) {
		return nil, errors.New("crypto: PKCS7 填充非法")
	}
	for _, b := range plain[len(plain)-pad:] {
		if int(b) != pad {
			return nil, errors.New("crypto: PKCS7 填充非法")
		}
	}
	return plain[:len(plain)-pad], nil
}

func ecbProcess(key, data []byte, decrypt bool) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data)%block.BlockSize() != 0 {
		return nil, errECBBlockSize
	}
	out := make([]byte, len(data))
	mode := block.Encrypt
	if decrypt {
		mode = block.Decrypt
	}
	for off := 0; off < len(data); off += block.BlockSize() {
		mode(out[off:off+block.BlockSize()], data[off:off+block.BlockSize()])
	}
	return out, nil
}
