package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildMinecraftLaunchCustomArgs(t *testing.T) {
	clientDir := t.TempDir()
	opts := LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.20.1",
		RoleName:  "TestPlayer",
		UserID:    88888,
		LocalPort: 25565,
		JvmArgs:   []string{"-Xmx4G"},
		CustomLaunchArgs: []string{
			"--username", "{{roleName}}",
			"--uuid", "{{uuid}}",
			"--server", "{{server}}",
		},
	}

	spec, err := BuildMinecraftLaunch(opts)
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}
	if spec.Exe != opts.JavaPath {
		t.Fatalf("Exe = %s，期望 %s", spec.Exe, opts.JavaPath)
	}
	// GameID 为空 → 无沙盒 → 工作目录即客户端目录。
	if spec.WorkDir != clientDir {
		t.Fatalf("WorkDir = %s，期望 %s", spec.WorkDir, clientDir)
	}

	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "TestPlayer") {
		t.Fatalf("参数应含角色名：%s", joined)
	}
	if !strings.Contains(joined, "127.0.0.1:25565") {
		t.Fatalf("参数应含本机 relay 地址：%s", joined)
	}
	if strings.Contains(joined, "{{") {
		t.Fatalf("占位符未全部替换：%s", joined)
	}
}

func TestBuildMinecraftLaunchSynthesizesVanillaArgs(t *testing.T) {
	clientDir := t.TempDir()
	opts := LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.20.1",
		RoleName:  "SaintSteve",
		UserID:    10001,
		LocalPort: 25565,
	}

	spec, err := BuildMinecraftLaunch(opts)
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}

	joined := strings.Join(spec.Args, " ")
	for _, want := range []string{
		defaultMainClass,
		"--server 127.0.0.1 --port 25565",
		"--username SaintSteve",
		"-DServer=RELEASE",
		"-DuserId=10001",
		"-Druntime_path=",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("参数缺少 %q：%s", want, joined)
		}
	}
}

// TestBuildMinecraftLaunchServerIsLocalRelay 守卫「--server 恒为本机 relay 地址」。
//
// 这是回归守卫：曾经为对齐所谓"官方命令行形状"把 --server 写成服务器公开域名
// （pc.bjdmc.net），导致客户端去连真实服务器而不是 relay，白端进服全部超时。
// 客户端必须连 relay，真实上游由代理自己拨 —— 与 --server 取值无关。
func TestBuildMinecraftLaunchServerIsLocalRelay(t *testing.T) {
	clientDir := t.TempDir()
	base := LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   "1.20.1",
		RoleName:  "SaintSteve",
		UserID:    10001,
		LocalPort: 25565,
	}

	spec, err := BuildMinecraftLaunch(base)
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}
	if got := argumentValue(t, spec.Args, "--server"); got != "127.0.0.1" {
		t.Fatalf("--server 必须是本机 relay，实得 %q", got)
	}
	if got := argumentValue(t, spec.Args, "--port"); got != "25565" {
		t.Fatalf("--port 应为 relay 端口，实得 %q", got)
	}

	// 自定义参数模板走全量覆盖路径（launch.go：模板非空时只做占位符替换，
	// 不经 buildGameArgs），因此 --server 由模板自身决定 —— 这里只坐实
	// 该路径不会把 relay 地址写坏，避免回归守卫对两种路径做出错误承诺。
	spec, err = BuildMinecraftLaunch(LaunchOptions{
		JavaPath:         `C:\Java\bin\javaw.exe`,
		ClientDir:        clientDir,
		Version:          "1.20.1",
		RoleName:         "SaintSteve",
		UserID:           10001,
		LocalPort:        25565,
		CustomLaunchArgs: []string{"--server", "{{host}}", "--port", "{{port}}"},
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}
	if got := argumentValue(t, spec.Args, "--server"); got != "127.0.0.1" {
		t.Fatalf("模板占位符 {{host}} 应展开为 relay，实得 %q", got)
	}
	if got := argumentValue(t, spec.Args, "--port"); got != "25565" {
		t.Fatalf("模板占位符 {{port}} 应展开为 relay 端口，实得 %q", got)
	}
}

