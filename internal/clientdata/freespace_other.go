//go:build !windows

package clientdata

// pickLargestFreeDrive 非 Windows 平台没有"盘根"概念，返回 false，
// 由调用方给出可读错误（nexus 的发布目标只有 windows/amd64，规范 §2）。
func pickLargestFreeDrive() (string, bool) { return "", false }
