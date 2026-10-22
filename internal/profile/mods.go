// mods 快照与一致性比对（架构文档 §4）：会话启动前比对，会话中不写镜像目录（P6）。
// 服务端校验的是 iid+md5 集合（facts §3.7）。
package profile

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nexus/internal/netease"
)

// ErrNoModsDir 表示镜像目录不存在组件目录。
var ErrNoModsDir = errors.New("profile: 组件目录不存在")

// ScanMods 扫描组件目录，产出 modInfo 条目（md5 大写，facts §3.7）。
func ScanMods(modsDir string) ([]netease.Mod, error) {
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoModsDir
		}
		return nil, err
	}
	var mods []netease.Mod
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jar") {
			continue
		}
		mod, err := netease.ParseJarName(e.Name())
		if err != nil {
			continue // 非组件 jar（第三方普通 mod）不进 modInfo。
		}
		sum, err := fileMD5(filepath.Join(modsDir, e.Name()))
		if err != nil {
			return nil, err
		}
		mod.MD5 = sum
		mods = append(mods, mod)
	}
	return mods, nil
}

// SyncResult 是 mods 比对结果（EVENTS.md §3 profile.mods.sync 字段来源）。
type SyncResult struct {
	Added   []string
	Removed []string
	Changed []string
}

// CompareMods 按 (iid, md5) 集合比对：期望（服务器要求）vs 实际（本地扫描）。
// added = 期望有而本地缺；removed = 本地多出；changed = iid 同但 md5 不同。
func CompareMods(expected, actual []netease.Mod) SyncResult {
	exp := indexByKey(expected)
	act := indexByKey(actual)
	var r SyncResult
	for iid, want := range exp {
		got, ok := act[iid]
		if !ok {
			r.Added = append(r.Added, iid)
			continue
		}
		if want != got {
			r.Changed = append(r.Changed, iid)
		}
	}
	for iid := range act {
		if _, ok := exp[iid]; !ok {
			r.Removed = append(r.Removed, iid)
		}
	}
	return r
}

func indexByKey(mods []netease.Mod) map[string]string {
	out := make(map[string]string, len(mods))
	for _, m := range mods {
		out[m.IID] = m.MD5
	}
	return out
}

func fileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))), nil
}
