package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNeteaseJavaGlobsForMajorHitsOfficialLayout 用真实形状的目录树验证网易自带 JRE 能被命中。
//
// 实测布局（官方启动器 dump 的 java_path）：
//
//	D:\MCLDownload\ext\jre-v64-220420\jdk17\bin\javaw.exe   ← 1.20
//	D:\MCLDownload\ext\jre-v64-220420\jdk22\bin\javaw.exe   ← 1.21.8
//
// 这里在临时根下复刻同样的层级，确认每个大版本只命中自己那一份 ——
// 只断言模式字符串包含某片段是不够的，那测不出层级写错。
func TestNeteaseJavaGlobsForMajorHitsOfficialLayout(t *testing.T) {
	root := t.TempDir()
	for _, major := range []string{"17", "22"} {
		writeJavaStub(t, root, "jdk"+major)
	}

	cases := map[string]string{"17": "jdk17", "22": "jdk22"}
	for major, dir := range cases {
		want := filepath.Join(root, "MCLDownload", "ext", "jre-v64-220420", dir, "bin", "javaw.exe")
		got, ok := firstMatchingFile(neteaseJavaGlobsForMajor(root, major))
		if !ok {
			t.Fatalf("major=%s 未命中：模式 %q", major, neteaseJavaGlobsForMajor(root, major))
		}
		if got != want {
			t.Fatalf("major=%s 命中 %q，期望 %q", major, got, want)
		}
	}
}

// TestPreferredJavaGlobsPreferStandard21ThenNetease22 版本档位顺序：
// 标准 JDK 21 优先，其次才是网易自带的 jdk22（该 ext 下没有 jdk21）。
func TestPreferredJavaGlobsPreferStandard21ThenNetease22(t *testing.T) {
	globs := preferredJavaGlobs("1.21.8")

	idx21, idx22 := -1, -1
	for i, g := range globs {
		if idx21 < 0 && strings.Contains(g, "jdk21") {
			idx21 = i
		}
		if idx22 < 0 && strings.Contains(g, "jdk22") {
			idx22 = i
		}
	}
	if idx21 < 0 {
		t.Fatalf("1.20.5+ 档位应含 Java 21 候选：%q", globs)
	}
	if idx22 < 0 {
		t.Fatalf("1.20.5+ 档位应含 Java 22 候选（网易自带 jdk22）：%q", globs)
	}
	if idx21 > idx22 {
		t.Fatalf("Java 21 候选应排在 Java 22 之前：21@%d 22@%d", idx21, idx22)
	}
}

// TestNeteaseJavaGlobsForMajorRespectsMajorVersion 版本不匹配时不得命中，
// 否则 1.20 会被塞一个 Java 8/22 运行时。
func TestNeteaseJavaGlobsForMajorRespectsMajorVersion(t *testing.T) {
	root := t.TempDir()
	writeJavaStub(t, root, "jdk17")

	if got, ok := firstMatchingFile(neteaseJavaGlobsForMajor(root, "21")); ok {
		t.Fatalf("jdk17 不应被当作 Java 21 命中，实得 %q", got)
	}
}

// TestNeteaseJavaGlobsHitsOfficialLayout 版本无关的兜底探测同样要命中该布局。
func TestNeteaseJavaGlobsHitsOfficialLayout(t *testing.T) {
	root := t.TempDir()
	javaExe := writeJavaStub(t, root, "jdk17")

	if got, ok := firstMatchingFile(neteaseJavaGlobs(root)); !ok || got != javaExe {
		t.Fatalf("兜底探测未命中 %q：got=%q ok=%v", javaExe, got, ok)
	}
}

