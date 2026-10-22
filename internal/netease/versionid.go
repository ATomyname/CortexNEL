// 版本号 → 网易 mc_version_id 映射（单一来源）。
//
// 组件清单查询（mc_version_id）与客户端本体包查询（mc_version）用同一套编号，
// 两处各写一份映射迟早会漂移，因此只允许这里一处实现。
//
// 编号规则（/game-patch-info 实测锚点）：id = 1_000_000 + minor*1000 + patch。
// 1.21→1021000、1.21.8→1021008、1.21.10→1021010（name=1.21.10-363051_…，
// 1021009/1021011 均"目标找不到"）、1.7.10→1007010。
// 历史缺陷：旧 switch 用 Contains 匹配，把 "1.21.10" 错映射进 1.21（1021000），
// 装错版本包后 verifyClientLayout 找 versions/1.21.10/ 必然失败。
package netease

import (
	"strconv"
	"strings"
)

// McVersionID 把 Minecraft 版本号（如 "1.20"）转成网易的 mc_version_id。
func McVersionID(version string) int { return mcVersionToID(version) }

func mcVersionToID(version string) int {
	if id, ok := parseVersionID(strings.ToLower(version)); ok {
		return id
	}
	return parseVersionIDLegacy(strings.ToLower(version))
}

// parseVersionID 按 "1.<minor>[.<patch>]" 推导 id：patch 占十进制后三位位权
// （minor*1000 + patch），两位补丁号在位权内（1.21.10 → 1021000+10 = 1021010）。
// 尾部噪声先截断；形态非法交回兜底表。
func parseVersionID(v string) (int, bool) {
	cut := strings.TrimLeft(v, "0123456789.")
	parts := strings.Split(strings.TrimSuffix(v, cut), ".")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "1" || parts[1] == "" {
		return 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, false
	}
	patch := 0
	if len(parts) == 3 {
		if parts[2] == "" {
			return 0, false
		}
		p, err := strconv.Atoi(parts[2])
		if err != nil {
			return 0, false
		}
		patch = p
	}
	if minor < 0 || minor > 200 || patch < 0 || patch > 999 {
		return 0, false
	}
	return 1000000 + minor*1000 + patch, true
}

// parseVersionIDLegacy 是非标准版本写法的兜底表（Contains 匹配，特异度降序）。
func parseVersionIDLegacy(v string) int {
	switch {
	case strings.Contains(v, "1.21.8"):
		return 1021008
	case strings.Contains(v, "1.21"):
		return 1021000
	case strings.Contains(v, "1.20.6"):
		return 1020006
	case strings.Contains(v, "1.20"):
		return 1020000
	case strings.Contains(v, "1.19.2"):
		return 1019002
	case strings.Contains(v, "1.18"):
		return 1018000
	case strings.Contains(v, "1.16"):
		return 1016000
	case strings.Contains(v, "1.15"):
		return 1015000
	case strings.Contains(v, "1.14.3"):
		return 1014003
	case strings.Contains(v, "1.13.2"):
		return 1013002
	case strings.Contains(v, "1.12.2"):
		return 1012002
	case strings.Contains(v, "1.12"):
		return 1012000
	case strings.Contains(v, "1.11.2"):
		return 1011002
	case strings.Contains(v, "1.10.2"):
		return 1010002
	case strings.Contains(v, "1.9.4"):
		return 1009004
	case strings.Contains(v, "1.8.9"):
		return 1008009
	case strings.Contains(v, "1.8.8"):
		return 1008008
	case strings.Contains(v, "1.8"):
		return 1008000
	case strings.Contains(v, "1.7.10"):
		return 1007010
	case strings.Contains(v, "1.7.2"):
		return 1007002
	case strings.Contains(v, "1.6.4"):
		return 1006004
	default:
		return 1020000
	}
}
