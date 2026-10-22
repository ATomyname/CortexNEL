package store

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDataDirMigratesLegacyDir 改名前的 %AppData%\nexus 必须整体迁移到 CortexNEL ——
// 里面是账号（DPAPI 加密）与服务器档案，丢了不可恢复。
func TestDataDirMigratesLegacyDir(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	legacy := filepath.Join(appData, legacyDataDirName)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(appData, dataDirName)
	if got != want {
		t.Fatalf("数据目录 = %q，期望 %q", got, want)
	}
	if !Exists(filepath.Join(want, "config.json")) {
		t.Fatal("旧目录内容未迁移")
	}
	if Exists(legacy) {
		t.Fatal("迁移后旧目录应已不存在（rename 而非拷贝）")
	}
}

// TestDataDirKeepsExistingTarget 新目录已存在时不覆盖，也不动旧目录
// （避免把用户已经用起来的数据换掉）。
func TestDataDirKeepsExistingTarget(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	target := filepath.Join(appData, dataDirName)
	legacy := filepath.Join(appData, legacyDataDirName)
	for _, dir := range []string{target, legacy} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "config.json"), []byte(`{"new":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != target {
		t.Fatalf("数据目录 = %q，期望新目录 %q", got, target)
	}
	data, err := os.ReadFile(filepath.Join(target, "config.json"))
	if err != nil || string(data) != `{"new":true}` {
		t.Fatalf("新目录内容被改动：%q err=%v", data, err)
	}
	if !Exists(legacy) {
		t.Fatal("旧目录不应被删除（留给用户自行清理）")
	}
}

// TestDataDirFreshInstall 全新机器（两个目录都不存在）直接用新目录名。
func TestDataDirFreshInstall(t *testing.T) {
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(appData, dataDirName) {
		t.Fatalf("数据目录 = %q，期望 %q", got, filepath.Join(appData, dataDirName))
	}
}

// TestDataDirWithoutAppData 无 %AppData% 时给出可读错误。
func TestDataDirWithoutAppData(t *testing.T) {
	t.Setenv("AppData", "")
	if _, err := DataDir(); err == nil {
		t.Fatal("缺 %AppData% 应报错")
	}
}

func TestPathsLayout(t *testing.T) {
	p, err := NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ids, err := p.ListIDs(SubdirServers)
	if err != nil || len(ids) != 0 {
		t.Fatalf("空目录 ids=%v err=%v", ids, err)
	}
	type empty struct{}
	if err := Save(p.ServerFile("b"), empty{}); err != nil {
		t.Fatal(err)
	}
	if err := Save(p.ServerFile("a"), empty{}); err != nil {
		t.Fatal(err)
	}
	ids, err = p.ListIDs(SubdirServers)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("ids 排序错误: %v", ids)
	}
}
