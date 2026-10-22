// Java 运行时探测。
//
// 探测顺序：JAVA_HOME → 系统 PATH → 已知安装目录（网易启动器内置 JRE 与主流发行版）。
// 版本档位按上游 Minecraft 的实际要求划分，与参考实现 Fantnel 的档位有意不同：
// Fantnel 把 1.16 也归到 Java 17，而 1.16 的 Forge 在 Java 17 下无法加载，
// 因此这里保留 Java 8。改动该档位需要同时给出 1.16 客户端的实测结论。
package launcher

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"nexus/internal/mcversion"
)

// errNoJavaRuntime 表示本机未探测到任何 Java 运行时。
// 调用方可据此提示用户手动配置 JavaPath，而不是拿空路径去起进程。
var errNoJavaRuntime = errors.New("launcher: 未探测到本机 Java 运行时，请在环境档案中手动指定 Java 路径")

// javaExecutableNames 返回本平台的 Java 可执行文件名（javaw 优先，避免多出控制台窗口）。
func javaExecutableNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"javaw.exe", "java.exe"}
	}
	return []string{"java"}
}

// DetectJava 探测一个可用的 Java 可执行文件路径。
func DetectJava() (string, error) {
	if home := strings.TrimSpace(os.Getenv("JAVA_HOME")); home != "" {
		for _, name := range javaExecutableNames() {
			if path := filepath.Join(home, "bin", name); isExistingFile(path) {
				return path, nil
			}
		}
	}
	for _, name := range javaExecutableNames() {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	if path, ok := firstMatchingFile(installedJavaGlobs()); ok {
		return path, nil
	}
	return "", errNoJavaRuntime
}

// DetectJavaForVersion 按目标 Minecraft 版本优选匹配的 Java 大版本，
// 未命中时回落到 DetectJava。
func DetectJavaForVersion(mcVersion string) (string, error) {
	if runtime.GOOS != "windows" {
		return DetectJava()
	}
	if path, ok := firstMatchingFile(preferredJavaGlobs(mcVersion)); ok {
		return path, nil
	}
	return DetectJava()
}

// preferredJavaGlobs 返回该 MC 版本首选 Java 大版本的安装位置模式。
func preferredJavaGlobs(mcVersion string) []string {
	appdata := os.Getenv("APPDATA")
	switch {
	case mcversion.AtLeast(mcVersion, "1.20.5"):
		// 1.20.5 起原版要求 Java 21，但网易客户端 1.21.8 实际用的是自带 jdk22
		// （官方启动器 dump：1.20 → ext\jre-v64-220420\jdk17，1.21.8 → 同目录 jdk22；
		// 该 ext 下只有 jdk17 与 jdk22，没有 jdk21）。
		// 先找 21 再找 22：装了标准 JDK 21 的机器仍优先用它，网易机器由 22 命中；
		// 否则会落到 PATH 上的任意版本（实测曾选中 Zulu 25，客户端启动即 exit 1）。
		return append(javaGlobsForMajor(appdata, "21"), javaGlobsForMajor(appdata, "22")...)
	case mcversion.AtLeast(mcVersion, "1.17"):
		return javaGlobsForMajor(appdata, "17")
	default:
		// 1.16 及更早：Java 8。网易启动器内置 JRE 即为 8，放在最前。
		return []string{
			filepath.Join(appdata, "NetEase", "MCLauncher", "jre*", "bin", "javaw.exe"),
			`C:\Program Files\Java\jre1.8*\bin\javaw.exe`,
			`C:\Program Files\Java\jdk1.8*\bin\javaw.exe`,
			`C:\Program Files (x86)\Java\jre1.8*\bin\javaw.exe`,
			`C:\Program Files\Eclipse Adoptium\jdk-8*\bin\javaw.exe`,
		}
	}
}

// javaGlobsForMajor 拼出某个 Java 大版本的常见安装位置模式。
func javaGlobsForMajor(appdata, major string) []string {
	globs := []string{
		`C:\Program Files\Java\jdk-` + major + `*\bin\javaw.exe`,
		`C:\Program Files\Eclipse Adoptium\jdk-` + major + `*\bin\javaw.exe`,
		`C:\Program Files\Microsoft\jdk-` + major + `*\bin\javaw.exe`,
		filepath.Join(appdata, "NetEase", "MCLauncher", "*"+major+"*", "bin", "javaw.exe"),
	}
	for _, root := range neteaseInstallRoots() {
		globs = append(globs, neteaseJavaGlobsForMajor(root, major)...)
	}
	return globs
}

// neteaseJavaGlobsForMajor 返回某个盘根下网易自带 JRE 匹配指定 Java 大版本的候选模式。
//
// 实测布局（官方启动器 dump 的 java_path 逐字命中）：
// D:\MCLDownload\ext\jre-v64-220420\jdk17\bin\javaw.exe
// 即 <盘根>\MCLDownload\ext\jre-<构建号>\jdk<大版本>\bin\javaw.exe。
// 中间两段（jre-* / *17*）层级与命名都不固定，因此两个都可能承载版本号的分量都加通配。
// 抽成独立函数是为了能对着真实形状的临时目录做端到端 glob 验证（javaruntime_test.go）。
func neteaseJavaGlobsForMajor(root, major string) []string {
	return []string{
		filepath.Join(root, "MCLDownload", "ext", "jre-*", "*"+major+"*", "bin", "javaw.exe"),
		filepath.Join(root, "MCLDownload", "ext", "jre-*", "jdk"+major, "bin", "javaw.exe"),
		filepath.Join(root, "MCLDownload", "ext", "jre-*", "jre"+major, "bin", "javaw.exe"),
		filepath.Join(root, "NetEase", "MCLauncher", "*"+major+"*", "bin", "javaw.exe"),
	}
}

// neteaseJavaGlobs 返回某个盘根下网易自带 JRE 与版本无关的候选模式。
func neteaseJavaGlobs(root string) []string {
	return []string{
		filepath.Join(root, "MCLDownload", "ext", "jre-*", "*", "bin", "javaw.exe"),
		filepath.Join(root, "NetEase", "MCLauncher", "jre*", "bin", "javaw.exe"),
	}
}

// installedJavaGlobs 返回与版本无关的已知 Java 安装位置模式。
func installedJavaGlobs() []string {
	if runtime.GOOS != "windows" {
		return nil
	}
	appdata := os.Getenv("APPDATA")
	localAppData := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")

	globs := []string{
		filepath.Join(appdata, "NetEase", "MCLauncher", "jre*", "bin", "javaw.exe"),
		filepath.Join(localAppData, "NetEase", "MCLauncher", "jre*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Eclipse Adoptium", "jdk*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Java", "jdk*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Java", "jre*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Microsoft", "jdk*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "BellSoft", "LibericaJDK*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Zulu", "zulu*", "bin", "javaw.exe"),
		filepath.Join(programFiles, "Amazon Corretto", "jdk*", "bin", "javaw.exe"),
		filepath.Join(programFilesX86, "Java", "jre*", "bin", "javaw.exe"),
		filepath.Join(appdata, ".minecraft", "runtime", "*", "*", "bin", "javaw.exe"),
		filepath.Join(appdata, ".minecraft", "runtime", "*", "bin", "javaw.exe"),
	}
	// 网易启动器与各盘 MCLDownload ext 下的 JRE。
	for _, root := range neteaseInstallRoots() {
		globs = append(globs, neteaseJavaGlobs(root)...)
	}
	return globs
}

// firstMatchingFile 返回首个命中且是普通文件的 glob 结果。
func firstMatchingFile(patterns []string) (string, bool) {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		matches, _ := filepath.Glob(pattern)
		for _, m := range matches {
			if isExistingFile(m) {
				return m, true
			}
		}
	}
	return "", false
}

// isExistingFile 判断路径存在且不是目录。
func isExistingFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// RequiredJavaMajors 返回该 MC 版本档位允许的 Java 大版本集合。
// 档位与 preferredJavaGlobs 一致：1.20.5+ 用 21/22（网易自带 jdk22），
// 1.17~1.20.4 用 17，1.16 及更早只能用 8（旧版 Forge 在 Java 9+ 无法加载）。
func RequiredJavaMajors(mcVersion string) []int {
	switch {
	case mcversion.AtLeast(mcVersion, "1.20.5"):
		return []int{21, 22}
	case mcversion.AtLeast(mcVersion, "1.17"):
		return []int{17}
	default:
		return []int{8}
	}
}

// javaProbeError 表示该 Java 根本无法探测：文件缺失、无法执行或非零退出。
// 与「能跑但版本串解析不出」必须区分 —— 前者拦截后调用方能走 CDN 部署自愈，
// 后者保守放行。
type javaProbeError struct{ cause error }

func (e javaProbeError) Error() string { return e.cause.Error() }

func (e javaProbeError) Unwrap() error { return e.cause }

// JavaMajor 返回指定 Java 可执行文件的实际大版本（执行 -version 并解析输出）。
// 同目录优先用 java.exe（javaw 无控制台但 -version 仍写 stderr，取同位 java 更稳）。
// 探测层面的失败（缺失/无法执行/非零退出）包在 javaProbeError 里浮出。
func JavaMajor(javaPath string) (int, error) {
	dir := filepath.Dir(javaPath)
	exe := javaPath
	for _, name := range []string{"java.exe", "java"} {
		if candidate := filepath.Join(dir, name); isExistingFile(candidate) {
			exe = candidate
			break
		}
	}
	out, err := exec.Command(exe, "-version").CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("launcher: 读取 Java 版本失败（%s）: %w", exe, javaProbeError{cause: err})
	}
	return parseJavaMajor(string(out))
}

