// JVM 与游戏参数装配。
//
// 优先复用版本清单自带的参数模板（网易基础包的 jvm_arguments /
// parameter_arguments，或原版 minecraftArguments / arguments），
// 只把 nexus 权威拥有的项（classpath、natives 路径、连接目标、身份）覆盖掉；
// 清单未提供模板时才合成原版参数序列。
// 该策略对齐参考实现 Fantnel CommandService.BuildCommand。
package launcher

import (
	"crypto/rand"
	"fmt"
	"path/filepath"
	"strings"

	"nexus/internal/mcversion"
)

// launchContext 是参数装配所需的已解析上下文（由 launch.go 组装）。
type launchContext struct {
	ClientDir   string
	WorkDir     string
	Version     string
	RoleName    string
	UUID        string
	UserID      int64
	GameID      string
	LocalPort   int
	ServerHost  string
	ServerPort  int
	ControlPort int
	AuthPort    int
	AuthToken   string
	AssetsDir   string
	AssetIndex  string
	NativesDir  string
	LibsDir     string
	ClassPath   string
	// ClassPathList 是 ClassPath 的未拼接形态（分号/冒号分隔前）。
	// 超长时 classpath_jar.go 要靠它重写清单，从拼接串反拆会丢空段信息。
	ClassPathList []string
	MainClass     string
	// UserProperties / UserPropertiesEx 是启动器握手参数的取值，由 launch.go
	// 经 userproperties.go 装配一次。放进上下文而不是在装配点各算一次，
	// 是因为其中的 filterkey 是随机值：算两次会得到两个互不相同的取值。
	UserProperties   string
	UserPropertiesEx string
}

// defaultTuningArgs 是用户未提供 JvmArgs 时的默认调优参数。
// GB18030 是网易中文客户端的日志与配置编码，缺它会让中文路径与日志乱码。
func defaultTuningArgs() []string {
	return []string{
		"-Xmx4G",
		"-XX:+UseG1GC",
		"-XX:-UseAdaptiveSizePolicy",
		"-XX:-OmitStackTraceInFastThrow",
		"-Dfile.encoding=GB18030",
	}
}

// buildJVMArgs 装配 JVM 段参数，主类恒为末项。
//
// 顺序约束（实测踩过的坑）：清单模板必须排在 nexus 自有参数**之后**。
// 网易 NeoForge 清单（1.21.8）的 jvm_arguments 尾部自带主类
// （`... --add-exports ... cpw.mods.bootstraplauncher.BootstrapLauncher`），
// 而 Java 只把「第一个非选项 token」当主类、其后一律按程序参数处理；
// 若模板排在前面，nexus 的 -cp / -Djava.library.path 会整段被当成程序参数，
// JVM 只剩模板里那个相对 classpath（cwd 是沙盒 → 解析不到），
// 于是报 ClassNotFoundException: cpw.mods.bootstraplauncher.BootstrapLauncher。
func buildJVMArgs(ctx launchContext, userArgs []string, template string) []string {
	tuning := userArgs
	if len(tuning) == 0 {
		tuning = defaultTuningArgs()
	}

	// 清单模板里除 classpath / natives / 堆上限之外的项要保留：
	// 网易基础包依赖其中的 -p / --add-modules / --add-opens 才能加载 LWJGL。
	rest := jvmTemplateRemainder(ctx, template)

	args := make([]string, 0, len(tuning)+len(rest)+16)
	args = append(args, tuning...)

	// 身份与游戏标识（facts §2：userId 即 entity_id）。
	args = append(args, "-DServer=RELEASE", fmt.Sprintf("-DuserId=%d", ctx.UserID))
	ctrlPort := ctx.AuthPort
	if ctrlPort <= 0 {
		ctrlPort = ctx.ControlPort
	}
	if ctrlPort > 0 {
		args = append(args, fmt.Sprintf("-DlauncherControlPort=%d", ctrlPort))
	}
	if ctx.GameID != "" {
		args = append(args, fmt.Sprintf("-DlauncherGameId=%s", ctx.GameID))
	}
	if token := tokenArgument(ctx.AuthToken); token != "" {
		args = append(args, token)
	}

	// natives 与其 runtime 子目录（对齐参考实现 AddNativePath）。
	// -cp 超长时改走 classpath jar：Windows CreateProcess 命令行上限 32,767
	// 字符，1.12.2 那批旧清单的 libraries 全量展开实测 39,561 字符会直接
	// 启动失败（Go 只回报含糊的 "filename or extension is too long"）。
	cpJar, cpValue := resolveClassPathArg(ctx.ClassPathList, ctx.WorkDir)
	args = append(args,
		fmt.Sprintf("-Djava.library.path=%s", ctx.NativesDir),
		fmt.Sprintf("-Druntime_path=%s", filepath.Join(ctx.NativesDir, "runtime")),
		"-cp", cpValue,
	)
	_ = cpJar // 生成结果已内联进 cpValue；jar 路径单独留作日志排障。

	// 模板放最后：它可能自带主类，必须成为最后一个 JVM 项。
	args = append(args, rest...)
	// 模板没带主类时（如 1.20 那类清单）才由 nexus 补。模板自带与 ctx.MainClass
	// 不一致时以模板的为准（它是该清单的真实入口），此时多出的这一项会落到
	// 程序参数里，不影响启动。
	if !hasArg(rest, ctx.MainClass) {
		args = append(args, ctx.MainClass)
	}
	return args
}

