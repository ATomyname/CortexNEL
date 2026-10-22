// 租赁服端点（facts §5.1）：公开租赁服列表、详情、地址解析与角色管理。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// RentalServer 是公开租赁服列表项。
type RentalServer struct {
	EntityID     string `json:"entity_id"`
	Name         string `json:"name"`
	ServerName   string `json:"server_name"`
	HasPassword  string `json:"has_pwd"`
	McVersion    string `json:"mc_version"`
	PlayerCount  uint   `json:"player_count"`
	Capacity     uint   `json:"capacity"`
	ImageURL     string `json:"image_url"`
	BriefSummary string `json:"brief_summary"`
}

// RentalRole 是租赁服上的玩家角色。
type RentalRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListRentalServers 拉取公开租赁服列表。
func (g *Gateway) ListRentalServers(ctx context.Context, offset, length int) ([]RentalServer, error) {
	out := &X19Response{}
	body := map[string]any{"offset": offset, "sort_type": 0}
	if err := g.Post(ctx, ClientBaseURL, "/rental-server/query/available-public-server", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/rental-server/query/available-public-server"); err != nil {
		return nil, err
	}
	var items []RentalServer
	var envelope struct {
		Items []RentalServer `json:"items"`
	}
	if err := json.Unmarshal(out.Data, &envelope); err == nil && envelope.Items != nil {
		return envelope.Items, nil
	}
	if err := json.Unmarshal(out.Data, &items); err != nil {
		return nil, fmt.Errorf("netease: 租赁服列表解析: %w", err)
	}
	return items, nil
}

// RentalDetail 拉取租赁服详情。
func (g *Gateway) RentalDetail(ctx context.Context, serverID string) (RentalServer, error) {
	out := &X19Response{}
	if err := g.Post(ctx, ClientBaseURL, "/rental-server-details/get", map[string]any{"server_id": serverID}, out); err != nil {
		return RentalServer{}, err
	}
	if err := out.checkCode("/rental-server-details/get"); err != nil {
		return RentalServer{}, err
	}
	var item RentalServer
	if err := json.Unmarshal(out.Data, &item); err != nil {
		return RentalServer{}, fmt.Errorf("netease: 租赁服详情解析: %w", err)
	}
	return item, nil
}

// RentalAddress 解析租赁服上游地址；password 为空时按无密码服发送 "none"。
func (g *Gateway) RentalAddress(ctx context.Context, serverID, password string) (*NetGameAddress, error) {
	out := &X19Response{}
	if password == "" {
		password = "none"
	}
	if err := g.Post(ctx, ClientBaseURL, "/rental-server-world-enter/get", map[string]any{"server_id": serverID, "pwd": password}, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/rental-server-world-enter/get"); err != nil {
		return nil, err
	}
	addr := &NetGameAddress{}
	if err := json.Unmarshal(out.Data, addr); err != nil {
		return nil, fmt.Errorf("netease: 租赁服地址解析: %w", err)
	}
	if addr.IP == "" || addr.Port == 0 {
		return nil, fmt.Errorf("租赁服未返回有效地址")
	}
	return addr, nil
}

// ListRentalRoles 查询用户在指定租赁服上的角色。
func (g *Gateway) ListRentalRoles(ctx context.Context, serverID string) ([]RentalRole, error) {
	out := &X19Response{}
	if err := g.Post(ctx, ClientBaseURL, "/rental-server-player/query/search-by-user-server", map[string]any{"server_id": serverID, "offset": 0, "length": 10}, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/rental-server-player/query/search-by-user-server"); err != nil {
		return nil, err
	}
	var roles []RentalRole
	var envelope struct {
		Items []RentalRole `json:"items"`
	}
	if err := json.Unmarshal(out.Data, &envelope); err == nil && envelope.Items != nil {
		return envelope.Items, nil
	}
	if err := json.Unmarshal(out.Data, &roles); err != nil {
		return nil, fmt.Errorf("netease: 租赁服角色解析: %w", err)
	}
	return roles, nil
}

// CreateRentalRole 在租赁服上创建角色。
func (g *Gateway) CreateRentalRole(ctx context.Context, serverID, name string, userID int64) error {
	out := &X19Response{}
	body := map[string]any{"server_id": serverID, "user_id": userID, "name": name, "create_ts": 555555, "is_online": false, "status": 0}
	if err := g.Post(ctx, "", "/rental-server-player", body, out); err != nil {
		return err
	}
	return out.checkCode("/rental-server-player")
}
