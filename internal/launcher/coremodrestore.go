// 恢复被误停用的核心组件（对存量安装的一次性修复，迁移完毕后可整文件删除）。
//
// 背景：已移除的旧规则把文件名里的组件类型标签 `@3` 当成「网易反作弊探针」，
// 在启动路径上把命中的 jar 一律改名成 `.disabled`。而 `@3` 的实际含义是
// MTypeId=3（核心模组）—— 见 internal/netease/modinfo.go 的 ParseJarName：
// 核心模组的 modPath 正是 `{ItemId}@3@0.jar`。网易的 RPC/网络控制组件
// （com.netease.mc.mod.network.networkMod.NetworkSocketMod 等）就装在这类
// 核心组件里，被停用后客户端不再回连启动器控制通道，进服不会触发。
//
// 恢复范围刻意收窄，避免覆盖用户意图：
//   - 只认 `@3@` 核心组件形状的 `.jar.disabled`，其他停用组件不动；
//   - 只在同名启用文件确实不存在时才改名回来；
//   - 幂等：跑第二遍无文件可改，返回 0。
//
// 用户若确实要停用某个核心组件，可在「白端设置」里重新切换（同一 .disabled 约定，
// 见 internal/app/plugin_binding.go 的 TogglePlugin）。
package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// coreComponentTag 是核心组件文件名（{ItemId}@{MTypeId}@{EntityId}.jar）里
// MTypeId=3 的那一段。用完整形状 `@3@` 而非宽匹配 `@3`，避免误伤巧合含 `@3` 的文件。
const coreComponentTag = "@3@"

// RestoreDisabledCoreComponents 恢复 modsDir 里被旧规则误停用的核心组件，
// 返回重新启用的组件数。
func RestoreDisabledCoreComponents(modsDir string) int {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return 0
	}
	restored := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, disabledModSuffix) {
			continue
		}
		enabled := strings.TrimSuffix(name, disabledModSuffix)
		if !strings.HasSuffix(enabled, modFileSuffix) || !strings.Contains(enabled, coreComponentTag) {
			continue
		}
		to := filepath.Join(modsDir, enabled)
		if _, err := os.Stat(to); err == nil {
			// 同名启用文件已存在：这次停用另有来源（例如用户在界面里停用的是另一份），不碰。
			continue
		}
		if os.Rename(filepath.Join(modsDir, name), to) == nil {
			restored++
		}
	}
	return restored
}
