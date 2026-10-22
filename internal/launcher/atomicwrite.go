// 原子写：落位文件时不留半份。
//
// 这是 launcher 内跨领域共用的唯一文件写入原语（无业务规则）。组件同步
// （modsync.go）、SDK 运行库落位（sdkruntime.go）与 SDK 运行库补件
// （sdkruntime_fetch.go）都要把文件铺进客户端会在启动中打开的目录，直接写会先
// 截断再写入，读到半份文件等于拿到一个损坏的 jar / dll。前两者对「目标是否已
// 就位」的判定规则不同，那是各自领域的业务规则，留在各自文件里；写入路径必须
// 共用，保证「不留半份」这一条只实现一次。
package launcher

import "os"

// writeFileAtomic 先写临时文件再改名，写入失败时清理临时文件。
func writeFileAtomic(path string, data []byte) bool {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return false
	}
	return renameIntoPlace(tmp, path)
}

// renameIntoPlace 把已写好的临时文件改名到目标路径，失败时清理临时文件。
// 组件同步与 SDK 运行库补件的下载落位走这里；archive.Download 现在自身已
// 原子落位（先写临时文件再改名），这条铁律在两侧各按各自的写入形态实现。
func renameIntoPlace(tmp, dst string) bool {
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return false
	}
	return true
}
