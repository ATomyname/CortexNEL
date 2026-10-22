// RSA 原始运算与协议全局客户端身份（facts §1/§3.5）。
// 密钥材料：facts 指向的旧实现已移出仓库；此处取自白名单参考实现 Nirvana
// （Reference/Fantnel-master Nirvana.Cipher/Cipher/Rsa.cs 使用方，YggdrasilGenerator.cs 内嵌值），
// 与 facts §1 "内嵌 PEM" 声明一致。
package netease

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"math/big"
)

// clientPrivateKeyB64 是协议全局客户端身份（PKCS#8 DER base64）。
const clientPrivateKeyB64 = "MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQDobJGIddxcs08xzTIVFanc4/84J1DbxiW7wLokIrap3txzyyMXj+AcDa8jLopLJkMg9rZLzL50Dwp+hmTTgiIcjkM1DVREsRBltzqjVNyiYPL2VGHLn+/eEivjhLNWUMcWrlAoJ5JJBi22oGPLlKDVJpg33JPI5nPZpwufdB0ecn2V0CAeeGyswQyAaqIjoiYOoP3HipjEYGQHp1RsADf4ozGRgv+2HGiSOyhlv00ixnF0nRUTzVh18ka0N3LdLMpAN/YAPkO8tmoWp0asyU3X+nyZFd29povvrRy4rL1lYmo8jdpfpSL+Yk+9+RybjBAhXRx6uDoBxaa2kwE4fyrrAgMBAAECggEAWT42szruHfoLkofDjyz+R/6TZLBT788pdeoOjwl1McyMwTlihA2Oc7cdZFjeaPSMGgAhBwHarx2HXgWkeUIibuyBCcHQdX+3WBb+wPA4t3CaWdMUqecDZzV6/KVbZu0lRKQxyvlGxhtFOjZjmyu6hZ2IHQrpA97Y5N2rLNKcy69W+QYJZappmBfbVgWM0NRmgmpg6siQ0Cm6Ryil3SBAPBVv+EUPiD9jdXbtVq7VwN4YmwUGScp2Fib1oUnqEAja1hfihVnRFQ246nKXhIc/YVrNmwBrxAVwaaPFRka6XjKkSF0WVbpqQLXhbY1fS8FoXGpVhiF6o4rTQJbpQxhQgQKBgQD7XysfPNt9G63gTrkZvjvEk5LKsRG42MAYEkuzxEal9E/AQv0jJrk0f1WO09hCcYQeaOQXhM9mezNQv5jXEnmepXqM3NTw1Di5yh3uvjSXQjdUt+7haNw+QjggBqZxyQZjtadYairSzfmWe7OwJIkCmdgaJKxm4qMExk9kUgApGwKBgQDstBdJHU/KEBqVpsIlu185vFFuaAvxiHjXHqGwytJMQ/5aVqaphIiQCAxaEogPSzPSm28UHVQiZeFO059EKOpSJscW4pV95Dr5BAbHuYecacqnZKbQqb69//Cfpne9tGYXlmP6QnYfPoc4wYfTfPyU2x3KtDhVxtEDutpSNU1ycQKBgBabWn92s66uvJZ9vfvotetZ8ku0XQmoxK3lh1Vlg40NSdbar3Vn2CQ2h3VO7BYdq2oouMq8sQJgdh7+/DnreXChJUJh4ey+yVM8MDD2fjhURjGiUSOIkLYwsmd+8Z0uHRr+jUxQUAWhbJ7yBRkEUCYhu+OuBKtEGrElPKKjFUydAoGAQLj9pQBe0OGWY1U1wRt67k6P9aB9o42tfSTjEXRkDHaLFiibab7TmI6a0gY/Le9iPDREKzvZxY4WDXfQFNMbP1tbFObf+Yxuk6iGMhaI/jvvLdZXxrajcVCKex0JoNWzFMAKlmOV6PUwBFTmzu1eI1XGz6Z3wPycKmjtSY1JoAECgYBBOfaUDMaG1xLzv+q1jPPs2U4lXPK2BXFE5RaliUGC+LIQREXPishII2LYFW3gtXj5QWfIGq6x0d6ca6Bja2vYRDDe5tlT/2VbZahiHpb2PYL/2WgeoHl7sT9Bb/nsKyo85Sv+doop6huy4+aeTiQHgrGR9JYMVBSIx6P8Tt5phA=="

