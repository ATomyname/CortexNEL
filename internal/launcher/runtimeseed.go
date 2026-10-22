// 运行时配置播种：把基础 gameDir 的客户端配置补进目标运行目录。
//
// 与组件同步（modsync.go）的规则刻意不同：组件按大小比对重建，配置只在目标缺失时
// 补写。理由是两者的"真源"不同 —— 组件以服务端要求的 iid+md5 集合为准，必须每次
// 对齐；config / defaultconfigs / options.txt 是客户端在运行目录里自行改写的本地状态
// （键位、画质、mod 各自的配置），每次启动覆盖会与客户端打架。
package launcher

import (
	"os"
	"path/filepath"
)

// seedEntries 是基础 gameDir 中需要播种的配置项。
// 只列进服与客户端启动真正会读到的项；saves / screenshots / logs 等无关目录不搬。
var seedEntries = []string{"config", "defaultconfigs", "options.txt"}

// SeedRuntimeConfig 把基础 gameDir 的配置播种进目标 gameDir，已存在的项一律保留。
func SeedRuntimeConfig(baseGameDir, targetGameDir string) {
	if baseGameDir == "" || targetGameDir == "" {
		return
	}
	for _, name := range seedEntries {
		src := filepath.Join(baseGameDir, name)
		dst := filepath.Join(targetGameDir, name)
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		if info.IsDir() {
			seedTreeIfAbsent(src, dst)
			continue
		}
		seedFileIfAbsent(src, dst)
	}
}

// seedTreeIfAbsent 递归复制目录树，已存在的文件一律保留。
func seedTreeIfAbsent(srcDir, dstDir string) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return
	}
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			seedTreeIfAbsent(src, dst)
			continue
		}
		seedFileIfAbsent(src, dst)
	}
}

// seedFileIfAbsent 在目标不存在或为空时复制文件。
func seedFileIfAbsent(src, dst string) {
	if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
		return
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o644)
}
