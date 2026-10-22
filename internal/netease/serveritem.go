// 服务器 item 端点（facts §5.1）：解析服务器 item 自身的 sub_entities 与
// sub_mod_list，产出该服的组件下载信息与 modInfo 投影。
//
// 与 component.go 的分界：本文件只处理"服务器 item 这一个实体"的两种响应形状，
// 不做按版本/按 id 的批量查询。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// serverItemDetails 查询服务器 item 自身的元数据（含 sub_entities 与 sub_mod_list）。
// 响应可能是单实体或实体列表两种形状（实测 /user-item-download-v2 两种都出现过）。
func (g *Gateway) serverItemDetails(ctx context.Context, serverID string) ([]ComponentDownloadInfo, error) {
	out := &X19Response{}
	body := map[string]any{"item_id": serverID, "length": 0, "offset": 0}
	if err := g.Post(ctx, ClientBaseURL, "/user-item-download-v2", body, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/user-item-download-v2"); err != nil {
		return nil, err
	}
	var components []ComponentDownloadInfo
	if err := json.Unmarshal(out.Data, &components); err == nil && components != nil {
		return components, nil
	}
	var one ComponentDownloadInfo
	if err := json.Unmarshal(out.Data, &one); err != nil {
		return nil, fmt.Errorf("netease: 服务器组件解析: %w", err)
	}
	return []ComponentDownloadInfo{one}, nil
}

// ServerComponents 返回该服的组件下载信息（服务器 mod 集合包，供安装对账）。
//
// 真源（实测 2026-09 布吉岛）：服务器 item 的 sub_entities 指向一个 mod 集合包
// （如 2026-09-04-01.7z，95MB，条目为 .minecraft/mods/*.jar），服务器 item 的
// sub_mod_list 是集合内 mod 的 iid 清单（用于落位后校验完整性）。二者都由
// 本方法原样返回，安装器负责下载-解压-落位-校验（见 internal/install）。
// 注意：sub_mod_list 的 iid 不能通过 /user-item-download-v2/get-list 查询
// （实测两域均返回空，也不可用 item_id 逐个查——code 16 目标找不到）。
func (g *Gateway) ServerComponents(ctx context.Context, serverID string) ([]ComponentDownloadInfo, error) {
	return g.serverItemDetails(ctx, serverID)
}

// ServerModInfo fetches the component set for a server（modInfo 投影）。
// 展开服务器 item 的 sub_mod_list —— modInfo 描述的必须与 install 实际落位的
// mod 套件一致（服务器 item 自身不是 mod，不计入）。
func (g *Gateway) ServerModInfo(ctx context.Context, serverID string) ([]Mod, error) {
	components, err := g.ServerComponents(ctx, serverID)
	if err != nil {
		return nil, err
	}
	return componentMods(components), nil
}
