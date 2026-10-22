package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSeedBaseModsCarriesCoreComponents 核心组件必须进沙盒。
// 这是旧规则漏掉的那一环：核心组件承载网易 RPC/网络控制客户端，
// 缺了它客户端不回连控制通道，进服不触发。
func TestSeedBaseModsCarriesCoreComponents(t *testing.T) {
	baseGameDir := t.TempDir()
	targetGameDir := t.TempDir()
	mustWriteMod(t, baseGameDir, "4690372021023046883@3@0.jar", "core-mod")
	mustWriteMod(t, baseGameDir, "CustomSkinLoader.jar", "third-party")

	if sent := SeedBaseMods(baseGameDir, targetGameDir); sent != 2 {
		t.Fatalf("已就位组件数 = %d，期望 2", sent)
	}
	for _, name := range []string{"4690372021023046883@3@0.jar", "CustomSkinLoader.jar"} {
		if !isExistingFile(filepath.Join(targetGameDir, "mods", name)) {
			t.Fatalf("组件 %s 未进入目标 mods", name)
		}
	}
}

// TestSeedBaseModsSkipsDisabledAndNonJar 用户主动停用的组件与非 jar 文件不复制。
// 用户意图必须延续到沙盒，否则界面上的插件开关形同虚设。
func TestSeedBaseModsSkipsDisabledAndNonJar(t *testing.T) {
	baseGameDir := t.TempDir()
	targetGameDir := t.TempDir()
	mustWriteMod(t, baseGameDir, "SomeMod.jar.disabled", "disabled-by-user")
	mustWriteMod(t, baseGameDir, "README.txt", "not-a-mod")

	if sent := SeedBaseMods(baseGameDir, targetGameDir); sent != 0 {
		t.Fatalf("已就位组件数 = %d，期望 0", sent)
	}
	if isExistingFile(filepath.Join(targetGameDir, "mods", "SomeMod.jar.disabled")) {
		t.Fatal(".disabled 组件不应被复制")
	}
	if isExistingFile(filepath.Join(targetGameDir, "mods", "README.txt")) {
		t.Fatal("非 jar 文件不应被复制")
	}
}

// TestSeedBaseModsRebuildsOnSizeChange 大小不同的同名组件会被覆盖：
// 服务端校验的是 iid+md5 集合，基础组件必须与服务端预期一致。
func TestSeedBaseModsRebuildsOnSizeChange(t *testing.T) {
	baseGameDir := t.TempDir()
	targetGameDir := t.TempDir()
	mustWriteMod(t, baseGameDir, "Mod.jar", "new-content")
	mustWriteMod(t, targetGameDir, "Mod.jar", "old")

	SeedBaseMods(baseGameDir, targetGameDir)

	data, err := os.ReadFile(filepath.Join(targetGameDir, "mods", "Mod.jar"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new-content" {
		t.Fatalf("组件内容 = %q，期望被覆盖为 %q", data, "new-content")
	}
}

// TestCopyModsSameDirIsNoop 源与目标同一目录时不得自我复制。
func TestCopyModsSameDirIsNoop(t *testing.T) {
	dir := t.TempDir()
	mustWriteMod(t, dir, "Mod.jar", "content")
	if got := copyMods(filepath.Join(dir, "mods"), filepath.Join(dir, "mods")); got != 0 {
		t.Fatalf("已就位组件数 = %d，期望 0", got)
	}
}

// TestSeedBaseModsEmptyArgsAreSafe 空路径不得 panic 或复制到当前目录。
func TestSeedBaseModsEmptyArgsAreSafe(t *testing.T) {
	if got := SeedBaseMods("", t.TempDir()); got != 0 {
		t.Fatalf("空来源应返回 0，得到 %d", got)
	}
	if got := SeedBaseMods(t.TempDir(), ""); got != 0 {
		t.Fatalf("空目标应返回 0，得到 %d", got)
	}
}

func mustWriteMod(t *testing.T, gameDir, name, body string) {
	t.Helper()
	modsDir := filepath.Join(gameDir, "mods")
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(modsDir, name), body)
}
