package launcher

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildJVMArgsKeepsClasspathBeforeTemplateMainClass 清单模板自带主类时，
// nexus 的 -cp 必须排在主类**之前**。
//
// 回归的是 1.21.8（NeoForge）线的真实故障：该清单的 jvm_arguments 尾部自带
// cpw.mods.bootstraplauncher.BootstrapLauncher，Java 只把第一个非选项 token 当主类，
// 其后一律按程序参数处理 —— -cp 若排在主类之后就会整段被当成程序参数，
// JVM 只剩模板里那个相对 classpath（cwd 是沙盒，解析不到），
// 于是报 ClassNotFoundException: cpw.mods.bootstraplauncher.BootstrapLauncher。
func TestBuildJVMArgsKeepsClasspathBeforeTemplateMainClass(t *testing.T) {
	const mainClass = "cpw.mods.bootstraplauncher.BootstrapLauncher"
	clientDir := t.TempDir()
	writeVersionManifest(t, clientDir, "1.21.8", `{
  "id": "1.21.8",
  "mainClass": "`+mainClass+`",
  "jvm_arguments": "-Dfile.encoding=UTF-8 -p libraries/a.jar --add-modules ALL-MODULE-PATH --add-opens java.base/java.util.jar=cpw.mods.securejarhandler `+mainClass+`",
  "parameter_arguments": "--username ${auth_player_name}"
}`)

	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.21.8",
		RoleName:  "Steve",
		UserID:    1,
		LocalPort: 25565,
	})
	if err != nil {
		t.Fatal(err)
	}

	cpIdx, mainIdx, mainCount := -1, -1, 0
	for i, a := range spec.Args {
		switch a {
		case "-cp":
			cpIdx = i
		case mainClass:
			mainCount++
			mainIdx = i
		}
	}
	if cpIdx < 0 {
		t.Fatalf("-cp 缺失：%q", spec.Args)
	}
	if mainIdx < 0 {
		t.Fatalf("主类缺失：%q", spec.Args)
	}
	if cpIdx > mainIdx {
		t.Fatalf("-cp（第 %d 项）必须排在主类（第 %d 项）之前，否则会被当成程序参数：%q",
			cpIdx, mainIdx, spec.Args)
	}
	if mainCount != 1 {
		t.Fatalf("主类出现 %d 次，期望 1 次（模板已提供，nexus 不应再补）：%q", mainCount, spec.Args)
	}
	if mainIdx+1 >= len(spec.Args) || !strings.HasPrefix(spec.Args[mainIdx+1], "--") {
		t.Fatalf("主类必须紧邻游戏段参数（其后第一项应是 -- 开头的游戏参数）：%q", spec.Args[mainIdx:])
	}
	if spec.Args[mainIdx+1] != "--username" {
		t.Fatalf("主类之后第一项 = %q，期望游戏段首个参数 --username", spec.Args[mainIdx+1])
	}
}

// TestBuildJVMArgsAppendsMainClassWhenTemplateLacksIt 模板不带主类时（1.20 那类清单），
// 主类由 nexus 补在 JVM 段末项。
func TestBuildJVMArgsAppendsMainClassWhenTemplateLacksIt(t *testing.T) {
	clientDir := t.TempDir()
	writeVersionManifest(t, clientDir, "1.20", `{
  "id": "1.20",
  "jvm_arguments": "-Dfile.encoding=UTF-8",
  "parameter_arguments": "--username ${auth_player_name}"
}`)

	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.20",
		RoleName:  "Steve",
		UserID:    1,
		LocalPort: 25565,
	})
	if err != nil {
		t.Fatal(err)
	}

	mainIdx := -1
	for i, a := range spec.Args {
		if a == defaultMainClass {
			mainIdx = i
		}
	}
	if mainIdx < 0 {
		t.Fatalf("模板未带主类时应补上默认主类 %q：%q", defaultMainClass, spec.Args)
	}
	if mainIdx+1 >= len(spec.Args) || spec.Args[mainIdx+1] != "--username" {
		t.Fatalf("主类后应紧邻游戏段参数：%q", spec.Args[mainIdx:])
	}
}

