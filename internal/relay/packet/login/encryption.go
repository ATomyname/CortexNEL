// EncryptionRequest 代答（架构文档 §6.6 时序）：吞掉上游请求 → Joiner 完成 join →
// 自建响应发上游 → server leg 切 AES-CFB8。client leg 保持明文（DR-3）。
package login

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

// SecretLen 是 MC 共享密钥长度。
const SecretLen = 16

type encryptionRequest struct {
	relay.NoForward
	serverID    string
	publicKey   []byte
	verifyToken []byte
}

func (p *encryptionRequest) Read(_ *relay.Conn, r *codec.Reader) error {
	sid, err := r.String()
	if err != nil {
		return err
	}
	p.serverID = sid
	pub, err := r.ByteArray()
	if err != nil {
		return err
	}
	p.publicKey = pub
	tok, err := r.ByteArray()
	if err != nil {
		return err
	}
	p.verifyToken = tok
	return nil
}

func (p *encryptionRequest) Handle(c *relay.Conn) (bool, error) {
	if c.Joiner == nil {
		return true, fmt.Errorf("login: 会话未提供 Joiner，无法代答加密")
	}

	// Joiner 生成共享密钥并完成 serverId = sha1(serverId‖secret‖pubkey) 与 Yggdrasil join
	//（架构文档 §6.6：引擎只负责其后的密码学启用）。
	secret, err := c.Joiner.OnEncryptionRequest(c, relay.EncryptionRequestData{
		ServerID:    p.serverID,
		PublicKey:   p.publicKey,
		VerifyToken: p.verifyToken,
	})
	if err != nil {
		return true, err
	}
	if len(secret) != SecretLen {
		return true, fmt.Errorf("login: Joiner 返回的共享密钥长度 %d ≠ 16B", len(secret))
	}

	pubAny, err := x509.ParsePKIXPublicKey(p.publicKey)
	if err != nil {
		return true, fmt.Errorf("login: 解析上游公钥: %w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return true, fmt.Errorf("login: 上游公钥不是 RSA")
	}

	encSecret, err := rsa.EncryptPKCS1v15(rand.Reader, pub, secret)
	if err != nil {
		return true, err
	}
	encToken, err := rsa.EncryptPKCS1v15(rand.Reader, pub, p.verifyToken)
	if err != nil {
		return true, err
	}

	// 1. 先以明文把响应发上游（facts §7.2 步骤 3）。
	resp := &encryptionResponse{
		secretEncrypted: encSecret,
		verifyTokenEnc:  encToken,
	}
	if err := c.SendServerPacket(idEncryptionResponse, resp); err != nil {
		return true, err
	}
	// 2. 响应落网后才切换 server leg 密钥流（顺序错一帧即 bad enc frame length）。
	if err := c.EnableServerEncryption(secret); err != nil {
		return true, err
	}
	return true, nil
}

// encryptionResponse 是代答响应（仅编码，从不解析——客户端永不发它，见 facts §7.4）。
type encryptionResponse struct {
	secretEncrypted []byte
	verifyTokenEnc  []byte
}

func (p *encryptionResponse) Write(w *codec.Writer) error {
	w.ByteArray(p.secretEncrypted)
	w.ByteArray(p.verifyTokenEnc)
	return nil
}

// clientEncryptionResponse 吞掉客户端发来的加密响应（facts §7.4：
// 代理从未向客户端发 EncryptionRequest，客户端不该发此包；防御性丢弃）。
type clientEncryptionResponse struct {
	relay.NoForward
}

func (p *clientEncryptionResponse) Handle(*relay.Conn) (bool, error) {
	return true, nil
}