// TestBuildMinecraftLaunchDirectServerMode 守卫直连官方服模式的参数形状
// （对齐 Fantnel：客户端 --server 指向真实服务器，AuthLib join 由本地
// 鉴权代答通道接住；控制通道端口仍走 --userProperties.launcherport）。
func TestBuildMinecraftLaunchDirectServerMode(t *testing.T) {
	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:    `C:\Java\bin\javaw.exe`,
		ClientDir:   t.TempDir(),
		Version:     "1.20.1",
		RoleName:    "SaintSteve",
		UserID:      10001,
		LocalPort:   25565,
		ServerHost:  "pc.example.net",
		ServerPort:  20001,
		ControlPort: 11413,
		AuthPort:    11414,
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}

	if got := argumentValue(t, spec.Args, "--server"); got != "pc.example.net" {
		t.Fatalf("直连模式 --server 应为真实服务器地址，实得 %q", got)
	}
	if got := argumentValue(t, spec.Args, "--port"); got != "20001" {
		t.Fatalf("直连模式 --port 应为真实服务器端口，实得 %q", got)
	}
	if got := jvmProperty(t, spec.Args, "-DlauncherControlPort"); got != "11414" {
		t.Fatalf("-DlauncherControlPort 应为鉴权代答端口，实得 %q", got)
	}
	// 控制通道（RPC 推进服目标）仍走 launcherport，两者必须分立。
	props := argumentValue(t, spec.Args, "--userProperties")
	if !strings.Contains(props, `"launcherport":[11413,0]`) {
		t.Fatalf("直连模式下 launcherport 仍应为控制通道端口：%s", props)
	}
}

// TestBuildMinecraftLaunchAuthPortFallsBackToControlPort 守卫 relay 模式回落：
// 未开鉴权代答通道（AuthPort=0）时 -DlauncherControlPort 写控制通道端口，
// 与既有 relay 链路行为完全一致。
func TestBuildMinecraftLaunchAuthPortFallsBackToControlPort(t *testing.T) {
	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:    `C:\Java\bin\javaw.exe`,
		ClientDir:   t.TempDir(),
		Version:     "1.20.1",
		RoleName:    "Steve",
		UserID:      4242,
		LocalPort:   25577,
		ControlPort: 11413,
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}
	if got := jvmProperty(t, spec.Args, "-DlauncherControlPort"); got != "11413" {
		t.Fatalf("AuthPort=0 时应回落控制通道端口，实得 %q", got)
	}
}

// jvmProperty 读取 key=value 单 token 形式的 JVM 系统属性值（如 -DlauncherControlPort=11413）。
func jvmProperty(t *testing.T, args []string, key string) string {
	t.Helper()
	prefix := key + "="
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
	}
	t.Fatalf("JVM 属性 %s 缺失：%v", key, args)
	return ""
}

// TestBuildMinecraftLaunchUsesManifestTemplate 守卫「版本清单自带参数模板时以模板为准，
// 但连接目标与工作目录由 nexus 接管」这一约定（对齐 Fantnel BuildCommand）。
func TestBuildMinecraftLaunchUsesManifestTemplate(t *testing.T) {
	clientDir := t.TempDir()
	version := "1.20.1"
	writeVersionManifest(t, clientDir, version, `{
	  "id": "1.20.1",
	  "mainClass": "net.minecraft.client.main.Main",
	  "assetIndex": {"id": "5"},
	  "jvm_arguments": "-Xmx1G -XX:+ShowCodeDetailsInExceptionMessages --add-opens java.base/java.lang=ALL-UNNAMED -Djava.library.path=D:\\stale -cp libraries\\tpl-only.jar",
	  "parameter_arguments": "--username ${auth_player_name} --uuid ${auth_uuid} --accessToken ${auth_access_token} --userProperties ${user_properties} --gameDir D:\\stale --server 1.2.3.4 --port 9999 --launchTag netease"
	}`)

	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: clientDir,
		Version:   version,
		RoleName:  "Tpl",
		UserID:    4242,
		LocalPort: 25577,
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}
	joined := strings.Join(spec.Args, " ")

	// 模板独有的项必须保留。
	for _, want := range []string{
		"--add-opens java.base/java.lang=ALL-UNNAMED",
		"-XX:+ShowCodeDetailsInExceptionMessages",
		"--launchTag netease",
		"--username Tpl",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("模板项 %q 丢失：%s", want, joined)
		}
	}
	// 模板里的 ${user_properties} 必须换成真实取值，而不是空对象或残留占位符。
	if !strings.Contains(joined, `--userProperties {"`) {
		t.Fatalf("--userProperties 未填入真实取值：%s", joined)
	}
	if !strings.Contains(joined, `"uid":[4242,0]`) {
		t.Fatalf("--userProperties 缺 uid：%s", joined)
	}
	// nexus 权威接管的项必须覆盖模板里的陈旧值。
	if !strings.Contains(joined, "--server 127.0.0.1 --port 25577") {
		t.Fatalf("连接目标未被接管：%s", joined)
	}
	if strings.Contains(joined, "1.2.3.4") || strings.Contains(joined, "9999") {
		t.Fatalf("模板中的旧连接目标未被移除：%s", joined)
	}
	if strings.Contains(joined, `D:\stale`) {
		t.Fatalf("模板中的旧路径未被移除：%s", joined)
	}
	// 模板自带的 -cp 是权威来源（实测与官方 java_args 逐段一致），
	// 但其中的相对路径必须绝对化到客户端根下 —— 否则 JVM 会去按服沙盒里找这些 JAR。
	if !strings.Contains(joined, filepath.Join(clientDir, "libraries", "tpl-only.jar")) {
		t.Fatalf("模板 -cp 的相对路径未绝对化到客户端根：%s", joined)
	}
	if strings.Contains(joined, "-Xmx1G") {
		t.Fatalf("模板堆上限应由 nexus 接管：%s", joined)
	}
	if strings.Contains(joined, "${") {
		t.Fatalf("占位符未全部处理：%s", joined)
	}
}

