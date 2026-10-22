package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSdkFixture 在目录里铺一份完整 SDK 运行库包，内容按文件名区分。
func writeSdkFixture(t *testing.T, dir string, names []string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		mustWrite(t, filepath.Join(dir, name), "pack:"+name)
	}
}

// TestSdkRuntimeManifestCoversGrayComponent 清单必须含 gray 组件本体与它的链接依赖：
// 二者缺一，Library 的 native 就绑定不上（实测炸点见 sdkruntime.go 包注释）。
func TestSdkRuntimeManifestCoversGrayComponent(t *testing.T) {
	must := []string{
		"api-ms-win-crt-utility-l1-1-1.dll", // gray 组件本体：GetToken/reviewWord/reviewName/Skip32/preInit
		"libenvsdk.dll",                     // gray 组件的链接依赖
	}
	index := make(map[string]bool, len(sdkRuntimeManifest))
	for _, name := range sdkRuntimeManifest {
		if index[name] {
			t.Fatalf("清单出现重复项 %s", name)
		}
		index[name] = true
	}
	for _, name := range must {
		if !index[name] {
			t.Fatalf("清单缺少 %s，客户端 native 会绑定失败", name)
		}
	}
	// WinFallbackLookup.dll 是 JDK 自带运行库、gray 组件也不引用它：算进清单就会
	// 变成新装机器的补件阻塞项（CDN 同样没有这一项）。
	if index["WinFallbackLookup.dll"] {
		t.Fatal("WinFallbackLookup.dll 是 JDK 自带件，不该进 SDK 包清单")
	}
	if len(sdkRuntimeClientChecked) != 50 {
		t.Fatalf("客户端 checkFile 清单 = %d 项，实测是 50 项", len(sdkRuntimeClientChecked))
	}
	if len(sdkRuntimeManifest) != len(sdkRuntimeClientChecked)+len(sdkRuntimeLoadDeps) {
		t.Fatal("完整清单应为 checkFile 清单 + 加载依赖")
	}
}

// TestSeedSdkRuntimeStagesManifestOnly 落位只搬清单内文件：来源目录里的其他 dll
// 一律不搬运。旧规则把「JRE bin 里非 JDK 自带的 dll」当候选，给 1.8.9 灌了 40 个
// JDK8 自带库却一个清单项都没补上。
func TestSeedSdkRuntimeStagesManifestOnly(t *testing.T) {
	source := t.TempDir()
	writeSdkFixture(t, source, sdkRuntimeManifest)
	mustWrite(t, filepath.Join(source, "java.dll"), "jdk-own")
	mustWrite(t, filepath.Join(source, "javafx_font.dll"), "jdk8-own")
	mustWrite(t, filepath.Join(source, "notes.txt"), "not-a-dll")

	nativesDir := t.TempDir()
	report := SeedSdkRuntime(source, nativesDir)
	if report.Seeded != len(sdkRuntimeManifest) {
		t.Fatalf("落位数 = %d，期望 %d", report.Seeded, len(sdkRuntimeManifest))
	}
	if len(report.Missing) != 0 {
		t.Fatalf("齐全来源不应有缺项，实得 %v", report.Missing)
	}
	if report.SourceDir != source {
		t.Fatalf("SourceDir = %q，期望 %q", report.SourceDir, source)
	}
	for _, name := range []string{"java.dll", "javafx_font.dll", "notes.txt"} {
		if _, err := os.Stat(filepath.Join(nativesDir, "runtime", name)); err == nil {
			t.Fatalf("清单外文件 %s 不应被落位", name)
		}
	}

	// 幂等：内容一致时不再写。
	if again := SeedSdkRuntime(source, nativesDir); again.Seeded != 0 {
		t.Fatalf("重复落位应为 0，实得 %d", again.Seeded)
	}

	// 同大小但内容不同的旧副本必须被替换；只比大小会留下坏缓存。
	name := sdkRuntimeClientChecked[0]
	mustWrite(t, filepath.Join(nativesDir, "runtime", name), "stale")
	mustWrite(t, filepath.Join(source, name), "fresh")
	if got := SeedSdkRuntime(source, nativesDir); got.Seeded != 1 {
		t.Fatalf("陈旧副本应被替换，落位数 = %d", got.Seeded)
	}
	data, err := os.ReadFile(filepath.Join(nativesDir, "runtime", name))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fresh" {
		t.Fatalf("陈旧副本未被替换，内容 = %q", data)
	}
}

