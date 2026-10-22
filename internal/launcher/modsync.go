// 组件目录同步：把基础客户端的组件与该服务器的组件铺进目标运行目录。
//
// 为什么必须同步基础组件：客户端以 --gameDir 指向按服沙盒（见 arguments.go 的
// `--gameDir ctx.WorkDir`），Forge 只从 <gameDir>/mods 加载组件。沙盒若只放
// 服务器组件，基础客户端的核心组件（网易 RPC/网络控制组件即在其中）就整批
// 缺失，客户端永远不会回连启动器控制通道，进服不触发。
//
// 与参考实现的差异：Fantnel 的 InstallerService 会把整套客户端装进 Runtime 目录，
// nexus 不复制下载职责（见 docs/design/ARCHITECTURE.md §4），因此改为从既有
// 基础 gameDir 同步。
//
// 本文件只处理组件（mods/*.jar）。运行时配置（config / defaultconfigs / options.txt）
// 的播种规则不同，单独放在 runtimeseed.go。
package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// SeedBaseMods 把基础 gameDir 的组件铺进目标 gameDir，返回已就位的组件数
// （含本就一致、未复制的）。
//
// 每次启动都按大小比对重建：服务端校验的是 iid+md5 集合
// （docs/facts/NETEASE-PROTOCOL.md §3.7），基础组件必须与服务端预期一致。
// 跳过 *.disabled：那是白端设置里用户主动停用的组件（约定见 sandbox.go），
// 用户意图必须延续到沙盒，否则界面上的开关形同虚设。
func SeedBaseMods(baseGameDir, targetGameDir string) int {
	if baseGameDir == "" || targetGameDir == "" {
		return 0
	}
	return copyMods(filepath.Join(baseGameDir, "mods"), filepath.Join(targetGameDir, "mods"))
}

// SyncComponentCache 把 nexus 组件缓存叠加进目标 gameDir，返回已就位的组件数。
//
// 缓存里是按网关权威清单对账过的该服组件（internal/install 的产物，含核心与
// 服务器组件）。调用方必须把它排在基础组件**之后**应用：否则本地旧副本会盖掉
// 刚对账回来的正确版本，而服务端校验的正是 iid+md5 集合（facts §3.7）。
func SyncComponentCache(cacheDir, targetGameDir string) int {
	if cacheDir == "" || targetGameDir == "" {
		return 0
	}
	return copyMods(cacheDir, filepath.Join(targetGameDir, "mods"))
}

// copyMods 把 srcModsDir 下已启用的组件按需复制进 dstModsDir，返回已就位的组件数。
// 源与目标同一目录时直接返回 0（无需复制）。
func copyMods(srcModsDir, dstModsDir string) int {
	if filepath.Clean(srcModsDir) == filepath.Clean(dstModsDir) {
		return 0
	}
	entries, err := os.ReadDir(srcModsDir)
	if err != nil {
		return 0
	}
	if err := os.MkdirAll(dstModsDir, 0o755); err != nil {
		return 0
	}
	synced := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, modFileSuffix) {
			continue
		}
		if copyFileIfChanged(filepath.Join(srcModsDir, name), filepath.Join(dstModsDir, name)) {
			synced++
		}
	}
	return synced
}

// findServerModsSource 定位该服务器的组件来源目录（缓存目录优先，其次独立运行目录）。
func findServerModsSource(gameID string) string {
	for _, root := range neteaseInstallRoots() {
		for _, cand := range []string{
			filepath.Join(root, "MCLDownload", "cache", "game", gameID, ".minecraft", "mods"),
			filepath.Join(root, "MCLDownload", "Game", gameID, ".minecraft", "mods"),
		} {
			if isExistingDir(cand) {
				return cand
			}
		}
	}
	return ""
}

// copyFileIfChanged 复制组件文件；目标已存在且大小一致时跳过复制但仍记为已就位。
//
// 只比大小就够：服务端校验的是 iid+md5 集合，条目在同步前已由上游对账
// （docs/facts/NETEASE-PROTOCOL.md §3.7）。原生库不能这么比（见 sdkruntime.go）。
func copyFileIfChanged(src, dst string) bool {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false
	}
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.Size() == srcInfo.Size() {
		return true
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return false
	}
	return writeFileAtomic(dst, data)
}