// parseJavaMajor 解析 `java -version` 输出中的大版本号：
// 形如 version "17.0.2" → 17；version "1.8.0_402" → 8（旧版命名 1.8 = 8）。
func parseJavaMajor(output string) (int, error) {
	const marker = `version "`
	i := strings.Index(output, marker)
	if i < 0 {
		return 0, fmt.Errorf("launcher: Java 版本输出无法解析: %.120s", output)
	}
	rest := output[i+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return 0, fmt.Errorf("launcher: Java 版本输出无法解析: %.120s", output)
	}
	version := rest[:end]
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0, fmt.Errorf("launcher: Java 版本串为空: %q", version)
	}
	major := parts[0]
	if major == "1" && len(parts) > 1 {
		major = parts[1]
	}
	switch major {
	case "8":
		return 8, nil
	case "17":
		return 17, nil
	case "21":
		return 21, nil
	case "22":
		return 22, nil
	}
	var n int
	for _, ch := range major {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("launcher: Java 大版本非数字: %q", major)
		}
		n = n*10 + int(ch-'0')
	}
	if n == 0 {
		return 0, fmt.Errorf("launcher: Java 大版本解析为 0: %q", version)
	}
	return n, nil
}

// CheckJavaMajor 校验 Java 可执行文件的大版本是否满足该 MC 版本的要求。
//
// 两类「确定不了」分开对待：
//   - 根本跑不起来（文件不存在、无法执行、非零退出）→ 返回错误。调用方
//     （ensureMirrorJava）据此走到 CDN 部署分支重新部署匹配的 JRE —— 这是
//     换机/迁移后 profile 里残留失效 JavaPath 的自愈路径。fail-open 会把它
//     变成「校验通过」，自愈永远不会触发，故障推迟到启动时以
//     "A Java Exception has occurred" 这类无法归因的对话框爆发（实测踩过）。
//   - 能跑但版本串解析不出 → 不拦截（保守：解析失败不该挡住可能可用的运行时），
//     由启动阶段验证兜底。
func CheckJavaMajor(javaPath, mcVersion string) error {
	major, err := JavaMajor(javaPath)
	if err != nil {
		var probeErr javaProbeError
		if errors.As(err, &probeErr) {
			return fmt.Errorf("launcher: Java 无法执行（%s）: %w", javaPath, err)
		}
		return nil // 能跑但版本串解析不出：保守放行，由启动阶段验证兜底
	}
	allowed := RequiredJavaMajors(mcVersion)
	for _, a := range allowed {
		if a == major {
			return nil
		}
	}
	pretty := make([]string, 0, len(allowed))
	for _, a := range allowed {
		pretty = append(pretty, strconv.Itoa(a))
	}
	return fmt.Errorf("launcher: Java 版本不匹配 —— MC %s 需要 Java %s，当前为 Java %d（%s）",
		mcVersion, strings.Join(pretty, "/"), major, javaPath)
}
