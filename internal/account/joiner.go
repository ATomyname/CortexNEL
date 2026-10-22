// relay.Joiner 实现（架构文档 §6.6）：EncryptionRequest 到达时完成
// serverId = sha1(serverId‖secret‖pubkey) 与 Yggdrasil join，返回共享密钥。
package account

import (
	"context"
	"crypto/rand"
	"fmt"
	"strconv"
	"time"

	"nexus/internal/crypto"
	"nexus/internal/netease"
	"nexus/internal/relay"
)

// Joiner 依赖通过构造注入：凭据供给与 modInfo 供给由会话编排层装配。
type Joiner struct {
	// Credentials 返回当前账号凭据（0xff 重登后可被编排层更新）。
	Credentials func() (netease.Credentials, error)
	// ModInfo 返回 {"mods":[...]}（档案域组装）。
	ModInfo func() string
	// MCVersion 目标 Minecraft 版本
	MCVersion string
}

// NewJoiner 构造。
func NewJoiner(creds func() (netease.Credentials, error), modInfo func() string, mcVersion string) *Joiner {
	return &Joiner{Credentials: creds, ModInfo: modInfo, MCVersion: mcVersion}
}

// OnEncryptionRequest 实现 relay.Joiner（facts §7.2 步骤 3）。
func (j *Joiner) OnEncryptionRequest(c *relay.Conn, req relay.EncryptionRequestData) ([]byte, error) {
	if j.Credentials == nil {
		return nil, fmt.Errorf("account: 未装配凭据供给")
	}
	creds, err := j.Credentials()
	if err != nil {
		return nil, err
	}

	secret := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}

	// serverId 按 MC 官方与 Fantnel 规范采用 Two's-complement SHA1 格式化散列字符串
	serverID := crypto.MinecraftAuthDigest(req.ServerID, secret, req.PublicKey)

	gameID, err := strconv.ParseInt(c.GameID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("account: 会话 GameID 非法 %q: %w", c.GameID, err)
	}

	modInfo := ""
	if j.ModInfo != nil {
		modInfo = j.ModInfo()
	}

	ver := j.MCVersion
	if ver == "" {
		ver = "1.20"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := netease.Join(ctx, netease.JoinParams{
		Credentials: creds,
		GameID:      gameID,
		ServerID:    serverID,
		ModInfo:     modInfo,
		MCVersion:   ver,
	}); err != nil {
		return nil, err
	}
	return secret, nil
}
