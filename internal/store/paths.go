// Package store 是版本化配置落盘与凭据唯一落盘点（架构文档 §9，P5）。
package store

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Subdir 相对布局（架构文档 §9）。
const (
	SubdirServers  = "servers"
	SubdirAccounts = "accounts"
	SubdirProfiles = "profiles"
	SubdirLogs     = "logs"
	configFileName = "config.json"
)

// Paths 是数据目录布局句柄。
type Paths struct {
	Root string
}

// 数据目录名。产品名统一为 CortexNEL；2184622「全仓改名 Nexus→CortexNEL」漏掉了
// 这里，因此首次运行要把旧的 nexus 目录整体迁移过来（见 DataDir）。
const (
	dataDirName       = "CortexNEL"
	legacyDataDirName = "nexus"
)

// DataDir 返回 %AppData%\CortexNEL。
//
// 首次运行时若发现改名前的 %AppData%\nexus，就地改名迁移过来：旧目录里是账号
// （token 为 DPAPI 加密，同用户可解）与服务器档案，丢了不可恢复；
// 实测该目录体积很大（几乎全是已停用的基岩端本体），所以必须 rename 而非拷贝。
// 迁移失败时退回旧目录继续用，不让应用起不来，下次启动再试。
func DataDir() (string, error) {
	base, err := appDataDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(base, dataDirName)
	legacy := filepath.Join(base, legacyDataDirName)
	if !Exists(target) && Exists(legacy) {
		if err := os.Rename(legacy, target); err != nil {
			return legacy, nil
		}
	}
	return target, nil
}

// NewPaths 在 root 下建目录布局。
func NewPaths(root string) (*Paths, error) {
	p := &Paths{Root: root}
	for _, sub := range []string{SubdirServers, SubdirAccounts, SubdirProfiles, SubdirLogs} {
		if err := mkdirAll(filepath.Join(root, sub)); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// ConfigFile 是全局配置路径。
func (p *Paths) ConfigFile() string { return filepath.Join(p.Root, configFileName) }

// ServerFile 是服务器档案路径。
func (p *Paths) ServerFile(id string) string { return filepath.Join(p.Root, SubdirServers, id+".json") }

// AccountFile 是账号元数据路径（token 不在此文件，见 credential.go）。
func (p *Paths) AccountFile(id string) string {
	return filepath.Join(p.Root, SubdirAccounts, id+".json")
}

// ProfileFile 是环境档案路径。
func (p *Paths) ProfileFile(id string) string {
	return filepath.Join(p.Root, SubdirProfiles, id+".json")
}

// ListIDs 列出某子目录下的全部档案 ID（去 .json 后缀）。
func (p *Paths) ListIDs(subdir string) ([]string, error) {
	return listJSONIDs(filepath.Join(p.Root, subdir))
}

// appDataDir 返回 %AppData% 根（Windows 一等目标，架构文档 §2）。
// 目录名由 DataDir 决定（含旧目录迁移），这里只管取根。
func appDataDir() (string, error) {
	base := os.Getenv("AppData")
	if base == "" {
		return "", errors.New("store: 未找到 %AppData% 目录")
	}
	return base, nil
}

func mkdirAll(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return nil
}

// listJSONIDs 枚举目录下的 .json 文件名（去后缀），按名排序保证确定性。
func listJSONIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}
	sort.Strings(ids)
	return ids, nil
}