// TestSeedSdkRuntimeReportsMissingWithoutBlocking 缺件时尽力补齐、照常启动，
// 但缺项必须落到报告里（规范 I2），否则又会像 1.8.9 那样静默缺 48 项。
func TestSeedSdkRuntimeReportsMissingWithoutBlocking(t *testing.T) {
	source := t.TempDir()
	present := sdkRuntimeManifest[0:2]
	writeSdkFixture(t, source, present)

	nativesDir := t.TempDir()
	report := SeedSdkRuntime(source, nativesDir)
	if report.Seeded != len(present) {
		t.Fatalf("落位数 = %d，期望 %d", report.Seeded, len(present))
	}
	if len(report.Missing) != len(sdkRuntimeManifest)-len(present) {
		t.Fatalf("缺项数 = %d，期望 %d: %v",
			len(report.Missing), len(sdkRuntimeManifest)-len(present), report.Missing)
	}
	for _, name := range present {
		for _, missing := range report.Missing {
			if missing == name {
				t.Fatalf("已落位的 %s 不应报缺", name)
			}
		}
	}

	// 空文件不算落位：System.load 到 0 字节 dll 等于没装。
	empty := sdkRuntimeManifest[3]
	if err := os.MkdirAll(filepath.Join(nativesDir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(nativesDir, "runtime", empty), "")
	after := SeedSdkRuntime(source, nativesDir)
	found := false
	for _, missing := range after.Missing {
		if missing == empty {
			found = true
		}
	}
	if !found {
		t.Fatalf("0 字节 %s 应报缺", empty)
	}
}

// TestSeedSdkRuntimeSafeOnEmptyArgs 空路径与不存在的来源目录不得 panic，且缺项要全报。
func TestSeedSdkRuntimeSafeOnEmptyArgs(t *testing.T) {
	if got := SeedSdkRuntime("", t.TempDir()); len(got.Missing) != len(sdkRuntimeManifest) {
		t.Fatalf("空来源应全报缺，实得 %d 项", len(got.Missing))
	}
	if got := SeedSdkRuntime(t.TempDir(), ""); got.Seeded != 0 || len(got.Missing) != 0 {
		t.Fatalf("空目标应直接返回零值，实得 %+v", got)
	}
	if got := SeedSdkRuntime(filepath.Join(t.TempDir(), "不存在"), t.TempDir()); got.Seeded != 0 {
		t.Fatalf("来源不存在应落位 0，实得 %d", got.Seeded)
	}
}

// TestResolveSdkRuntimeSourcePrefersPackThenSelfHeals 取件来源优先用启动器置备的
// 包目录，其次自愈归集已有版本目录里的齐全副本；都不齐时返回空串而不是随便挑一份。
func TestResolveSdkRuntimeSourcePrefersPackThenSelfHeals(t *testing.T) {
	clientDir := t.TempDir()
	packDir := filepath.Join(clientDir, sdkRuntimePackDirName)
	// natives 目录名随分发包而异，两种形态都要能被发现。
	selfHeal := filepath.Join(clientDir, "versions", "1.20", "natives-windows-x86_64", "runtime")
	partial := filepath.Join(clientDir, "versions", "1.8.9", "natives", "runtime")

	writeSdkFixture(t, selfHeal, sdkRuntimeManifest)
	writeSdkFixture(t, partial, sdkRuntimeManifest[0:1])

	if got := ResolveSdkRuntimeSource(clientDir); got != selfHeal {
		t.Fatalf("无包目录时应自愈到齐全副本，实得 %q", got)
	}

	writeSdkFixture(t, packDir, sdkRuntimeManifest)
	if got := ResolveSdkRuntimeSource(clientDir); got != packDir {
		t.Fatalf("有包目录时应优先取它，实得 %q", got)
	}

	// 只有不齐的副本时不随便挑一份 —— 挑了就等于把缺件藏起来。
	empty := t.TempDir()
	writeSdkFixture(t, filepath.Join(empty, "versions", "1.8.9", "natives", "runtime"), sdkRuntimeManifest[0:1])
	if got := ResolveSdkRuntimeSource(empty); got != "" {
		t.Fatalf("无齐全副本应返回空串，实得 %q", got)
	}
	if got := ResolveSdkRuntimeSource(""); got != "" {
		t.Fatalf("空客户端根应返回空串，实得 %q", got)
	}
}