// hasArg 判断 token 列表里是否已有完全相等的项。
func hasArg(tokens []string, want string) bool {
	for _, tok := range tokens {
		if tok == want {
			return true
		}
	}
	return false
}

// jvmTemplateRemainder 展开清单 JVM 模板，并剔除由 nexus 权威接管的项。
func jvmTemplateRemainder(ctx launchContext, template string) []string {
	if strings.TrimSpace(template) == "" {
		return nil
	}
	tokens := substituteAll(tokenizeArguments(template), jvmPlaceholders(ctx))
	tokens = dropFlagWithValue(tokens, "-cp", "-classpath")
	tokens = dropPrefixed(tokens,
		"-Djava.library.path", "-Druntime_path", "-Xmx", "-Xms")
	tokens = absolutizeTemplatePaths(tokens, ctx.ClientDir)
	return dropUnresolvedPlaceholders(tokens)
}

// absolutizeTemplatePaths 把模板里仍然相对、但能在客户端根下解析到实存路径的取值
// 改写为绝对路径。
//
// 为什么必须改：模板里的路径是「相对客户端根」的，例如实测 1.21.8 清单的
//
//	-Dlog4j.configurationFile=versions\1.21.8\log4j2.xml
//	-Dminecraft.client.jar=versions\1.21.8\1.21.8.jar
//	-DlibraryDirectory=libraries
//	-p libraries/net/neoforged/fancymodloader/bootstraplauncher/...
//
// 官方启动器靠把进程工作目录设成客户端根来解析它们；nexus 的工作目录是按服沙盒，
// 相对路径会落到沙盒上（实测模块路径整条失效，JVM 报 Unknown module:
// cpw.mods.securejarhandler）。参考实现 Fantnel 同样是把库目录绝对化
// （CommandService.cs:408-409 替换 ${library_directory} 与 libraryDirectory）。
//
// 只改写「拼到客户端根下确实存在」的取值，所以不会误伤
// `--add-opens java.base/java.util.jar=cpw.mods.securejarhandler` 这类非路径取值。
func absolutizeTemplatePaths(tokens []string, clientDir string) []string {
	if clientDir == "" {
		return tokens
	}
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		out[i] = absolutizeTokenPaths(tok, clientDir)
	}
	return out
}

// absolutizeTokenPaths 处理单个 token：`-Dname=value` 改写 value，其余按路径列表整体判定。
// 分号分隔的多段（-p / -Djava.library.path）逐段判定。
func absolutizeTokenPaths(tok, clientDir string) string {
	if head, value, ok := strings.Cut(tok, "="); ok {
		if rewritten, changed := absolutizePathList(value, clientDir); changed {
			return head + "=" + rewritten
		}
		return tok
	}
	if rewritten, changed := absolutizePathList(tok, clientDir); changed {
		return rewritten
	}
	return tok
}

// absolutizePathList 对可能由分号分隔的路径列表逐段绝对化，返回新值与是否发生改写。
func absolutizePathList(value, clientDir string) (string, bool) {
	segments := strings.Split(value, classPathSeparator())
	changed := false
	for i, seg := range segments {
		if seg == "" || filepath.IsAbs(seg) {
			continue
		}
		candidate := filepath.Join(clientDir, seg)
		if isExistingFile(candidate) || isExistingDir(candidate) {
			segments[i] = candidate
			changed = true
		}
	}
	return strings.Join(segments, classPathSeparator()), changed
}

