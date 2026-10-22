// SDK 运行库来源：判断从哪儿拿到这份包。
//
// 词汇约定（四份文件各一名一责）：清单（有哪些文件）见 sdkruntime_manifest.go，
// 来源（本文件，从哪儿拿），补件（把缺失的从 CDN 下载回来）见 sdkruntime_fetch.go，
// 落位（铺进 natives/runtime）见 sdkruntime.go。
//
// SDK 运行库包是版本无关的（1.8.9 / 1.12.2 / 1.20 / 1.21 共用同一套），
// 因此「任何一份齐全副本」都是合法来源。
//
// 只认齐全副本，绝不随便挑一份缺件的 —— 挑了就等于把缺件藏起来，客户端会在
// Library 的 native 调用处抛 UnsatisfiedLinkError，而日志上只看得见「启动后崩了」。
package launcher

import (
	"os"
	"path/filepath"
	"sort"
)

// sdkRuntimePackDirName 是启动器置备的 SDK 运行库包目录名，位于客户端根下
// （与 versions/ 同级）。这是首选取件点：包随启动器安装分发，不走 CDN
// （见 docs/facts/NETEASE-PROTOCOL.md §10.6）。
const sdkRuntimePackDirName = "sdkruntime"

// ResolveSdkRuntimeSource 挑一份齐全的 SDK 运行库副本作为取件来源，找不到返回空串。
//
// 顺序：启动器置备的包目录优先，其次自愈归集已有版本目录里的 natives/runtime。
// 后者让存量安装不用重装也能补齐，但它只是取件来源，不是落位位置。
func ResolveSdkRuntimeSource(clientDir string) string {
	if clientDir == "" {
		return ""
	}
	if pack := filepath.Join(clientDir, sdkRuntimePackDirName); isCompleteSdkRuntime(pack) {
		return pack
	}
	for _, dir := range versionNativesRuntimeDirs(clientDir) {
		if isCompleteSdkRuntime(dir) {
			return dir
		}
	}
	return ""
}

// versionNativesRuntimeDirs 列出客户端根下各版本的 natives 运行库目录。
// natives 目录名随版本分发包而异（natives / natives-windows-x86_64 等），
// 因此按 versions/<ver>/<natives 变体>/runtime 匹配而不是写死一个。
func versionNativesRuntimeDirs(clientDir string) []string {
	var dirs []string
	for _, pattern := range []string{
		filepath.Join(clientDir, "versions", "*", "natives", "runtime"),
		filepath.Join(clientDir, "versions", "*", "natives-*", "runtime"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		dirs = append(dirs, matches...)
	}
	sort.Strings(dirs)
	return dirs
}

// isCompleteSdkRuntime 判断目录是否已具备完整权威清单（0 字节不算）。
func isCompleteSdkRuntime(dir string) bool {
	if dir == "" {
		return false
	}
	for _, name := range sdkRuntimeManifest {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}
