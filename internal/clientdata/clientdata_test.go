package clientdata

import (
	"os"
	"path/filepath"
	"testing"

	"nexus/internal/store"
)

// TestResolveRootCreatesUnderDriveRoot 选址结果必须是 <盘根>\CortexNEL\client 且被真实创建。
func TestResolveRootCreatesUnderDriveRoot(t *testing.T) {
	fakeDrive := t.TempDir() // 用临时目录冒充盘根
	dir, err := resolveRoot(func() (string, bool) { return fakeDrive, true })
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeDrive, DirName, ClientSubdir)
	if dir != want {
		t.Fatalf("目录 = %q，期望 %q", dir, want)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("目录未被创建：%v", err)
	}
}

// TestResolveRootWithoutDrive 没有任何可用盘时给出可读错误，而不是空路径。
func TestResolveRootWithoutDrive(t *testing.T) {
	if _, err := resolveRoot(func() (string, bool) { return "", false }); err == nil {
		t.Fatal("无可用盘时应报错")
	}
}

// TestRootReusesConfiguredDir 已配置且可用的目录必须沿用（否则已下载的本体会作废）。
func TestRootReusesConfiguredDir(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configured := t.TempDir()
	settings := &store.Settings{ClientDataDir: configured}

	got, err := Root(paths, settings)
	if err != nil {
		t.Fatal(err)
	}
	if got != configured {
		t.Fatalf("目录 = %q，期望沿用 %q", got, configured)
	}
}

// TestRootPersistsPickedDir 首次选址必须写回 config.json，
// 否则每次启动都会重新选址、已下载的客户端本体将不可见。
// 用临时目录冒充盘根，避免测试在真实磁盘上建目录。
func TestRootPersistsPickedDir(t *testing.T) {
	paths, err := store.NewPaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fakeDrive := t.TempDir()
	settings := &store.Settings{}

	got, err := rootWith(paths, settings, func() (string, bool) { return fakeDrive, true })
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(fakeDrive, DirName, ClientSubdir)
	if got != want {
		t.Fatalf("目录 = %q，期望 %q", got, want)
	}
	loaded, err := store.LoadSettings(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientDataDir != want {
		t.Fatalf("config.json 中的目录 = %q，期望 %q", loaded.ClientDataDir, want)
	}
}

// TestVersionDir 版本目录位于版本数据目录下。
func TestVersionDir(t *testing.T) {
	got := VersionDir(`D:\CortexNEL\client`, "1.20")
	want := filepath.Join(`D:\CortexNEL\client`, "versions", "1.20")
	if got != want {
		t.Fatalf("版本目录 = %q，期望 %q", got, want)
	}
}