// TestBuildMinecraftLaunchCarriesControlPort 守卫自动进服的前提：
// 控制通道端口必须一路落到 argv 的 --userProperties.launcherport，
// 客户端靠它回连启动器索要进服目标。这条链断了客户端就只会停在主菜单。
func TestBuildMinecraftLaunchCarriesControlPort(t *testing.T) {
	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:    `C:\Java\bin\javaw.exe`,
		ClientDir:   t.TempDir(),
		Version:     "1.20.1",
		RoleName:    "Steve",
		UserID:      4242,
		LocalPort:   25577,
		ControlPort: 11413,
		AuthToken:   "plain-token",
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}

	props := argumentValue(t, spec.Args, "--userProperties")
	if !strings.Contains(props, `"launcherport":[11413,0]`) {
		t.Fatalf("--userProperties 缺 launcherport：%s", props)
	}
	// 同一次启动只允许一个 filterkey：取值算两次会得到两个互不相同的随机串。
	if strings.Count(strings.Join(spec.Args, " "), "filterkey") != 1 {
		t.Fatalf("filterkey 出现次数应为 1：%v", spec.Args)
	}
	if !hasArgumentPrefix(spec.Args, "-DToken=") {
		t.Fatal("给出 AuthToken 时应写出 -DToken")
	}
}

// TestBuildMinecraftLaunchOmitsUnboundControlPort 控制通道没开时不得写出端口 0：
// 让客户端去连一个没有监听的端口比不给端口更糟。
func TestBuildMinecraftLaunchOmitsUnboundControlPort(t *testing.T) {
	spec, err := BuildMinecraftLaunch(LaunchOptions{
		JavaPath:  `C:\Java\bin\javaw.exe`,
		ClientDir: t.TempDir(),
		Version:   "1.20.1",
		RoleName:  "Steve",
		UserID:    4242,
		LocalPort: 25577,
	})
	if err != nil {
		t.Fatalf("BuildMinecraftLaunch failed: %v", err)
	}

	if props := argumentValue(t, spec.Args, "--userProperties"); strings.Contains(props, "launcherport") {
		t.Fatalf("未开控制通道时不应写 launcherport：%s", props)
	}
	if hasArgumentPrefix(spec.Args, "-DToken=") {
		t.Fatal("AuthToken 为空时不应写 -DToken")
	}
}

// argumentValue 取「开关 + 值」中的值。
func argumentValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("参数 %s 缺失：%v", flag, args)
	return ""
}

// hasArgumentPrefix 判断是否存在以给定前缀开头的参数。
func hasArgumentPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

// TestAccessTokenForVersion 守卫低版本白端需要 32 位 hex token 的差异。
func TestAccessTokenForVersion(t *testing.T) {
	if got := accessTokenFor("1.20.1"); got != "0" {
		t.Fatalf("1.20.1 accessToken = %q，期望 \"0\"", got)
	}
	got := accessTokenFor("1.12.2")
	if len(got) != 32 {
		t.Fatalf("1.12.2 accessToken 长度 = %d，期望 32：%q", len(got), got)
	}
	for _, r := range got {
		if !strings.ContainsRune("ABCDEF0123456789", r) {
			t.Fatalf("accessToken 含非 hex 字符 %c：%q", r, got)
		}
	}
}

