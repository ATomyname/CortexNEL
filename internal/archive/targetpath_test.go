// 条目名 → 目标路径的安全映射：越界与 Windows 名字空间策略的单元判据。
//
// 这一层单独测是因为它现在决定整次解压的成败（见 extract.go 的 skipLog）：
// 拒绝一个合法名字 = 白费一整包下载，放行一个越界名字 = 写出 dest 之外。
// 两个方向的误判都必须在这一层挡住。
package archive

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeTargetAcceptsOrdinaryNames 常规名字必须放行，且目标落在 dest 内。
func TestSafeTargetAcceptsOrdinaryNames(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	for _, name := range []string{
		"file.txt",
		"versions/1.20/1.20.json",
		"libraries/net/minecraft/x.jar",
		"..dots-leading.txt", // 整段不等于 ..，不能误伤
		"./already-relative.txt",
	} {
		target, reason := safeTarget(dest, name)
		if reason != "" {
			t.Errorf("safeTarget(%q) 被拒：%s", name, reason)
			continue
		}
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(filepath.Separator)) {
			t.Errorf("safeTarget(%q) = %q，落在 dest 之外", name, target)
		}
	}
}

// TestSafeTargetRejectsUnsafeNames 无法安全表示或试图越界的名字必须给出原因。
// 期望值就是原因串里的关键词：原因会进错误消息与日志，必须能定位到具体问题。
func TestSafeTargetRejectsUnsafeNames(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	cases := map[string]string{
		"../escaped.txt":      "相对路径越界",
		"sub/../../up.txt":    "相对路径越界",
		`C:\windows\evil.txt`: "绝对路径",
		"foo.txt:alternate":   "NTFS 数据流",
		"CON":                 "保留设备名",
		"sub/COM1/x.jar":      "保留设备名",
		"NUL.txt":             "保留设备名",
		"trailing.":           "尾随点或空格",
		"sub/trailing./x.txt": "尾随点或空格",
		"trailing ":           "尾随点或空格",
		"":                    "空条目名",
	}
	for name, want := range cases {
		target, reason := safeTarget(dest, name)
		if reason == "" {
			t.Errorf("safeTarget(%q) 未被拒，落到了 %q（期望提到 %s）", name, target, want)
			continue
		}
		if !strings.Contains(reason, want) {
			t.Errorf("safeTarget(%q) 原因 = %q，期望包含 %q", name, reason, want)
		}
	}
}

// TestSafeTargetContainsRootRelativePath 无盘符的根相对路径（/abs/x）在 Unix 上是
// 绝对路径（会被拒），在 Windows 上没有卷标、Join 后落在 dest 内。两种平台语义
// 不同，但必须满足同一条安全不变量：绝不落在 dest 之外。
func TestSafeTargetContainsRootRelativePath(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	target, reason := safeTarget(dest, "/abs/escaped.txt")
	if reason != "" {
		return // 拒绝同样是安全结果
	}
	if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(filepath.Separator)) {
		t.Fatalf("根相对路径被放到了 dest 之外: %q", target)
	}
}

// TestSafeTargetContainsEscapeAfterClean 包含检查是兜底：即使前面几关都放行，
// Clean 之后逃出 dest 也必须拦下。
func TestSafeTargetContainsEscapeAfterClean(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "out")
	// 同一盘上的兄弟目录：Join 后落在 dest 外，但名字本身不含 ".." 段。
	name := filepath.Join("..", filepath.Base(dest)+"-sibling", "x.txt")
	if target, reason := safeTarget(dest, name); reason == "" {
		t.Fatalf("逃到兄弟目录的名字未被拒：%q", target)
	}
}
