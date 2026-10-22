//go:build windows

package clientdata

import "golang.org/x/sys/windows"

// pickLargestFreeDrive 返回可用空间最大的固定盘根（如 `D:\`）；找不到返回 false。
//
// 只认固定盘：网络盘与可移动盘会在拔盘后失效，把 750MB 级的客户端本体放上去
// 等于随时变成"要重新下载"。可用空间取用户可用字节数（配额）而非总剩余。
func pickLargestFreeDrive() (string, bool) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return "", false
	}
	best := ""
	var bestFree uint64
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		ptr := windows.StringToUTF16Ptr(root)
		if windows.GetDriveType(ptr) != windows.DRIVE_FIXED {
			continue
		}
		var free, total, totalFree uint64
		if err := windows.GetDiskFreeSpaceEx(ptr, &free, &total, &totalFree); err != nil {
			continue
		}
		if free > bestFree {
			best, bestFree = root, free
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}
