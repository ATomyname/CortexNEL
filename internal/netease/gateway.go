// 网关客户端核心：端点常量、签名请求收发、x19 通用响应壳。
//
// 具体端点用例按域拆分到同包其它文件（facts §5.1/§5.2 为共同依据）：
//
//	netgame.go     网络服列表/详情/地址
//	rental.go      租赁服列表/详情/地址/角色
//	component.go   组件清单与 modInfo 投影
//	serveritem.go  服务器 item 的 sub_entities/sub_mod_list
//	patchinfo.go   客户端本体包（含 mc_version_id 映射）
//	skin.go        皮肤检索/应用/查询
//	character.go   游戏角色查询与创建
//	interconn.go   interconn 会话建立
package netease

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nexus/internal/crypto"
	"nexus/internal/httpx"
)

// 端点基址（facts §5.1）。
const (
	GatewayBaseURL   = "https://x19apigatewayobt.nie.netease.com"
	ClientBaseURL    = "https://x19mclobt.nie.netease.com"
	InterconnBaseURL = "https://x19obtcore.nie.netease.com:8443"
	MgbBaseURL       = "https://mgbsdk.matrix.netease.com"
	MicrogameBaseURL = "https://microgame.5054399.net"
)

// Gateway 是带签名头的 x19 网关客户端。
type Gateway struct {
	BaseURL string
	// InterconnURL 是 authentication/update 等核心互联接口基址；可在测试中覆写。
	InterconnURL string
	Creds        Credentials
	HTTP         *http.Client
	// UserAgent 为空时用 com4399UserAgent；其他端点可覆写。
	UserAgent string
}

// NewGateway 构造网关客户端。
func NewGateway(creds Credentials) *Gateway {
	return &Gateway{
		BaseURL:      GatewayBaseURL,
		InterconnURL: InterconnBaseURL,
		Creds:        creds,
		HTTP:         httpx.NewClient(20 * time.Second),
	}
}

// ua 返回本客户端的 User-Agent。
func (g *Gateway) ua() string {
	if g.UserAgent != "" {
		return g.UserAgent
	}
	return com4399UserAgent
}

// Post 发送签名请求：headers 按 facts §5.2 对 (path, body) 签名。
func (g *Gateway) Post(ctx context.Context, baseURL, path string, body any, result any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if baseURL == "" {
		baseURL = g.BaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", g.ua())
	headers := crypto.ComputeGatewayHeaders(
		strconv.FormatInt(g.Creds.UserID, 10), g.Creds.Token, path, raw)
	req.Header.Set("user-id", headers.UserID)
	req.Header.Set("user-token", headers.UserToken)

	resp, err := g.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("netease: %s HTTP %d: %s", path, resp.StatusCode, truncate(data, 200))
	}
	if len(data) == 0 {
		return fmt.Errorf("netease: %s empty response HTTP %d", path, resp.StatusCode)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("netease: %s 响应解析: %w", path, err)
		}
	}
	return nil
}

// X19Response 是 x19 网关通用响应壳（code=200 成功）。
type X19Response struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Status  string          `json:"status"`
	Data    json.RawMessage `json:"data"`
}

// UnmarshalJSON 兼容 Fantnel 的实体响应壳：单对象用 entity，列表用 entities。
func (r *X19Response) UnmarshalJSON(raw []byte) error {
	type response X19Response
	var v struct {
		response
		Entity   json.RawMessage `json:"entity"`
		Entities json.RawMessage `json:"entities"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	*r = X19Response(v.response)
	if len(r.Data) == 0 {
		if len(v.Entity) != 0 {
			r.Data = v.Entity
		} else if len(v.Entities) != 0 {
			r.Data = v.Entities
		}
	}
	return nil
}

// checkCode 校验 x19 响应；参考客户端以 code=0 表示成功，兼容旧网关的 200。
func (r *X19Response) checkCode(path string) error {
	if r.Code != 0 && r.Code != 200 {
		return fmt.Errorf("netease: %s code=%d message=%s status=%s", path, r.Code, r.Message, r.Status)
	}
	return nil
}

// IsTokenExpired 判断网易网关错误是否因 Token 失效/过期引发。
func IsTokenExpired(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "code=10") ||
		strings.Contains(msg, "code=22") ||
		strings.Contains(msg, "code=11") ||
		strings.Contains(msg, "code=101") ||
		strings.Contains(msg, "请先登录") ||
		strings.Contains(msg, "token")
}

func truncate(data []byte, n int) string {
	if len(data) <= n {
		return string(data)
	}
	return string(data[:n]) + "..."
}
