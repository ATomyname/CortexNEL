package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestIsClientInstall 客户端根的判据是「有 versions 与 libraries」，不是目录名。
func TestIsClientInstall(t *testing.T) {
	root := t.TempDir()
	if isClientInstall(root) {
		t.Fatal("空目录不应判为客户端根")
	}
	mustMkdirAll(t, filepath.Join(root, "versions"))
	if isClientInstall(root) {
		t.Fatal("只有 versions 不应判为客户端根")
	}
	mustMkdirAll(t, filepath.Join(root, "libraries"))
	if !isClientInstall(root) {
		t.Fatal("versions+libraries 应判为客户端根")
	}
}

// TestSelectClientRootByModsPicksMatchingLine 组件重合度要能区分产品线。
// 实测形态：Java 线根为 @2@8 资产组件，EC 线根为 ecguard / bedrockloader 等具名组件。
func TestSelectClientRootByModsPicksMatchingLine(t *testing.T) {
	javaRoot := t.TempDir()
	ecRoot := t.TempDir()
	writeMods(t, javaRoot, "4690287328189015853@2@8.jar", "4687456828685462969@2@8.jar")
	writeMods(t, ecRoot, "ecguard-0.1.0.jar", "bedrockloader-1.21.8-1.0.0-obf.jar", "4687456828685462969@3@0.jar")
	cacheMods := writeMods(t, t.TempDir(), "ecguard-0.1.0.jar", "bedrockloader-1.21.8-1.0.0-obf.jar")

	got := selectClientRootByMods([]string{javaRoot, ecRoot}, cacheMods)
	if got != ecRoot {
		t.Fatalf("选中 %q，期望 EC 根 %q", got, ecRoot)
	}
}

// TestSelectClientRootByModsIgnoresDisabledJars 停用的组件不参与匹配。
func TestSelectClientRootByModsIgnoresDisabledJars(t *testing.T) {
	root := t.TempDir()
	writeMods(t, root, "ecguard-0.1.0.jar.disabled")
	cacheMods := writeMods(t, t.TempDir(), "ecguard-0.1.0.jar")

	if got := selectClientRootByMods([]string{root}, cacheMods); got != "" {
		t.Fatalf(".disabled 不应算作重合，实得 %q", got)
	}
}

// TestSelectClientRootByModsWithoutEvidenceKeepsDefault 无证据返回空串，调用方保留默认根。
func TestSelectClientRootByModsWithoutEvidenceKeepsDefault(t *testing.T) {
	javaRoot := t.TempDir()
	writeMods(t, javaRoot, "4690287328189015853@2@8.jar")
	cacheMods := writeMods(t, t.TempDir(), "ecguard-0.1.0.jar")

	if got := selectClientRootByMods([]string{javaRoot}, cacheMods); got != "" {
		t.Fatalf("无重合应返回空串，实得 %q", got)
	}
	if got := selectClientRootByMods([]string{javaRoot}, filepath.Join(t.TempDir(), "不存在")); got != "" {
		t.Fatalf("组件缓存缺失应返回空串，实得 %q", got)
	}
}

// TestSelectClientRootByModsTieKeepsCandidateOrder 平局取候选顺序靠前者（默认根排在前）。
func TestSelectClientRootByModsTieKeepsCandidateOrder(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeMods(t, first, "a.jar")
	writeMods(t, second, "a.jar")
	cacheMods := writeMods(t, t.TempDir(), "a.jar")

	if got := selectClientRootByMods([]string{first, second}, cacheMods); got != first {
		t.Fatalf("平局应取首项 %q，实得 %q", first, got)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeMods 在 root/mods 下创建若干 jar 桩文件，返回该 mods 目录
// （selectClientRootByMods 的第二个参数就是 mods 目录，与 findServerModsSource 的返回一致）。
func writeMods(t *testing.T, root string, names ...string) string {
	t.Helper()
	modsDir := filepath.Join(root, "mods")
	mustMkdirAll(t, modsDir)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(modsDir, name), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return modsDir
}
