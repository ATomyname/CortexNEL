// token.go — 账号认证 token 的显式刷新（登录后心跳）。
//
//	POST {interconn}/authentication/update
//	  body = x19 传输加密（HttpEncryptX19 同构），明文 JSON 只含 entity_id；
//	  签名 = ComputeDynamicToken(path, 明文JSON, entityId, token)；
//	  响应 = x19 解密 → {code, entity:{token,...}}。
//
// 返回的 token 替换 login-otp 的初始 token（实测旧 token 续用会被服务端拒绝）。
package netease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"nexus/internal/crypto"
)

// updateEntity 是 /authentication/update 响应实体。
type updateEntity struct {
	Token      string `json:"token"`
	IsRegister bool   `json:"is_register"`
	EntityID   string `json:"entity_id"`
}

// RefreshAuthenticationToken 执行登录后的显式 authentication/update 心跳。
// 请求体只包含 entity_id；返回 token 应替换 login-otp 的初始 token。
func (g *Gateway) RefreshAuthenticationToken(ctx context.Context) (string, error) {
	plain, err := json.Marshal(struct {
		EntityID int64 `json:"entity_id"`
	}{EntityID: g.Creds.UserID})
	if err != nil {
		return "", err
	}
	return g.authenticationUpdate(ctx, plain)
}

func (g *Gateway) authenticationUpdate(ctx context.Context, plain []byte) (string, error) {
	enc, err := httpEncrypt(plain)
	if err != nil {
		return "", fmt.Errorf("netease: 鉴权刷新加密: %w", err)
	}
	headers := crypto.ComputeGatewayHeaders(
		strconv.FormatInt(g.Creds.UserID, 10), g.Creds.Token, "/authentication/update", plain)
	baseURL := g.InterconnURL
	if baseURL == "" {
		baseURL = InterconnBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/authentication/update", bytes.NewReader(enc))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", com4399UserAgent)
	req.Header.Set("user-id", headers.UserID)
	req.Header.Set("user-token", headers.UserToken)

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("netease: /authentication/update HTTP %d: %s", resp.StatusCode, truncate(data, 200))
	}
	dec, err := httpDecrypt(data)
	if err != nil {
		return "", fmt.Errorf("netease: 鉴权刷新响应解密: %w", err)
	}
	var envelope struct {
		Code    int           `json:"code"`
		Message string        `json:"message"`
		Entity  *updateEntity `json:"entity"`
	}
	if err := json.Unmarshal(dec, &envelope); err != nil {
		return "", fmt.Errorf("netease: 鉴权刷新响应解析: %w", err)
	}
	if envelope.Code != 0 && envelope.Code != 200 {
		return "", fmt.Errorf("netease: /authentication/update code=%d message=%s", envelope.Code, envelope.Message)
	}
	if envelope.Entity == nil || envelope.Entity.Token == "" {
		return "", fmt.Errorf("netease: 鉴权刷新未返回 token")
	}
	return envelope.Entity.Token, nil
}
