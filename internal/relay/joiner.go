// Joiner：认证挂接契约（架构文档 §6.6）。引擎对认证零知识，
// 接口由 relay 定义、由 internal/account 实现（D2：显式小接口）。
package relay

// EncryptionRequestData 是上游 EncryptionRequest 中引擎关心的字段
// （facts §7.2 步骤 3：serverId 可为空，pubkey/verifyToken 原样回给服务器）。
type EncryptionRequestData struct {
	ServerID    string
	PublicKey   []byte
	VerifyToken []byte
}

// Joiner 在上游 EncryptionRequest 到达时被调用（引擎已吞掉该包，不透传客户端）。
// 实现方完成 Yggdrasil join（serverId = sha1(serverId‖secret‖pubkey)）并返回共享密钥；
// 引擎只负责其后的密码学启用（加密响应构造、server leg 切 AES-CFB8）。
type Joiner interface {
	OnEncryptionRequest(c *Conn, req EncryptionRequestData) (secret []byte, err error)
}
