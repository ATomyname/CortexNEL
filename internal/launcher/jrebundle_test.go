package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureJreRealDownload 真实部署验证：从官方 CDN 下载 jdk17 分发包并解压。
// 197MB 级下载，仅在显式设置 JOINPROBE_JRE=1 时运行（避免常规测试触发大流量）。
func TestEnsureJreRealDownload(t *testing.T) {
	if os.Getenv("JOINPROBE_JRE") != "1" {
		t.Skip("需要 JOINPROBE_JRE=1（真实 197MB 下载）")
	}
	dest := filepath.Join(os.TempDir(), "joinprobe-jre")
	ctx := context.Background()
	path, err := EnsureJre(ctx, []int{17}, dest, func(stage, message string, done, total int64) {
		t.Logf("[%s] %s (%d/%d)", stage, message, done, total)
	})
	if err != nil {
		t.Fatalf("部署失败: %v", err)
	}
	if major, err := JavaMajor(path); err != nil || major != 17 {
		t.Fatalf("部署出的 javaw 大版本 = %d（err=%v），期望 17", major, err)
	}
	// 幂等：再次调用应直接命中已部署。
	path2, err := EnsureJre(ctx, []int{17}, dest, nil)
	if err != nil {
		t.Fatalf("幂等调用失败: %v", err)
	}
	if path2 != path {
		t.Fatalf("幂等调用返回不同路径: %q vs %q", path2, path)
	}
}

// TestEnsureJreRealDownloadJava8 真实部署验证 Java 8 档位（MC ≤1.16 的来源）。
// 41MB 级下载，与 jdk17 同样门控在 JOINPROBE_JRE=1 下。
func TestEnsureJreRealDownloadJava8(t *testing.T) {
	if os.Getenv("JOINPROBE_JRE") != "1" {
		t.Skip("需要 JOINPROBE_JRE=1（真实 41MB 下载）")
	}
	dest := filepath.Join(os.TempDir(), "joinprobe-jre8")
	ctx := context.Background()
	path, err := EnsureJre(ctx, []int{8}, dest, func(stage, message string, done, total int64) {
		t.Logf("[%s] %s (%d/%d)", stage, message, done, total)
	})
	if err != nil {
		t.Fatalf("Java 8 部署失败: %v", err)
	}
	if major, err := JavaMajor(path); err != nil || major != 8 {
		t.Fatalf("部署出的 javaw 大版本 = %d（err=%v），期望 8", major, err)
	}
}

