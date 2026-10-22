//go:build !windows

package launcher

// fixedDrives 非 Windows 平台没有盘符概念，返回空 —— 候选根列表为空。
func fixedDrives() []rune { return nil }