func TestTokenizeArgumentsRespectsQuotes(t *testing.T) {
	got := tokenizeArguments(`-cp "C:\a b\x.jar" --name "Hello World" -Xmx2G`)
	want := []string{"-cp", `C:\a b\x.jar`, "--name", "Hello World", "-Xmx2G"}
	if len(got) != len(want) {
		t.Fatalf("token 数 = %d，期望 %d：%q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token[%d] = %q，期望 %q", i, got[i], want[i])
		}
	}
}

func TestEnsureOptionsTxt(t *testing.T) {
	clientDir := t.TempDir()
	EnsureOptionsTxt(clientDir)

	data, err := os.ReadFile(filepath.Join(clientDir, "options.txt"))
	if err != nil {
		t.Fatalf("options.txt 应被创建：%v", err)
	}
	for _, want := range []string{"lang:zh_cn", "maxFps:120", "guiScale:2"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("options.txt 缺少 %q：%s", want, data)
		}
	}
}

// TestEnsureOptionsTxtKeepsExisting 守卫不覆盖用户已有设置。
func TestEnsureOptionsTxtKeepsExisting(t *testing.T) {
	clientDir := t.TempDir()
	path := filepath.Join(clientDir, "options.txt")
	mustWrite(t, path, "lang:en_us\n")

	EnsureOptionsTxt(clientDir)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "lang:en_us\n" {
		t.Fatalf("已有 options.txt 被覆盖：%s", data)
	}
}

func TestDetectServerClientDir(t *testing.T) {
	clientDir, modsDir, version := DetectServerClientDir("", "1.20")
	if clientDir == "" || modsDir == "" || version == "" {
		t.Fatalf("探测结果不应为空：dir=%q mods=%q ver=%q", clientDir, modsDir, version)
	}
}

func TestPrepareGameRuntime(t *testing.T) {
	// baseGameDir 用临时目录：真实基础目录含整套组件，测试不该把它们搬进沙盒。
	baseGameDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseGameDir, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(baseGameDir, "mods", "4690372021023046883@3@0.jar"), "fake-core-mod")

	// nexus 组件缓存（该服按网关清单下载的组件）作为最后一路叠加来源。
	cacheDir := t.TempDir()
	mustWrite(t, filepath.Join(cacheDir, "4690287328189015853@2@8.jar"), "cached-asset-mod")

	sandbox, report := PrepareGameRuntime(RuntimeSource{
		BaseGameDir:       baseGameDir,
		ComponentCacheDir: cacheDir,
		GameID:            "test-srv-999",
		RoleName:          "Steve",
	})
	if runtime.GOOS != "windows" {
		if sandbox != "" {
			t.Fatalf("非 Windows 平台应返回空串，得到 %q", sandbox)
		}
		return
	}
	if sandbox == "" {
		t.Fatal("期望非空沙盒目录")
	}
	if !strings.Contains(sandbox, "test-srv-999-Steve") {
		t.Fatalf("沙盒路径应含 gameId-roleName：%s", sandbox)
	}
	if !isExistingFile(filepath.Join(sandbox, "options.txt")) {
		t.Fatalf("沙盒应含 options.txt：%s", sandbox)
	}
	// 核心组件必须真的进沙盒：这正是旧规则漏掉的环，客户端靠它回连控制通道。
	if !isExistingFile(filepath.Join(sandbox, "mods", "4690372021023046883@3@0.jar")) {
		t.Fatal("核心组件应被播种进沙盒 mods")
	}
	if report.BaseMods != 1 {
		t.Fatalf("BaseMods = %d，期望 1", report.BaseMods)
	}
	// nexus 组件缓存也要进沙盒：本地没有网易缓存时，这是该服组件的唯一来源。
	if !isExistingFile(filepath.Join(sandbox, "mods", "4690287328189015853@2@8.jar")) {
		t.Fatal("组件缓存里的组件应被叠加进沙盒 mods")
	}
	if report.CachedMods != 1 {
		t.Fatalf("CachedMods = %d，期望 1", report.CachedMods)
	}
}

func writeVersionManifest(t *testing.T, clientDir, version, body string) {
	t.Helper()
	dir := versionDir(clientDir, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, version+".json"), body)
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