// targetServerHostPort 返回客户端 --server 与 --port 的目标。
// 未显式指定 ServerHost 时恒为本机 relay（127.0.0.1:LocalPort）；
// 显式指定 ServerHost 时指向真实服务器地址（用户直连官方 IP 诉求）。
func targetServerHostPort(ctx launchContext) (string, int) {
	if ctx.ServerHost != "" && ctx.ServerPort > 0 {
		return ctx.ServerHost, ctx.ServerPort
	}
	return relayHost, ctx.LocalPort
}

// buildGameArgs 装配游戏段参数。
func buildGameArgs(ctx launchContext, template string) []string {
	if strings.TrimSpace(template) == "" {
		return synthesizeGameArgs(ctx)
	}

	tokens := substituteAll(tokenizeArguments(template), gamePlaceholders(ctx))
	tokens = dropFlagWithValue(tokens, "--server", "--port", "--gameDir", "--assetsDir",
		"--userProperties", "--userPropertiesEx")
	tokens = dropUnresolvedPlaceholderPairs(tokens)

	targetHost, targetPort := targetServerHostPort(ctx)
	// nexus 权威接管的项：工作目录、资源目录与连接目标。
	tokens = append(tokens,
		"--gameDir", ctx.WorkDir,
		"--assetsDir", ctx.AssetsDir,
		"--server", targetHost,
		"--port", fmt.Sprintf("%d", targetPort),
	)
	return append(tokens, launcherHandshakeArgs(ctx)...)
}

// synthesizeGameArgs 合成原版参数序列（版本清单未给模板时使用）。
func synthesizeGameArgs(ctx launchContext) []string {
	targetHost, targetPort := targetServerHostPort(ctx)
	args := []string{
		"--username", ctx.RoleName,
		"--version", ctx.Version,
		"--gameDir", ctx.WorkDir,
		"--assetsDir", ctx.AssetsDir,
		"--assetIndex", ctx.AssetIndex,
		"--uuid", ctx.UUID,
		"--accessToken", accessTokenFor(ctx.Version),
		"--userType", "mojang",
		"--versionType", "release",
		"--server", targetHost,
		"--port", fmt.Sprintf("%d", targetPort),
	}
	return append(args, launcherHandshakeArgs(ctx)...)
}

// launcherHandshakeArgs 是客户端识别启动器所需的两个参数。
// 其中 --userProperties 携带控制通道端口（launcherport），是自动进服的前提。
func launcherHandshakeArgs(ctx launchContext) []string {
	return []string{
		"--userProperties", ctx.UserProperties,
		"--userPropertiesEx", ctx.UserPropertiesEx,
	}
}

