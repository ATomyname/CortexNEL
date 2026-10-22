// 组件清单端点（facts §5.1/§3.7）：核心组件、服务器组件与 modInfo 投影。
//
// 与 serveritem.go 的分界：本文件面向"按版本/按 id 批量查组件"，
// serveritem.go 面向"服务器 item 自身的 sub_entities / sub_mod_list 展开"。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ComponentDownloadInfo is the server component metadata returned by the
// client endpoint.  The jar MD5 is the value required by Yggdrasil modInfo.
type ComponentDownloadInfo struct {
	EntityID    string                 `json:"entity_id"`
	ItemID      string                 `json:"item_id"`
	MTypeID     int                    `json:"mtypeid"`
	SubEntities []ComponentDownloadSub `json:"sub_entities"`
	// SubModList 是"该 item 由哪些 mod 组件构成"的 iid 列表（真源官方启动器
	// aov.cs：服务器 item 的响应带 sub_mod_list，官方启动器据此再查每个 mod
	// 的下载信息）。服务器 item 自身不是 mod（官方沙盒 mods 里没有它）。
	SubModList []uint64 `json:"sub_mod_list"`
}

type ComponentDownloadSub struct {
	JarMD5     string `json:"jar_md5"`
	ResName    string `json:"res_name"`
	ResVersion int    `json:"res_version"`
	ResSize    int64  `json:"res_size"`
	// ResURL / ResMD5 是组件的下载地址与归档校验值（真源：Fantnel
	// EntityComponentDownloadInfoResponseSub 的 res_url / res_md5）。
	// 没有这两项就无法补齐本地缺失的组件 —— 客户端会在缺组件的状态下启动。
	ResURL string `json:"res_url"`
	ResMD5 string `json:"res_md5"`
}

// ComponentDetails 取组件的下载详情（含 res_url / res_md5）。
//
// 与 GameCoreMods 的区别：后者只产出 modInfo 需要的 iid+md5，本方法保留下载字段 ——
// 缺了下载地址就无法补齐本地缺失的组件。
// 网关对这批 id 返回 null 时报错而不是静默返回空，否则调用方会以为"服务端没有组件"。
func (g *Gateway) ComponentDetails(ctx context.Context, itemIDs []uint64) ([]ComponentDownloadInfo, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	out := &X19Response{}
	// item_id_list 必须是字符串数组（真源 SearchByIdsQueryAlfa.item_id_list =
	// List<string>）：iid 超出 JSON double 精度（2^53），按数字发送会被服务端
	// 解析丢精度，实测返回 0 实体。
	ids := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		ids = append(ids, strconv.FormatUint(id, 10))
	}
	if err := g.Post(ctx, GatewayBaseURL, "/user-item-download-v2/get-list",
		map[string]any{"item_id_list": ids}, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/user-item-download-v2/get-list"); err != nil {
		return nil, err
	}
	if isJSONNull(out.Data) {
		return nil, fmt.Errorf("netease: 组件详情为空（item_id_list=%v）", itemIDs)
	}
	var components []ComponentDownloadInfo
	if err := json.Unmarshal(out.Data, &components); err != nil {
		return nil, fmt.Errorf("netease: 组件详情解析: %w", err)
	}
	return components, nil
}

// ServerComponentPack 取某个服务器的资源包（/user-item-download-v2，客户端域）。
// 服务器没有独立资源包时网关返回 null，此时返回 (nil, nil)。
func (g *Gateway) ServerComponentPack(ctx context.Context, serverID string) (*ComponentDownloadInfo, error) {
	out := &X19Response{}
	body := map[string]any{"item_id": serverID, "length": 0, "offset": 0}
	if err := g.Post(ctx, ClientBaseURL, "/user-item-download-v2", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/user-item-download-v2"); err != nil {
		return nil, err
	}
	if isJSONNull(out.Data) {
		return nil, nil
	}
	var one ComponentDownloadInfo
	if err := json.Unmarshal(out.Data, &one); err != nil {
		return nil, fmt.Errorf("netease: 服务器资源包解析: %w", err)
	}
	return &one, nil
}

