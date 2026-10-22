// 游戏角色端点（facts §5.1）：角色查询与创建。
// gameID 维度与网络服/租赁服共用 GameCharacter 形状。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// GameCharacter 是服务器角色的最小稳定形状。
type GameCharacter struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// QueryGameCharacters 查询角色列表（facts §5.1；gateway code 22 未解，facts §6.4）。
func (g *Gateway) QueryGameCharacters(ctx context.Context, gameID string, offset, length int) (*X19Response, error) {
	body := map[string]any{
		"offset": offset, "length": length,
		"user_id": g.Creds.UserID, "game_id": gameID, "game_type": MasterTypeGame,
	}
	out := &X19Response{}
	if err := g.Post(ctx, "", "/game-character/query/user-game-characters", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// QueryRoles 查询用户在指定游戏下的角色，兼容 items / list 信封与裸数组。
func (g *Gateway) QueryRoles(ctx context.Context, gameID string, offset, length int) ([]GameCharacter, error) {
	out := &X19Response{}
	body := map[string]any{"offset": offset, "length": length, "user_id": g.Creds.UserID, "game_id": gameID, "game_type": MasterTypeGame}
	if err := g.Post(ctx, "", "/game-character/query/user-game-characters", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/game-character/query/user-game-characters"); err != nil {
		return nil, err
	}
	var envelope struct {
		Items []GameCharacter `json:"items"`
		List  []GameCharacter `json:"list"`
	}
	if err := json.Unmarshal(out.Data, &envelope); err == nil {
		if envelope.Items != nil {
			return envelope.Items, nil
		}
		if envelope.List != nil {
			return envelope.List, nil
		}
	}
	var direct []GameCharacter
	if err := json.Unmarshal(out.Data, &direct); err != nil {
		return nil, fmt.Errorf("netease: 角色列表解析: %w", err)
	}
	return direct, nil
}

// CreateRole 在指定游戏下创建角色。
func (g *Gateway) CreateRole(ctx context.Context, gameID, name string) error {
	out := &X19Response{}
	body := map[string]any{"game_id": gameID, "game_type": MasterTypeGame, "user_id": g.Creds.UserID, "name": name}
	if err := g.Post(ctx, "", "/game-character", body, out); err != nil {
		return err
	}
	return out.checkCode("/game-character")
}