// TestBuildJVMArgsUsesTemplateClasspath -cp 必须取自清单模板自带的那份，而不是由
// libraries 列表重建。
//
// 实测依据：清单的 -cp 就是官方启动器实际传入的那一份（1.20 → 94 段、1.21.8 → 113 段，
// 与官方 java_args 完全一致）；而 libraries 列表不等于 classpath —— 1.20 的 libraries
// 缺 fmlloader / modlauncher 等 21 支，1.21.8 的 libraries 反而多出 3 支模块路径 JAR。
// 后者正是那次 IncompatibleClassChangeError（ModuleClassLoader 被 app 类加载器加载）的来源。
//
// 本用例同时固定住两条线的相反约定：1.20 的 -cp **保留** securejarhandler（官方如此），
// 因此不允许再用「剔除与 -p 重叠条目」这类规则。
func TestBuildJVMArgsUsesTemplateClasspath(t *testing.T) {
	const mainClass = "net.minecraft.client.main.Main"
	clientDir := t.TempDir()

	// 模板 -cp 里的三段：两支普通库 + 一支同时位于 -p 的 securejarhandler。
	for _, rel := range []string{
		"com/example/only-in-template.jar",
		"org/ow2/asm/asm/9.7/asm-9.7.jar",
		"cpw/mods/securejarhandler/2.1.10/securejarhandler-2.1.10.jar",
	} {
		mustWriteNested(t, filepath.Join(clientDir, "libraries", filepath.FromSlash(rel)), "jar")
	}
	// libraries 列表只给一支，且与模板 -cp 不同：用来证明模板 -cp 优先。
	writeVersionManifest(t, clientDir, "1.20", `{
  "id": "1.20",
  "mainClass": "`+mainClass+`",
  "jvm_arguments": "-Dfile.encoding=UTF-8 -p libraries/cpw/mods/securejarhandler/2.1.10/securejarhandler-2.1.10.jar -cp libraries/com/example/only-in-template.jar;libraries/org/ow2/asm/asm/9.7/asm-9.7.jar;libraries/cpw/mods/securejarhandler/2.1.10/securejarhandler-2.1.10.jar",
  "libraries": [
    {"name": "com.example:rebuilt-only:1.0", "downloads": {"artifact": {"path": "com/example/rebuilt-only.jar"}}}
  ],
  "parameter_arguments": "--username ${auth_player_name}"
}`)

	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.20",
		RoleName:  "Steve",
		UserID:    1,
		LocalPort: 25565,
	})
	if err != nil {
		t.Fatal(err)
	}

	var cp []string
	for i, a := range spec.Args {
		if a == "-cp" {
			cp = strings.Split(spec.Args[i+1], ";")
		}
	}
	if len(cp) != 3 {
		t.Fatalf("-cp 应取自模板的 3 段，实得 %d：%q", len(cp), cp)
	}
	wantPrefix := filepath.Join(clientDir, "libraries")
	for _, seg := range cp {
		if !strings.HasPrefix(seg, wantPrefix) {
			t.Fatalf("-cp 段应绝对化到客户端根下：%q", seg)
		}
	}
	joined := strings.Join(cp, ";")
	if strings.Contains(joined, "rebuilt-only") {
		t.Fatalf("-cp 不应由 libraries 重建：%q", cp)
	}
	if !strings.Contains(joined, "securejarhandler") {
		t.Fatalf("1.20 的 -cp 必须保留 securejarhandler（官方如此）：%q", cp)
	}
}

