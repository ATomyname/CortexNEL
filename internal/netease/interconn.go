// interconn 会话端点（facts §5.1/§6.4）：建立并启动游戏会话。
// code 10 = 未 login-start（先调 InterconnLoginStart 再 InterconnStart）。
package netease

import "context"

// InterconnLoginStart 建立 interconn 会话（facts §5.1/§6.4：code 10 = 未 login-start）。
func (g *Gateway) InterconnLoginStart(ctx context.Context) error {
	body := map[string]any{"strict_mode": true}
	return g.Post(ctx, InterconnBaseURL, "/interconn/web/game-play-v2/login-start", body, nil)
}

// InterconnStart 启动游戏会话（facts §5.1）。
func (g *Gateway) InterconnStart(ctx context.Context, gameID string) (*X19Response, error) {
	body := map[string]any{
		"game_id": gameID, "game_type": MasterTypeGame,
		"strict_mode": true, "item_list": []string{"10000"},
	}
	out := &X19Response{}
	if err := g.Post(ctx, InterconnBaseURL, "/interconn/web/game-play-v2/start", body, out); err != nil {
		return nil, err
	}
	return out, nil
}