// serverPublicKeyB64 是 authserver 用于解 signContent 的服务器公钥（X.509 SPKI DER base64）。
const serverPublicKeyB64 = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA4HJFrYdVTeoSvH6qsnJElfXuf7FnxxFQdz3gRCs66LDrZfaoGoWt2e/aGIOv8uGHliBWnZP42Ike9Qf5aiYVtQ4mlj2bXZjifHG35LlS1Bq6yCA6k1WevWcrGWOuLzny3jo8Wbdi0lIFMTT2hN98sF2k4YcvyE9zhqxfRNFGVI5kLyxm9CeTKAXGBU5mw3yQWJ8cPRR4866jpGGOhBWlJdilWt2NES9bid8SbhTT55wqumnVO5J5/DaMyTgKIQngH7NyZQljAhdK5I23dzpGop322n2eQ+mTNLuquwU453o1cbyQobgC6vh5/F1QT2INBR2qYCnRzzJ6hrhE5kIMZwIDAQAB"

// ErrBadPadding 表示 PKCS#1 类型 1 填充非法。
var ErrBadPadding = errors.New("netease: PKCS#1 填充非法")

// ClientKey 返回协议全局客户端私钥（单例解析）。
func ClientKey() (*rsa.PrivateKey, error) {
	der, err := base64.StdEncoding.DecodeString(clientPrivateKeyB64)
	if err != nil {
		return nil, err
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("netease: 客户端私钥不是 RSA")
	}
	return rsaKey, nil
}

// ServerPublicKey 返回 authserver 公钥。
func ServerPublicKey() (*rsa.PublicKey, error) {
	der, err := base64.StdEncoding.DecodeString(serverPublicKeyB64)
	if err != nil {
		return nil, err
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("netease: 服务器公钥不是 RSA")
	}
	return rsaPub, nil
}

// RawPrivateOp 执行 RSA 私钥原始运算（PKCS#1 类型 1 填充 + m^d mod n），
// 即 facts §3.5 的 SignPadMode=1（模式 2/3 实测 0x11 拒）。
func RawPrivateOp(priv *rsa.PrivateKey, data []byte) ([]byte, error) {
	k := (priv.N.BitLen() + 7) / 8
	if len(data) > k-11 {
		return nil, errors.New("netease: 私钥运算数据过长")
	}
	em := make([]byte, k)
	em[0] = 0x00
	em[1] = 0x01
	psLen := k - len(data) - 3
	for i := 0; i < psLen; i++ {
		em[2+i] = 0xFF
	}
	em[2+psLen] = 0x00
	copy(em[3+psLen:], data)

	m := new(big.Int).SetBytes(em)
	c := new(big.Int).Exp(m, priv.D, priv.N)
	out := make([]byte, k)
	c.FillBytes(out)
	return out, nil
}

// RawPublicDecrypt 执行 RSA 公钥原始运算并剥离类型 1 填充（解 signContent 用）。
func RawPublicDecrypt(pub *rsa.PublicKey, cipher []byte) ([]byte, error) {
	k := (pub.N.BitLen() + 7) / 8
	if len(cipher) != k {
		return nil, errors.New("netease: 密文长度与模长不符")
	}
	c := new(big.Int).SetBytes(cipher)
	m := new(big.Int).Exp(c, big.NewInt(int64(pub.E)), pub.N)
	em := make([]byte, k)
	m.FillBytes(em)
	if em[0] != 0x00 || em[1] != 0x01 {
		return nil, ErrBadPadding
	}
	sep := 2
	for sep < k && em[sep] != 0x00 {
		sep++
	}
	if sep == k {
		return nil, ErrBadPadding
	}
	return em[sep+1:], nil
}
