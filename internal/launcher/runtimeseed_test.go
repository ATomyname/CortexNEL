package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedRuntimeConfigCopiesWhenAbsent 目标缺失时补齐配置。
func TestSeedRuntimeConfigCopiesWhenAbsent(t *testing.T) {
	baseGameDir := t.TempDir()
	targetGameDir := t.TempDir()
	mustWriteNested(t, filepath.Join(baseGameDir, "config", "forge-client.toml"), "base-config")
	mustWriteNested(t, filepath.Join(baseGameDir, "defaultconfigs", "defaults.toml"), "base-defaults")
	mustWrite(t, filepath.Join(baseGameDir, "options.txt"), "guiScale:3\n")

	SeedRuntimeConfig(baseGameDir, targetGameDir)

	for _, rel := range []string{
		filepath.Join("config", "forge-client.toml"),
		filepath.Join("defaultconfigs", "defaults.toml"),
		"options.txt",
	} {
		if !isExistingFile(filepath.Join(targetGameDir, rel)) {
			t.Fatalf("%s 应被播种", rel)
		}
	}
}

// TestSeedRuntimeConfigKeepsExisting 目标已有配置时不得覆盖：
// 那是客户端在沙盒里自行改写的运行时状态（键位、画质、mod 配置）。
func TestSeedRuntimeConfigKeepsExisting(t *testing.T) {
	baseGameDir := t.TempDir()
	targetGameDir := t.TempDir()
	mustWrite(t, filepath.Join(baseGameDir, "options.txt"), "guiScale:3\n")
	mustWriteNested(t, filepath.Join(baseGameDir, "config", "forge-client.toml"), "base-config")
	mustWrite(t, filepath.Join(targetGameDir, "options.txt"), "guiScale:1\n")
	mustWriteNested(t, filepath.Join(targetGameDir, "config", "forge-client.toml"), "player-config")

	SeedRuntimeConfig(baseGameDir, targetGameDir)

	if got := readText(t, filepath.Join(targetGameDir, "options.txt")); got != "guiScale:1\n" {
		t.Fatalf("options.txt 被覆盖：%q", got)
	}
	if got := readText(t, filepath.Join(targetGameDir, "config", "forge-client.toml")); got != "player-config" {
		t.Fatalf("config 被覆盖：%q", got)
	}
}

// TestSeedRuntimeConfigEmptyArgsAreSafe 空路径不得 panic。
func TestSeedRuntimeConfigEmptyArgsAreSafe(t *testing.T) {
	SeedRuntimeConfig("", t.TempDir())
	SeedRuntimeConfig(t.TempDir(), "")
}

// mustWriteNested 与 mustWrite 相同，但会先建好父目录（配置项位于子目录里）。
func mustWriteNested(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, body)
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