// TestAbsolutizeTemplatePaths 模板里能落到客户端根下的相对路径要绝对化，
// 非路径取值（--add-opens 的 module/package=target、-Dxxx=true 之类）必须原样保留。
func TestAbsolutizeTemplatePaths(t *testing.T) {
	root := t.TempDir()
	mustWriteNested(t, filepath.Join(root, "versions", "1.21.8", "log4j2.xml"), "x")
	mustWriteNested(t, filepath.Join(root, "libraries", "net", "neoforged", "bootstraplauncher.jar"), "x")

	in := []string{
		`-Dlog4j.configurationFile=versions\1.21.8\log4j2.xml`,
		`-Dminecraft.client.jar=versions\1.21.8\missing.jar`,
		"-DlibraryDirectory=libraries",
		"-p", "libraries/net/neoforged/bootstraplauncher.jar;libraries/nope.jar",
		"--add-opens", "java.base/java.util.jar=cpw.mods.securejarhandler",
		"-Dfile.encoding=UTF-8",
	}
	got := absolutizeTemplatePaths(in, root)

	wantLog4j := `-Dlog4j.configurationFile=` + filepath.Join(root, "versions", "1.21.8", "log4j2.xml")
	if got[0] != wantLog4j {
		t.Fatalf("log4j 配置路径未绝对化\n实得 %q\n期望 %q", got[0], wantLog4j)
	}
	if got[1] != in[1] {
		t.Fatalf("客户端根下不存在的路径不应改写：%q", got[1])
	}
	wantLib := "-DlibraryDirectory=" + filepath.Join(root, "libraries")
	if got[2] != wantLib {
		t.Fatalf("libraryDirectory 未绝对化\n实得 %q\n期望 %q", got[2], wantLib)
	}
	wantP := filepath.Join(root, "libraries", "net", "neoforged", "bootstraplauncher.jar") + ";libraries/nope.jar"
	if got[4] != wantP {
		t.Fatalf("-p 应只绝对化存在的段\n实得 %q\n期望 %q", got[4], wantP)
	}
	if got[5] != "--add-opens" || got[6] != in[6] {
		t.Fatalf("--add-opens 的 module/package=target 取值不应被改写：%q %q", got[5], got[6])
	}
	if got[7] != "-Dfile.encoding=UTF-8" {
		t.Fatalf("非路径取值不应被改写：%q", got[7])
	}
}

// TestBuildGameArgsDropsSingleDashUserProperties 清单模板里的 -userProperties
// （单横线）必须被剔除，否则与 nexus 追加的 --userProperties 构成重复传参。
//
// 真实故障（2026-09-22，MC 1.12.2）：minecraftArguments 模板带
// `-userProperties ${user_properties}`，而 buildGameArgs 追加的是
// `--userProperties`。joptsimple 把 -x 与 --x 视为同一个选项，客户端启动即
// 报 MultipleArgumentsForOptionException: Found multiple arguments for
// option userProperties。原先 dropFlagWithValue 按整串精确匹配，匹配不到
// 单横线写法。
func TestBuildGameArgsDropsSingleDashUserProperties(t *testing.T) {
	ctx := launchContext{
		WorkDir:          `C:\sandbox\.minecraft`,
		AssetsDir:        `C:\client\.minecraft\assets`,
		Version:          "1.12.2",
		RoleName:         "Steve",
		UUID:             "uuid",
		AssetIndex:       "1.12.2",
		LocalPort:        25565,
		UserProperties:   `{"uid":[1,0]}`,
		UserPropertiesEx: `{"GameType":2}`,
	}
	// 形状取自真实 1.12.2.json 的 minecraftArguments。
	template := "--username ${auth_player_name} --version ${version_name} " +
		"--gameDir ${game_directory} --assetsDir ${assets_root} " +
		"--tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker " +
		"-userProperties ${user_properties} --userPropertiesEx ${user_properties_ex}"

	got := buildGameArgs(ctx, template)

	// 统计每个握手参数出现几次（-x 与 --x 归一后计数）。
	counts := map[string]int{}
	for i := 0; i < len(got); i++ {
		name := strings.TrimLeft(got[i], "-")
		if name != "userProperties" && name != "userPropertiesEx" {
			continue
		}
		counts[name]++
		if i+1 >= len(got) {
			t.Fatalf("%s 缺少取值", got[i])
		}
		i++
	}
	for name, n := range counts {
		if n != 1 {
			t.Fatalf("%s 出现 %d 次，应恰好 1 次：%v", name, n, got)
		}
	}
	// 连接目标必须由 nexus 接管（本机 relay），不能被模板里的旧值顶掉。
	if !containsPair(got, "--server", "127.0.0.1") {
		t.Fatalf("--server 未指向本机 relay：%v", got)
	}
}

// containsPair 判断 token 列表里是否存在相邻的 (flag, value)。
func containsPair(tokens []string, flag, value string) bool {
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i] == flag && tokens[i+1] == value {
			return true
		}
	}
	return false
}
