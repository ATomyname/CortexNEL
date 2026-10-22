// 进服前置资产补齐：实现 service.AssetProvider（由 service 定义接口、此处实现）。
//
// 修复的是这样一类故障：本地没有某服的组件，旧逻辑只能退回默认客户端目录，
// 于是"点 A 服却起了 B 服的客户端"。这里改为本地没有就下载，补不齐就明确报错。
package app

import (
	"context"
	"fmt"

	"nexus/internal/account"
	"nexus/internal/clientdata"
	"nexus/internal/install"
	"nexus/internal/netease"
	"nexus/internal/service"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

// 编译期断言：接口漂移立刻暴露。
var _ service.AssetProvider = (*joinAssets)(nil)

// joinAssets 补齐"启动镜像客户端必须具备的本地资产"。
//
// 一切以网关权威清单为准（iid+md5），每次进服都对账：本机网易安装里的游戏内容
// 一律不复用 —— 其版本与完整性无从对账，服务端更新后旧组件会静默过期。
// 存储沿用既有规则：客户端本体与组件都落 clientdata 选址的版本数据目录。
type joinAssets struct {
	accounts  *account.Manager
	events    *telemetry.Bus
	logger    *telemetry.Logger
	installer *install.Installer
	// resolveRoot 返回版本数据目录（由 app 注入 clientdata.Root，含选址与写回设置）。
	resolveRoot func() (string, error)
}

// EnsureJoinAssets 实现 service.AssetProvider。
func (j *joinAssets) EnsureJoinAssets(ctx context.Context, req service.AssetRequest) (service.AssetResult, error) {
	if req.ServerID == "" {
		return service.AssetResult{}, nil
	}

	acct, err := j.accounts.Get(req.AccountID)
	if err != nil {
		return service.AssetResult{}, fmt.Errorf("读取账号失败: %w", err)
	}
	token, err := j.accounts.Token(req.AccountID)
	if err != nil || token == "" {
		return service.AssetResult{}, fmt.Errorf("补齐进服资产需要可用的账号 token（请先登录）: %w", err)
	}
	root, err := j.resolveRoot()
	if err != nil {
		return service.AssetResult{}, err
	}
	gw := netease.NewGateway(netease.Credentials{UserID: acct.UserID, Token: token})

	// ① 客户端本体：MD5 标记判已装，缺则下载（带进度与 MD5 校验）。
	// 返回值是实际客户端根（归档条目在 .minecraft/ 前缀下，见 install.clientRoot）。
	clientDir, err := j.installer.EnsureClient(ctx, gw, root, req.MCVersion)
	if err != nil {
		return service.AssetResult{}, err
	}

	// ② 组件：核心 + 该服权威清单，每次按 iid+md5 对账，有差异只下差异。
	cacheDir := clientdata.ComponentsDir(root, req.ServerID)
	installed, err := j.installer.EnsureComponents(ctx, gw, req.MCVersion, req.ServerID, cacheDir, req.IsRental)
	if err != nil {
		return service.AssetResult{}, err
	}
	j.logger.Info("install", "进服资产已按权威清单对账就绪",
		"serverID", req.ServerID, "mcVersion", req.MCVersion, "isRental", req.IsRental,
		"newComponents", installed,
		"cacheDir", cacheDir, "clientRoot", clientDir)

	return service.AssetResult{ClientDir: clientDir, ComponentCacheDir: cacheDir}, nil
}

// newJoinAssets 组装资产补齐器：安装进度发布为 install.progress（事件清单 §7），
// 每个关键节点（下载/校验/解压/落位/对账/跳过）同步落日志（module=install）。
func newJoinAssets(paths *store.Paths, accounts *account.Manager, bus *telemetry.Bus,
	logger *telemetry.Logger, settings *store.Settings) *joinAssets {
	inst := install.New(nil)
	inst.Report = func(p install.Progress) {
		bus.Publish("install.progress", "ok", map[string]any{
			"stage":   string(p.Stage),
			"subject": p.Subject,
			"done":    p.Done,
			"total":   p.Total,
			"message": p.Message,
		})
	}
	inst.Log = logger
	return &joinAssets{
		accounts:    accounts,
		events:      bus,
		logger:      logger,
		installer:   inst,
		resolveRoot: func() (string, error) { return clientdata.Root(paths, settings) },
	}
}