// accessTokenFor 返回 --accessToken 取值。
// 1.18 起客户端接受占位 "0"；更早版本会校验长度，需要 32 位大写 hex
// （取值规则对齐参考实现 Fantnel CommandService.BuildCommand）。
func accessTokenFor(version string) string {
	if mcversion.AtLeast(version, "1.18") {
		return "0"
	}
	const alphabet = "ABCDEF0123456789"
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// 随机源不可用时退回占位值：宁可让老版本客户端明确报鉴权格式错，
		// 也不要用可预测的固定串冒充随机 token。
		return "0"
	}
	for i, b := range buf {
		buf[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(buf)
}

// jvmPlaceholders 是 JVM 模板占位符表（原版 arguments.jvm 口径）。
func jvmPlaceholders(ctx launchContext) map[string]string {
	return map[string]string{
		"${classpath}":           ctx.ClassPath,
		"${natives_directory}":   ctx.NativesDir,
		"${library_directory}":   ctx.LibsDir,
		"${classpath_separator}": classPathSeparator(),
		"${version_name}":        ctx.Version,
		"${launcher_name}":       "nexus",
		"${launcher_version}":    "1",
		"${game_directory}":      ctx.WorkDir,
	}
}

// gamePlaceholders 是游戏参数模板占位符表（原版 minecraftArguments 口径）。
func gamePlaceholders(ctx launchContext) map[string]string {
	return map[string]string{
		"${auth_player_name}":  ctx.RoleName,
		"${auth_uuid}":         ctx.UUID,
		"${auth_access_token}": accessTokenFor(ctx.Version),
		"${auth_session}":      accessTokenFor(ctx.Version),
		"${version_name}":      ctx.Version,
		"${version_type}":      "release",
		"${user_type}":         "mojang",
		"${game_directory}":    ctx.WorkDir,
		"${assets_root}":       ctx.AssetsDir,
		"${game_assets}":       ctx.AssetsDir,
		"${assets_index_name}": ctx.AssetIndex,
		// 客户端靠 user_properties 里的 launcherport 回连控制通道，
		// 由 userproperties.go 装配（缺它就不会自动进服）。
		"${user_properties}": ctx.UserProperties,
	}
}

// substituteUserPlaceholders 展开用户自定义模板的 {{...}} 占位符。
func substituteUserPlaceholders(ctx launchContext, args []string) []string {
	targetHost, targetPort := targetServerHostPort(ctx)
	table := map[string]string{
		"{{roleName}}":  ctx.RoleName,
		"{{username}}":  ctx.RoleName,
		"{{uuid}}":      ctx.UUID,
		"{{userId}}":    fmt.Sprintf("%d", ctx.UserID),
		"{{gameId}}":    ctx.GameID,
		"{{host}}":      targetHost,
		"{{server}}":    fmt.Sprintf("%s:%d", targetHost, targetPort),
		"{{port}}":      fmt.Sprintf("%d", targetPort),
		"{{gameDir}}":   ctx.WorkDir,
		"{{version}}":   ctx.Version,
		"{{assetsDir}}": ctx.AssetsDir,
	}
	return substituteAll(args, table)
}

// substituteAll 对每个 token 做整表替换。
func substituteAll(tokens []string, table map[string]string) []string {
	out := make([]string, len(tokens))
	for i, tok := range tokens {
		for key, value := range table {
			tok = strings.ReplaceAll(tok, key, value)
		}
		out[i] = tok
	}
	return out
}

// tokenizeArguments 按空白切分参数串，并识别成对引号（路径常含空格）。
func tokenizeArguments(s string) []string {
	var (
		out   []string
		cur   strings.Builder
		quote rune
		open  bool
	)
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case open && r == quote:
			open = false
		case !open && (r == '"' || r == '\''):
			open, quote = true, r
		case !open && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// dropFlagWithValue 移除「开关 + 紧随其后的值」这种成对参数。
// dropFlagWithValue 成对移除给定选项及其取值。
//
// 匹配按"去掉前导横线后的名字"进行：joptsimple 等解析器把 -x 与 --x 视为
// 同一个选项，而网易清单模板里两种写法混用（实测 1.12.2 的
// minecraftArguments 用 -userProperties，nexus 追加的是 --userProperties）。
// 原先按整串精确匹配，导致模板里那对没被剔除、又被追加一遍，客户端启动即
// 报 MultipleArgumentsForOptionException（2026-09-22 实测）。
func dropFlagWithValue(tokens []string, flags ...string) []string {
	target := make(map[string]struct{}, len(flags))
	for _, f := range flags {
		target[optionFlagName(f)] = struct{}{}
	}
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		if _, hit := target[optionFlagName(tokens[i])]; hit {
			i++ // 连带跳过其值
			continue
		}
		out = append(out, tokens[i])
	}
	return out
}

// optionFlagName 去掉选项名前的横线，用于 -x / --x 归一化比较。
func optionFlagName(tok string) string {
	return strings.TrimLeft(tok, "-")
}

// dropPrefixed 移除以给定前缀开头的单体参数（如 -Xmx512m）。
func dropPrefixed(tokens []string, prefixes ...string) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		matched := false
		for _, p := range prefixes {
			if strings.HasPrefix(tok, p) {
				matched = true
				break
			}
		}
		if !matched {
			out = append(out, tok)
		}
	}
	return out
}

// dropUnresolvedPlaceholders 移除仍含 ${...} 的 token：
// 未知占位符原样进 argv 会让 JVM 报难以定位的参数错误。
func dropUnresolvedPlaceholders(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if strings.Contains(tok, "${") {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// dropUnresolvedPlaceholderPairs 移除值仍是未知占位符的「开关 + 值」对，
// 以及残留的裸占位符 token。
func dropUnresolvedPlaceholderPairs(tokens []string) []string {
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "--") && i+1 < len(tokens) && strings.Contains(tokens[i+1], "${") {
			i++
			continue
		}
		if strings.Contains(tok, "${") {
			continue
		}
		out = append(out, tok)
	}
	return out
}
