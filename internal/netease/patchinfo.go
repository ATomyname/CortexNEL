// 客户端本体包端点（/game-patch-info，facts §5.1）：基础包与版本包的下载信息。
// 测试见 patchinfo_test.go。
package netease

import (
	"context"
	"encoding/json"
	"fmt"
)

// ClientPatchInfo 是客户端本体包的信息（/game-patch-info）。
// 字段名取自实测响应（1.20 基础包）：
//
//	{"url":"https://x19.gdl.netease.com/mc_client/base-…​.7z","size":150390122,
//	 "md5":"46787c7777dc29440c75e64534d2dacc","name":"base-150832_1642684473.7z",
//	 "version":"3","mc_version":0,"core_lib_name":"CoreLibs_0.7z","core_lib_size":0,
//	 "core_lib_md5":"","core_lib_url":""}
//
// 基础包（mc_version=0）没有核心库；版本包带 core_lib_* 一组字段。
type ClientPatchInfo struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	MD5         string `json:"md5"`
	Size        int64  `json:"size"`
	Version     string `json:"version"`
	McVersion   int    `json:"mc_version"`
	CoreLibName string `json:"core_lib_name"`
	CoreLibURL  string `json:"core_lib_url"`
	CoreLibMD5  string `json:"core_lib_md5"`
	CoreLibSize int64  `json:"core_lib_size"`
}

// ClientPatchInfo 取客户端本体包信息：mcVersionID 为 0 取基础包，否则取该版本包
// （版本号取值见 mcVersionToID；真源 Fantnel NPFLauncher.GetMinecraftClientLibsAsync）。
func (g *Gateway) ClientPatchInfo(ctx context.Context, mcVersionID int) (*ClientPatchInfo, error) {
	out := &X19Response{}
	if err := g.Post(ctx, ClientBaseURL, "/game-patch-info",
		map[string]any{"mc_version": mcVersionID}, out); err != nil {
		return nil, err
	}
	if err := out.checkCode("/game-patch-info"); err != nil {
		return nil, err
	}
	var info ClientPatchInfo
	if err := json.Unmarshal(out.Data, &info); err != nil {
		return nil, fmt.Errorf("netease: 客户端本体包信息解析: %w", err)
	}
	if info.URL == "" {
		return nil, fmt.Errorf("netease: 客户端本体包缺少下载地址（mc_version=%d）", mcVersionID)
	}
	return &info, nil
}
