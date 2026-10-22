// Package clientdata 管理 nexus 自有的版本数据目录 —— 下载回来的客户端本体存放处。
//
// 为什么不写用户的网易安装目录：客户端本体含 versions / libraries / assets，
// 与官方启动器共用同一份文件会互相覆盖，出问题难以归因（规范 P6 同理）。
//
// 选址由本包决定并写回 config.json（用户指定：取可用空间最大的盘，在其根目录下创建），
// 避免换机器或重装后丢失 —— 750MB 级别的数据重复下载一次代价很高。
package clientdata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"nexus/internal/store"
)

const (
	// DirName 是版本数据目录在盘根下的名字。
	DirName = "CortexNEL"
	// ClientSubdir 是版本数据目录下存放客户端本体的子目录。
	ClientSubdir = "client"
)

// Root 返回版本数据目录（<盘>:\CortexNEL\client），必要时选址、创建并写回设置。
//
// 规则：
//   - 设置里已有路径且目录可用 → 直接沿用（已下载的本体不因重启而失效）；
//   - 已有路径但不可用（盘被拔、权限变化）→ 重新选址，而不是让下载整体失败；
//   - 首次调用 → 取可用空间最大的固定盘创建，并把结果写回 config.json。
//
// 有副作用：首次选址会创建目录并保存设置。
func Root(paths *store.Paths, settings *store.Settings) (string, error) {
	return rootWith(paths, settings, pickLargestFreeDrive)
}

// rootWith 是 Root 的可注入版本：选址器作为参数传入，使测试不必触碰真实磁盘。
func rootWith(paths *store.Paths, settings *store.Settings, pick func() (string, bool)) (string, error) {
	if dir := settings.ClientDataDir; dir != "" {
		if err := os.MkdirAll(dir, 0o755); err == nil {
			return dir, nil
		}
	}
	dir, err := resolveRoot(pick)
	if err != nil {
		return "", err
	}
	settings.ClientDataDir = dir
	if err := store.SaveSettings(paths, *settings); err != nil {
		return "", fmt.Errorf("clientdata: 写回版本数据目录: %w", err)
	}
	return dir, nil
}

// resolveRoot 由盘根拼出版本数据目录并创建。pick 返回可用空间最大的盘根（如 `D:\`）。
func resolveRoot(pick func() (string, bool)) (string, error) {
	drive, ok := pick()
	if !ok {
		return "", errors.New("clientdata: 未找到可用磁盘，无法创建版本数据目录")
	}
	dir := filepath.Join(drive, DirName, ClientSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("clientdata: 创建版本数据目录 %s: %w", dir, err)
	}
	return dir, nil
}

// VersionDir 返回某个 Minecraft 版本的客户端本体目录（位于版本数据目录下）。
func VersionDir(root, mcVersion string) string {
	return filepath.Join(root, "versions", mcVersion)
}

// ComponentsDir 返回某个服务器的组件缓存目录。
//
// 按服隔离：不同服的权威组件集不同，混在一个目录里会互相覆盖，
// 而服务端校验的正是 iid+md5 集合（facts §3.7）。
func ComponentsDir(root, serverID string) string {
	return filepath.Join(root, "components", serverID)
}
