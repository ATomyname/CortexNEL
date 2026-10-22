// 网络服端点（facts §5.1/§5.3）：列表、详情、上游地址解析。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// NetGameItem 是网络服列表项（facts §5.3，形状核对 Nirvana EntityNetGameItem）。
type NetGameItem struct {
	EntityID       string   `json:"entity_id"`
	Name           string   `json:"name"`
	OnlineCount    string   `json:"online_count"`
	BriefSummary   string   `json:"brief_summary"`
	TitleImageURL  string   `json:"title_image_url"`
	ImageURL       string   `json:"image_url"`
	BriefImageURLs []string `json:"brief_image_urls"`
}

// NetGameDetail 是网络服详情（facts §5.3；封面取 BriefImageURLs[0]）。
type NetGameDetail struct {
	EntityID      string `json:"entity_id"`
	Name          string `json:"name"`
	McVersionList []struct {
		Name string `json:"name"`
	} `json:"mc_version_list"`
	BriefImageURLs    []string `json:"brief_image_urls"`
	DetailDescription string   `json:"detail_description"`
	DeveloperName     string   `json:"developer_name"`
	ServerAddress     string   `json:"server_address"`
	ServerPort        int      `json:"server_port"`
	Status            int      `json:"status"`
	ServerStatus      int      `json:"server_status"`
	IsMaintenance     bool     `json:"is_maintenance"`
}

// NetGameAddress 是网络服上游连接地址（facts §5.3）。
// 租赁服地址解析复用同一形状（rental.go）。
type NetGameAddress struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// ListNetGames 拉取网络服列表（facts §5.1/§5.3，master_type_id=2）。
func (g *Gateway) ListNetGames(ctx context.Context, offset, length int) ([]NetGameItem, error) {
	body := map[string]any{
		"available_mc_versions": []string{},
		"item_type":             1,
		"length":                length,
		"offset":                offset,
		"master_type_id":        MasterTypeGame,
		"secondary_type_id":     "",
	}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/item/query/available", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/item/query/available"); err != nil {
		return nil, err
	}
	var items []NetGameItem
	if err := json.Unmarshal(out.Data, &items); err != nil {
		return nil, fmt.Errorf("netease: 网络服列表解析: %w", err)
	}
	return items, nil
}

// NetGameDetail 拉取单个网络服详情（facts §5.1/§5.3）。
func (g *Gateway) NetGameDetail(ctx context.Context, itemID string) (*NetGameDetail, error) {
	body := map[string]any{"item_id": itemID}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/item-details/get_v2", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/item-details/get_v2"); err != nil {
		return nil, err
	}
	d := &NetGameDetail{}
	if err := json.Unmarshal(out.Data, d); err != nil {
		return nil, fmt.Errorf("netease: 网络服详情解析: %w", err)
	}
	return d, nil
}

// NetGameAddress 解析网络服上游地址（facts §5.1/§5.3）。
func (g *Gateway) NetGameAddress(ctx context.Context, itemID string) (*NetGameAddress, error) {
	body := map[string]any{"item_id": itemID}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/item-address/get", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/item-address/get"); err != nil {
		return nil, err
	}
	addr := &NetGameAddress{}
	if err := json.Unmarshal(out.Data, addr); err != nil {
		return nil, fmt.Errorf("netease: 网络服地址解析: %w", err)
	}
	return addr, nil
}

// SearchAuthItemList 查询服务器要求组件清单（modInfo 权威来源，facts §5.1/§3.7）。
func (g *Gateway) SearchAuthItemList(ctx context.Context, gameID string) (*X19Response, error) {
	body := map[string]any{"game_id": gameID}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/game-auth-item-list/query/search-by-game", body, out); err != nil {
		return nil, err
	}
	return out, nil
}