// writeJavaStub 在复刻的网易布局下创建 javaw.exe 桩文件，返回其路径。
func writeJavaStub(t *testing.T, root, javaDir string) string {
	t.Helper()
	javaExe := filepath.Join(root, "MCLDownload", "ext", "jre-v64-220420", javaDir, "bin", "javaw.exe")
	if err := os.MkdirAll(filepath.Dir(javaExe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(javaExe, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	return javaExe
}

// TestCheckJavaMajorBlocksUnrunnableJava 跑不起来的 Java 必须报错而不是放行。
//
// 换机后 profile 里持久化的 JavaPath 指向不存在的 javaw —— fail-open 会把
// 它判成「校验通过」，ensureMirrorJava 的 CDN 部署分支永远不触发，故障推迟到
// 启动时以 "A Java Exception has occurred" 爆发（真实踩过）。拦截后调用方
// 才能走自愈路径。
func TestCheckJavaMajorBlocksUnrunnableJava(t *testing.T) {
	// 文件不存在。
	err := CheckJavaMajor(filepath.Join(t.TempDir(), "nope", "javaw.exe"), "1.8.9")
	if err == nil {
		t.Fatal("不存在的 Java 必须报错（放行会堵死 CDN 部署自愈）")
	}
	// 文件存在但无法执行（不是可执行文件）。
	bogus := filepath.Join(t.TempDir(), "javaw.exe")
	if err := os.WriteFile(bogus, []byte("not an executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckJavaMajor(bogus, "1.8.9"); err == nil {
		t.Fatal("无法执行的 Java 必须报错")
	}
}

// TestJavaProbeErrorOnlyForExecFailures 探测错误只覆盖「跑不起来」：
// 能跑但版本串解析不出（parse 层）不许归为探测失败 —— 那类要保守放行。
func TestJavaProbeErrorOnlyForExecFailures(t *testing.T) {
	_, err := JavaMajor(filepath.Join(t.TempDir(), "missing", "javaw.exe"))
	if err == nil {
		t.Fatal("缺失的 Java 应返回错误")
	}
	var probeErr javaProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("缺失/无法执行应包成 javaProbeError，实得 %T: %v", err, err)
	}

	_, parseErr := parseJavaMajor("garbage output")
	if parseErr == nil {
		t.Fatal("垃圾输出应报错")
	}
	if errors.As(parseErr, &probeErr) {
		t.Fatalf("解析失败不是探测失败，不应包成 javaProbeError: %v", parseErr)
	}
}

// TestParseJavaMajor `java -version` 输出的大版本解析：
// 新命名（17.0.2 → 17）与旧命名（1.8.0_402 → 8）都要支持。
func TestParseJavaMajor(t *testing.T) {
	cases := []struct {
		output string
		want   int
	}{
		{`openjdk version "17.0.2" 2022-01-18`, 17},
		{`openjdk version "22" 2024-03-19`, 22},
		{`java version "1.8.0_402"`, 8},
		{`openjdk version "21.0.3" 2024-04-16`, 21},
	}
	for _, c := range cases {
		got, err := parseJavaMajor(c.output)
		if err != nil {
			t.Fatalf("parseJavaMajor(%q): %v", c.output, err)
		}
		if got != c.want {
			t.Fatalf("parseJavaMajor(%q) = %d，期望 %d", c.output, got, c.want)
		}
	}
	if _, err := parseJavaMajor("garbage output"); err == nil {
		t.Fatal("无版本串的输出应报错")
	}
}

// TestRequiredJavaMajors 版本档位与 preferredJavaGlobs 保持一致：
// ≤1.16 → 8；1.17~1.20.4 → 17；1.20.5+ → 21/22。
func TestRequiredJavaMajors(t *testing.T) {
	cases := []struct {
		version string
		want    string
	}{
		{"1.12.2", "8"},
		{"1.8.9", "8"},
		{"1.16.5", "8"},
		{"1.17.1", "17"},
		{"1.20", "17"},
		{"1.20.1", "17"},
		{"1.20.5", "21/22"},
		{"1.21.8", "21/22"},
	}
	for _, c := range cases {
		parts := make([]string, 0, 2)
		for _, v := range RequiredJavaMajors(c.version) {
			parts = append(parts, strconv.Itoa(v))
		}
		if got := strings.Join(parts, "/"); got != c.want {
			t.Fatalf("requiredJavaMajors(%q) = %s，期望 %s", c.version, got, c.want)
		}
	}
}
