// 条目名 → 目标路径的安全映射。
//
// 归档内容来自远端、可被影响，条目名是典型的路径穿越面。这里集中所有"这个名字
// 能不能写"的策略，解压与按规则重定位（ExtractMap）共用同一份判据 —— 分散到两处
// 很容易在其中一处漏掉防护。
package archive

import (
	"path/filepath"
	"strings"
)

// windowsReservedNames 是写入即失败或打开设备而非文件的名字。
var windowsReservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true,
	"com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// safeTarget 把归档内的相对条目名映射到 dest 内的绝对路径。
//
// 拒绝时返回非空原因，调用方据此上报；target 仅在原因为空时可用。防护分三层：
//  1. 越界：绝对路径、任何 `..` 路径段、以及 Clean 后逃出 dest；
//  2. NTFS 数据流分隔符 `:` —— foo.txt:stream 会写进备用数据流，正常读取是空文件；
//  3. Windows 名字空间：保留设备名、尾随点/空格（NTFS 会吃掉，两条目塌缩成一个，
//     后解出的覆盖先解出的，内容静默错误）。
//
// 越界按"整段等于 .."判定，而不是按前缀 ".." 判定：拒绝现在会让整次解压判失败
// （见 skipLog），误伤一个合法但恰好以 .. 开头的文件名就会白费一整包下载。
// Clean 后的包含检查仍然保留，作为最终兜底。
func safeTarget(dest, name string) (target, reason string) {
	original := name
	name = filepath.FromSlash(name)
	if name == "" {
		return "", "空条目名"
	}
	if filepath.IsAbs(name) {
		return "", "绝对路径越界（" + original + "）"
	}
	if hasParentComponent(name) {
		return "", "相对路径越界（" + original + "）"
	}
	if strings.ContainsRune(name, ':') {
		return "", "含 NTFS 数据流分隔符（" + original + "）"
	}
	if hasWindowsReservedComponent(name) {
		return "", "Windows 保留设备名（" + original + "）"
	}
	if hasTrailingDotOrSpaceComponent(name) {
		return "", "文件名尾随点或空格，Windows 会吃掉（" + original + "）"
	}
	target = filepath.Join(dest, name)
	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(filepath.Separator)) {
		return "", "相对路径越界（" + original + "）"
	}
	return target, ""
}

// isPathSep 同时认两种分隔符：归档内的名字一律用 /，FromSlash 之后才变成 \。
func isPathSep(r rune) bool { return r == '\\' || r == '/' }

// hasParentComponent 判断路径里是否有整段的 ".."（即试图上跳）。
func hasParentComponent(name string) bool {
	for _, comp := range strings.FieldsFunc(name, isPathSep) {
		if comp == ".." {
			return true
		}
	}
	return false
}

// hasWindowsReservedComponent 逐段检查路径里的 Windows 保留设备名。
// 目录段同样要查 —— `sub/CON/x` 里的 CON 也会让整条路径写不进去。
func hasWindowsReservedComponent(name string) bool {
	for _, comp := range strings.FieldsFunc(name, isPathSep) {
		base := comp
		if i := strings.IndexByte(base, '.'); i > 0 {
			base = base[:i] // "COM1.txt" 的保留判定看第一段之前的部分
		}
		if windowsReservedNames[strings.ToLower(base)] {
			return true
		}
	}
	return false
}

// hasTrailingDotOrSpaceComponent 逐段检查尾随点/空格。NTFS 对任何路径段都会吃掉
// 它们，两条目塌缩成一个文件，后解出的覆盖先解出的 —— 内容静默错误。
// "." 与 ".." 是路径元字符，Join 前就会被 Clean 处理，不算在内。
func hasTrailingDotOrSpaceComponent(name string) bool {
	for _, comp := range strings.FieldsFunc(name, isPathSep) {
		if comp == "." || comp == ".." {
			continue
		}
		if comp != strings.TrimRight(comp, ". ") {
			return true
		}
	}
	return false
}
