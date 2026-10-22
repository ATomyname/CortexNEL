// 按服隔离的运行沙盒准备（编排）。
//
// 沙盒布局对齐参考实现 Fantnel：<Game 根>/Runtime/<gameId>-<roleName>/.minecraft，
// 使不同服务器的 mods / config / options.txt 互不污染。
//
// 沙盒不是「空白目录」：客户端以 --gameDir 指向它（见 arguments.go），Forge 只从
// 沙盒的 mods 目录加载组件，因此沙盒必须先播种基础客户端内容，再叠加服务器组件。
// 各步的规则与理由分散在对应文件：组件见 modsync.go，配置见 runtimeseed.go，
// 存量误停用的撤销见 legacycoremods.go。本文件只负责编排顺序与报告。
package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const (
	// modFileSuffix 是组件文件后缀（只有 .jar 会被 Forge 加载）。
	modFileSuffix = ".jar"
	// disabledModSuffix 是停用组件的后缀，与 app 层插件开关约定一致
	// （internal/app/plugin_binding.go 的 TogglePlugin）。
	disabledModSuffix = ".disabled"
	// defaultOptionsContent 是首次启动的 options.txt 初值。
	// 只有基础 gameDir 也没有 options.txt 时才用到（见 SeedRuntimeConfig）。
	defaultOptionsContent = "guiScale:2\nlang:zh_cn\nmaxFps:120\n"
)

// RuntimeSource 是一次运行目录准备的输入来源。
type RuntimeSource struct {
	// BaseGameDir 是基础客户端 gameDir（组件与配置的播种来源）。
	BaseGameDir string
	// ComponentCacheDir 是 nexus 组件缓存：按网关权威清单下载下来的该服组件。
	// 为空表示没有（本地已有网易缓存时通常如此）。
	ComponentCacheDir string
	GameID            string
	RoleName          string
}

// RuntimeSyncReport 是一次运行目录准备的同步结果，供调用方记录到日志。
// 该报告存在的理由：沙盒曾静默丢掉基础核心组件而无人察觉，同步结果必须可核实（规范 I2）。
type RuntimeSyncReport struct {
	// BaseMods 是基础组件铺进沙盒后已就位的组件数（含本就一致、未复制的）。
	BaseMods int
	// CachedMods 是从 nexus 组件缓存叠加后已就位的组件数（权威清单产物）。
	CachedMods int
	// RestoredCoreMods 是撤销旧规则误停用后重新启用的核心组件数。
	RestoredCoreMods int
}

// PrepareGameRuntime 为某个服务器与角色准备独立运行沙盒，返回沙盒 .minecraft 路径
// 与本次同步结果。GameID 为空或非 Windows 时返回空串与零值报告，调用方应退回
// 公共客户端目录。
//
// 播种顺序（后者覆盖前者）：基础 gameDir（本应用数据目录里的客户端根）→
// nexus 组件缓存。缓存排最后：它是按网关权威清单对账过的（iid+md5），
// 必须以权威清单的版本为准。
func PrepareGameRuntime(src RuntimeSource) (string, RuntimeSyncReport) {
	var report RuntimeSyncReport
	if src.GameID == "" || runtime.GOOS != "windows" {
		return "", report
	}
	roleName := src.RoleName
	if roleName == "" {
		roleName = "default"
	}

	base := neteaseGameRoot()
	if base == "" {
		// 没有网易安装时退到本应用数据目录，保持沙盒语义不变。
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			base = filepath.Join(appdata, "nexus")
		} else {
			base = "."
		}
	}

	// 先撤销旧规则的误停用：组件同步会尊重 .disabled，不先恢复就等于继续沿用那个 bug。
	// 放在建沙盒之前是因为它只作用于基础目录，与沙盒能否创建无关 —— 沙盒创建失败时
	// 客户端会退回基础 gameDir 启动，那时更需要核心组件是启用的。
	if src.BaseGameDir != "" {
		report.RestoredCoreMods = RestoreDisabledCoreComponents(filepath.Join(src.BaseGameDir, "mods"))
	}

	sandbox := filepath.Join(base, "Runtime", fmt.Sprintf("%s-%s", src.GameID, roleName), ".minecraft")
	if err := os.MkdirAll(filepath.Join(sandbox, "mods"), 0o755); err != nil {
		return "", report
	}

	report.BaseMods = SeedBaseMods(src.BaseGameDir, sandbox)
	SeedRuntimeConfig(src.BaseGameDir, sandbox)
	report.CachedMods = SyncComponentCache(src.ComponentCacheDir, sandbox)
	EnsureOptionsTxt(sandbox)
	return sandbox, report
}

// EnsureOptionsTxt 在 options.txt 缺失或为空时写入默认值。
func EnsureOptionsTxt(clientDir string) {
	path := filepath.Join(clientDir, "options.txt")
	info, err := os.Stat(path)
	if err == nil && info.Size() > 0 {
		return
	}
	if err != nil && !os.IsNotExist(err) {
		return
	}
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(defaultOptionsContent), 0o644)
}
