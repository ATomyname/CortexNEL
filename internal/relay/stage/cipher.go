// AES-CFB8 密码对：server leg 专用（client leg 永不明文以外的形态——不，client leg 永远明文，见 DR-3）。
// 每方向独立密钥流：CFB8 是有状态流，读/写各持一个实例。
package stage

import "nexus/internal/crypto"

// CipherPair 持有一条腿上两个方向的 CFB8 流。
type CipherPair struct {
	enc *crypto.CFB8
	dec *crypto.CFB8
}

// NewCipherPair 以 16B 共享密钥构造（key = iv = secret，facts §2 AES-CFB8 规格）。
func NewCipherPair(secret []byte) (*CipherPair, error) {
	enc, err := crypto.NewCFB8Encrypter(secret)
	if err != nil {
		return nil, err
	}
	dec, err := crypto.NewCFB8Decrypter(secret)
	if err != nil {
		return nil, err
	}
	return &CipherPair{enc: enc, dec: dec}, nil
}

// TransformRead 就地解密一段新到的字节（保持密钥流顺序，调用方必须按到达顺序喂入）。
func (p *CipherPair) TransformRead(data []byte) {
	if len(data) > 0 {
		p.dec.XORKeyStream(data, data)
	}
}

// TransformWrite 就地加密待发字节（调用方必须按发送顺序喂入）。
func (p *CipherPair) TransformWrite(data []byte) {
	if len(data) > 0 {
		p.enc.XORKeyStream(data, data)
	}
}