// isJSONNull 判断响应体是否为 JSON null（网关用 null 表示"没有这项"）。
func isJSONNull(data []byte) bool {
	return strings.TrimSpace(string(data)) == "null"
}

type gameCoreModListResponse struct {
	McVersionID int      `json:"mc_version_id"`
	GameType    int      `json:"game_type"`
	IidList     []uint64 `json:"iid_list"`
}

// GameCoreMods 从网关获取指定 MC 版本的核心组件清单 (Fantnel NPFLauncher.GetGameCoreModListAsync)。
func (g *Gateway) GameCoreMods(ctx context.Context, mcVersion string, isRental bool) ([]Mod, error) {
	gameType := 2
	if isRental {
		gameType = 8
	}
	mcVerID := mcVersionToID(mcVersion)

	out := &X19Response{}
	body := map[string]any{"mc_version_id": mcVerID, "game_type": gameType}
	if err := g.Post(ctx, GatewayBaseURL, "/game-auth-item-list/query/search-by-game", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/game-auth-item-list/query/search-by-game"); err != nil {
		return nil, err
	}
	var resp gameCoreModListResponse
	if err := json.Unmarshal(out.Data, &resp); err != nil {
		return nil, fmt.Errorf("netease: 核心组件列表解析: %w", err)
	}
	if len(resp.IidList) == 0 {
		return nil, nil
	}

	detailOut := &X19Response{}
	detailBody := map[string]any{"item_id_list": resp.IidList}
	if err := g.Post(ctx, GatewayBaseURL, "/user-item-download-v2/get-list", detailBody, detailOut); err != nil {
		return nil, err
	}
	if err := detailOut.checkCode("/user-item-download-v2/get-list"); err != nil {
		return nil, err
	}
	var components []ComponentDownloadInfo
	if err := json.Unmarshal(detailOut.Data, &components); err != nil {
		return nil, fmt.Errorf("netease: 核心组件详情解析: %w", err)
	}
	return componentMods(components), nil
}

// FetchAuthoritativeMods 直接向网关请求实时最新的权威组件清单 (包含 MC 核心组件与网络服组件)。
func (g *Gateway) FetchAuthoritativeMods(ctx context.Context, mcVersion, serverID string, isRental bool) ([]Mod, error) {
	var allMods []Mod

	// 1. 获取对应 MC 版本的网易核心组件 (Core Mods)
	coreMods, err := g.GameCoreMods(ctx, mcVersion, isRental)
	if err == nil && len(coreMods) > 0 {
		allMods = append(allMods, coreMods...)
	}

	// 2. 获取服务器特定的组件 (NetGame Mods)
	serverMods, err := g.ServerModInfo(ctx, serverID)
	if err == nil && len(serverMods) > 0 {
		allMods = append(allMods, serverMods...)
	}

	return allMods, nil
}

// componentMods 把组件下载信息投影成 modInfo 需要的 iid+md5 集合。
// 路径形状 <itemID>@<mtypeid>@0.jar 与官方启动器一致（modInfo.go 消费）。
func componentMods(components []ComponentDownloadInfo) []Mod {
	mods := make([]Mod, 0, len(components))
	for _, c := range components {
		if c.ItemID == "" {
			continue
		}
		for _, sub := range c.SubEntities {
			if sub.JarMD5 == "" {
				continue
			}
			path := fmt.Sprintf("%s@%d@0.jar", c.ItemID, c.MTypeID)
			mods = append(mods, Mod{
				ModPath: path,
				ID:      path,
				IID:     c.ItemID,
				MD5:     strings.ToUpper(sub.JarMD5),
				Name:    sub.ResName,
				Version: strconv.Itoa(sub.ResVersion),
			})
		}
	}
	return mods
}

// serverModIIDs 从服务器 item 实体里收集 sub mod 的 iid 列表（去重保序）。
func serverModIIDs(items []ComponentDownloadInfo) []uint64 {
	seen := map[uint64]bool{}
	var iids []uint64
	for _, it := range items {
		for _, id := range it.SubModList {
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			iids = append(iids, id)
		}
	}
	return iids
}
