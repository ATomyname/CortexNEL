// 完整性校验：判断落盘内容是否与分发方给出的指纹一致。
//
// 安装标记（internal/install 的 marker）与下载前的归档校验都走这里 —— 判据集中
// 一处，避免各处自行拼 MD5 而在大小写上各写各的（网关回小写、modInfo 给大写是实测）。
package archive

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// FileMD5 计算文件 MD5（小写 hex）。
func FileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Matches 判断文件 MD5 是否与期望一致（不区分大小写）。文件不存在返回 false。
func Matches(path, wantMD5 string) bool {
	sum, err := FileMD5(path)
	if err != nil {
		return false
	}
	return strings.EqualFold(sum, wantMD5)
}
