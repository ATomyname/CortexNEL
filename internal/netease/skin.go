// 皮肤端点（facts §5.1）：免费皮肤检索、批量取图、应用与已装备查询。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// SkinItem 是皮肤商店条目。
type SkinItem struct {
	EntityID       string   `json:"entity_id"`
	Name           string   `json:"name"`
	PreviewURL     string   `json:"preview_url"`
	TitleImageURL  string   `json:"title_image_url"`
	ImageURL       string   `json:"image_url"`
	BriefImageURLs []string `json:"brief_image_urls"`
}

// ListFreeSkins 拉取免费皮肤列表。
func (g *Gateway) ListFreeSkins(ctx context.Context, offset, length int) ([]SkinItem, error) {
	body := map[string]any{"is_has": true, "item_type": 2, "length": length, "master_type_id": 10, "offset": offset, "price_type": 3, "secondary_type_id": 31}
	return g.skinQuery(ctx, "/item/query/available", body)
}

// SearchSkins 按关键词检索皮肤。
func (g *Gateway) SearchSkins(ctx context.Context, keyword string, offset, length int) ([]SkinItem, error) {
	body := map[string]any{"is_has": true, "is_sync": 0, "item_type": 2, "keyword": keyword, "length": length, "master_type_id": 10, "offset": offset, "price_type": 3, "secondary_type_id": "31", "sort_type": 1, "year": 0}
	return g.skinQuery(ctx, "/item/query/search-by-keyword", body)
}

// skinQuery 执行一次皮肤列表查询，兼容 items 信封与裸数组两种响应形状。
func (g *Gateway) skinQuery(ctx context.Context, path string, body map[string]any) ([]SkinItem, error) {
	out := &X19Response{}
	if err := g.Post(ctx, "", path, body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode(path); err != nil {
		return nil, err
	}
	var envelope struct {
		Items []SkinItem `json:"items"`
	}
	if err := json.Unmarshal(out.Data, &envelope); err == nil && envelope.Items != nil {
		return envelope.Items, nil
	}
	var items []SkinItem
	if err := json.Unmarshal(out.Data, &items); err != nil {
		return nil, fmt.Errorf("netease: 皮肤列表解析: %w", err)
	}
	return items, nil
}

// BatchSkinImages 批量取皮肤封面图，返回 entity_id → 图片 URL 的映射。
// 优先级：title_image > image > brief_image[0] > preview。
func (g *Gateway) BatchSkinImages(ctx context.Context, ids []string) (map[string]string, error) {
	if len(ids) == 0 {
		return make(map[string]string), nil
	}
	body := map[string]any{
		"channel_id":       11,
		"entity_ids":       ids,
		"is_has":           true,
		"with_price":       true,
		"with_title_image": true,
	}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/item/query/search-by-ids", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/item/query/search-by-ids"); err != nil {
		return nil, err
	}
	var items []SkinItem
	if err := json.Unmarshal(out.Data, &items); err != nil {
		var env struct {
			Items []SkinItem `json:"items"`
		}
		if err2 := json.Unmarshal(out.Data, &env); err2 == nil {
			items = env.Items
		}
	}
	res := make(map[string]string, len(items))
	for _, it := range items {
		img := it.TitleImageURL
		if img == "" {
			img = it.ImageURL
		}
		if img == "" && len(it.BriefImageURLs) > 0 {
			img = it.BriefImageURLs[0]
		}
		if img == "" {
			img = it.PreviewURL
		}
		if img != "" {
			res[it.EntityID] = img
		}
	}
	return res, nil
}

// ApplySkin 把皮肤应用到全部相关 game_type（java 客户端各形态）。
func (g *Gateway) ApplySkin(ctx context.Context, skinID string) error {
	settings := make([]map[string]any, 0, 5)
	for _, gameType := range []int{9, 8, 2, 10, 7} {
		settings = append(settings, map[string]any{"client_type": "java", "game_type": gameType, "skin_id": skinID, "skin_mode": 0, "skin_type": 31})
	}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/user-game-skin-multi", map[string]any{"skin_settings": settings}, out); err != nil {
		return err
	}
	return out.checkCode("/user-game-skin-multi")
}

// UserGameSkinItem 是用户当前配置的皮肤条目。
type UserGameSkinItem struct {
	UserID     any    `json:"user_id"`
	GameType   int    `json:"game_type"`
	SkinType   int    `json:"skin_type"`
	SkinID     string `json:"skin_id"`
	SkinMode   int    `json:"skin_mode"`
	ClientType string `json:"client_type"`
}

// QueryUserSkins 查询用户当前已装备的皮肤。
func (g *Gateway) QueryUserSkins(ctx context.Context) ([]UserGameSkinItem, error) {
	out := &X19Response{}
	body := map[string]any{"user_id": g.Creds.UserID}
	if err := g.Post(ctx, "", "/user-game-skin/query/search-by-type", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/user-game-skin/query/search-by-type"); err != nil {
		return nil, err
	}
	var envelope struct {
		Entities []UserGameSkinItem `json:"entities"`
	}
	if err := json.Unmarshal(out.Data, &envelope); err == nil && len(envelope.Entities) > 0 {
		return envelope.Entities, nil
	}
	var direct []UserGameSkinItem
	if err := json.Unmarshal(out.Data, &direct); err == nil && len(direct) > 0 {
		return direct, nil
	}
	return nil, nil
}

// HasEquippedSkin 检查用户是否已设置皮肤（返回是否已设置及当前皮肤 ID）。
func (g *Gateway) HasEquippedSkin(ctx context.Context) (bool, string, error) {
	skins, err := g.QueryUserSkins(ctx)
	if err != nil {
		return false, "", err
	}
	for _, s := range skins {
		if s.SkinID != "" && s.SkinID != "0" {
			return true, s.SkinID, nil
		}
	}
	return false, "", nil
}
