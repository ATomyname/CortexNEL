//go:build windows

// 固定盘枚举：探测网易客户端安装位置时用哪些盘。
//
// 只认固定盘 —— 网易启动器只往固定盘装（实测 D 盘默认），光驱/网络盘探测
// 必然失败，可移动盘拔了就失效。此判据与 clientdata 选数据根的判据一致，
// 但用途不同（那里是"往哪放"，这里是"去哪找"），各自持有实现。
package launcher

import "golang.org/x/sys/windows"

// fixedDrives 返回本机固定盘的盘符（A-Z 升序）。枚举失败返回空 ——
// 调用方得到空候选列表，与"没有任何盘装了网易客户端"同路。
func fixedDrives() []rune {
	drives := make([]rune, 0, 26)
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return drives
	}
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if windows.GetDriveType(windows.StringToUTF16Ptr(root)) != windows.DRIVE_FIXED {
			continue
		}
		drives = append(drives, rune('A'+i))
	}
	return drives
}