// writeJreDeployment 在 destDir 造一个「已部署」的 JRE 形态：javaw 桩 + 内容为
// 分发包 MD5 的安装标记。判据与 deployedJavaw 严格同构。
func writeJreDeployment(t *testing.T, destDir string, b jreBundle, withMarker bool) string {
	t.Helper()
	stem := strings.TrimSuffix(filepath.Base(b.URL), ".7z")
	javaw := filepath.Join(destDir, stem, b.TopDir, "bin", "javaw.exe")
	if err := os.MkdirAll(filepath.Dir(javaw), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(javaw, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withMarker {
		marker := filepath.Join(destDir, stem+".md5")
		if err := os.WriteFile(marker, []byte(b.MD5), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return javaw
}

// TestEnsureJreFindsDeployedJava8 已部署且标记命中的 Java 8 必须被直接命中，不再报
// "CDN 上没有匹配 Java 8 的已实证分发包"。
//
// 这是一次真实故障的护栏（2026-09-21）：登记表里长期只有 jdk17，MC 1.8.9
// 这类 ≤1.16 档位要 Java 8，机器上只有 Java 21，于是一键进服无路可走。
// jdk8_v64_v1.7z 的地址与 jre8/ 顶层目录已实证（HEAD 200 / 41,818,948 B /
// 7z magic / 解压后 javaw -version = 1.8.0_60）。
//
// 走"已部署"分支（EnsureJre 的第①步），不触发网络下载。
func TestEnsureJreFindsDeployedJava8(t *testing.T) {
	dest := t.TempDir()
	// deployedJavaw 的候选路径：<dest>/<包名 stem>/<TopDir>/bin/javaw.exe。
	var bundle jreBundle
	for _, b := range knownJREBundles {
		if b.Major == 8 {
			bundle = b
			break
		}
	}
	if bundle.URL == "" {
		t.Fatal("登记表缺少 Java 8 分发包（MC ≤1.16 档位的来源）")
	}
	javaw := writeJreDeployment(t, dest, bundle, true)

	got, err := EnsureJre(context.Background(), []int{8}, dest, nil)
	if err != nil {
		t.Fatalf("已部署的 Java 8 应直接命中: %v", err)
	}
	if got != javaw {
		t.Fatalf("javaw 路径 = %q，期望 %q", got, javaw)
	}
}

// TestFindDeployedJavawHitsWithoutDownload 只查盘、不下载：已部署且标记命中的
// JRE 必须被找到；**javaw 存在但没有标记（或标记不符）必须判未部署** ——
// 这是"半个解压树被当成可用运行时、从此永远不再重下"这一真实故障的护栏。
func TestFindDeployedJavawHitsWithoutDownload(t *testing.T) {
	dest := t.TempDir()
	// 未部署时应返回空（且绝不能触发下载）。
	if got := FindDeployedJavaw([]int{8}, dest); got != "" {
		t.Fatalf("未部署时应返回空串，实得 %q", got)
	}

	var bundle jreBundle
	for _, b := range knownJREBundles {
		if b.Major == 8 {
			bundle = b
			break
		}
	}
	if bundle.URL == "" {
		t.Fatal("登记表缺少 Java 8 分发包")
	}

	// javaw 存在但没有标记：半个树不是已部署，绝不能命中。
	javaw := writeJreDeployment(t, dest, bundle, false)
	if got := FindDeployedJavaw([]int{8}, dest); got != "" {
		t.Fatalf("javaw 存在但无标记时应判未部署，实得 %q", got)
	}

	// 补上标记后命中，路径与 EnsureJre 的解压落点同构。
	marker := filepath.Join(dest, strings.TrimSuffix(filepath.Base(bundle.URL), ".7z")+".md5")
	if err := os.WriteFile(marker, []byte(bundle.MD5), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindDeployedJavaw([]int{8}, dest); got != javaw {
		t.Fatalf("已部署的 Java 8 应命中 %q，实得 %q", javaw, got)
	}
	// 大版本不命中时不得返回（17 的包不能冒充 8）。
	if got := FindDeployedJavaw([]int{17}, dest); got != "" {
		t.Fatalf("大版本不匹配时应返回空串，实得 %q", got)
	}

	// 标记内容与登记 MD5 不符（包更新后旧标记失效）不得命中。
	if err := os.WriteFile(marker, []byte("deadbeef"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := FindDeployedJavaw([]int{8}, dest); got != "" {
		t.Fatalf("标记不符时应判未部署，实得 %q", got)
	}
}

// TestKnownJREBundlesCoverRequiredMajors 每个 MC 档位至少要有一个可用分发包。
//
// RequiredJavaMajors 返回的是候选集（如 1.20.5+ → {21,22}，任一满足即可），
// 所以判据是"候选与登记表有交集"，不是"候选全部登记"（jdk21 官方无包，
// 由 jdk22 覆盖，见 jrebundle.go 包注释）。
func TestKnownJREBundlesCoverRequiredMajors(t *testing.T) {
	registered := map[int]bool{}
	for _, b := range knownJREBundles {
		if b.URL == "" || b.Major <= 0 || b.TopDir == "" {
			t.Fatalf("登记表条目不完整: %+v", b)
		}
		if len(b.MD5) != 32 {
			t.Fatalf("登记表 %s 缺少 32 位 MD5 指纹: %q", b.URL, b.MD5)
		}
		registered[b.Major] = true
	}
	// 覆盖各档位的代表版本（RequiredJavaMajors 的三条分支）。
	for _, mc := range []string{"1.8.9", "1.12.2", "1.16.5", "1.17.1", "1.20.1", "1.20.6", "1.21.8"} {
		majors := RequiredJavaMajors(mc)
		if len(majors) == 0 {
			t.Fatalf("MC %s 没有给出任何 Java 候选", mc)
		}
		covered := false
		for _, major := range majors {
			if registered[major] {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("MC %s 需要 Java %v，登记表一个都没有", mc, majors)
		}
	}
}
