// Classpath 与 natives 目录定位。
package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// classPathSeparator 是 JVM -cp 的平台分隔符。
func classPathSeparator() string {
	if runtime.GOOS == "windows" {
		return ";"
	}
	return ":"
}

// resolveClassPath 计算 -cp 列表。
//
// 优先用清单 JVM 模板自带的 -cp：实测它就是官方启动器实际传入的那一份
// （1.20 → 94 段、1.21.8 → 113 段，与官方 java_args 完全一致）。
// 清单的 libraries 列表**不等于** classpath：1.20 的 libraries 缺 fmlloader /
// modlauncher 等 21 支，1.21.8 的 libraries 反而多出 3 支模块路径 JAR
// （多出来的那 3 支若进 -cp，securejarhandler 里的 ModuleClassLoader 会被
// app 类加载器加载，首次 LWJGL 原生调用即抛 IncompatibleClassChangeError）。
// 模板没有 -cp 时才回落到 libraries 展开 / 扫描目录。
func resolveClassPath(clientDir, version string, manifest versionManifest, hasManifest bool) []string {
	if hasManifest {
		if fromTemplate := classPathFromTemplate(manifest.jvmArgumentTemplate(), clientDir); len(fromTemplate) > 0 {
			return fromTemplate
		}
	}

	libsDir := filepath.Join(clientDir, "libraries")
	var paths []string
	if hasManifest {
		paths = manifestLibraryPaths(libsDir, manifest)
	}
	if len(paths) == 0 {
		paths = scanLibraryJars(libsDir)
	}

	coreJar := filepath.Join(versionDir(clientDir, version), version+".jar")
	if _, err := os.Stat(coreJar); err == nil {
		paths = append(paths, coreJar)
	} else if len(paths) == 0 {
		// 一个都没找到时仍然给出核心 JAR，让 JVM 报出可读的缺文件错误，
		// 而不是收到空 -cp 后报 ClassNotFound。
		paths = append(paths, coreJar)
	}
	return paths
}

// classPathFromTemplate 取出清单 JVM 模板里 -cp / -classpath 的条目并绝对化。
//
// 模板里的路径是相对客户端根的（如 `libraries\net\...`），而 nexus 的进程工作目录是
// 按服沙盒，因此必须逐段拼到客户端根下，否则 JVM 在沙盒里找不到这些 JAR。
func classPathFromTemplate(template, clientDir string) []string {
	tokens := tokenizeArguments(template)
	for i, tok := range tokens {
		if (tok != "-cp" && tok != "-classpath") || i+1 >= len(tokens) {
			continue
		}
		var out []string
		for _, seg := range strings.Split(tokens[i+1], classPathSeparator()) {
			if seg == "" {
				continue
			}
			if clientDir != "" && !filepath.IsAbs(seg) {
				seg = filepath.Join(clientDir, seg)
			}
			out = append(out, seg)
		}
		return out
	}
	return nil
}

// manifestLibraryPaths 把 libraries 展开成本地 JAR 路径。
// downloads.artifact.path 优先；只有 Maven 坐标时按坐标推路径。
func manifestLibraryPaths(libsDir string, manifest versionManifest) []string {
	out := make([]string, 0, len(manifest.Libraries))
	for _, lib := range manifest.Libraries {
		if !isRuleAllowed(lib.Rules) {
			continue
		}
		if lib.Downloads != nil && lib.Downloads.Artifact != nil && lib.Downloads.Artifact.Path != "" {
			out = append(out, filepath.Join(libsDir, filepath.FromSlash(lib.Downloads.Artifact.Path)))
			continue
		}
		if rel, ok := mavenRelativePath(lib.Name); ok {
			out = append(out, filepath.Join(libsDir, rel))
		}
	}
	return out
}

// mavenRelativePath 把 groupId:artifactId:version[:classifier] 转成相对路径。
func mavenRelativePath(coordinate string) (string, bool) {
	parts := strings.Split(coordinate, ":")
	if len(parts) < 3 {
		return "", false
	}
	groupPath := strings.ReplaceAll(parts[0], ".", string(filepath.Separator))
	artifactID, version := parts[1], parts[2]
	fileName := fmt.Sprintf("%s-%s.jar", artifactID, version)
	if len(parts) >= 4 {
		fileName = fmt.Sprintf("%s-%s-%s.jar", artifactID, version, parts[3])
	}
	return filepath.Join(groupPath, artifactID, version, fileName), true
}

// scanLibraryJars 递归扫描 libraries 目录下的全部 JAR（清单不可用时的兜底）。
func scanLibraryJars(libsDir string) []string {
	var out []string
	_ = filepath.WalkDir(libsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".jar") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// nativesDirCandidates 是 natives 目录的候选名，按优先级排列。
// 首项与参考实现 Fantnel 的固定路径 versions/<ver>/natives 一致，
// 其余是不同网易/第三方启动器留下的历史命名。
func nativesDirCandidates(clientDir, version string) []string {
	base := versionDir(clientDir, version)
	return []string{
		filepath.Join(base, "natives"),
		filepath.Join(base, version+"-natives"),
		filepath.Join(base, "natives-windows-x86_64"),
		filepath.Join(base, "natives-windows"),
	}
}

// resolveNativesDir 返回实际存在的 natives 目录；全都不存在时返回首选路径，
// 让 JVM 自己报缺库错误而不是静默拼出空 java.library.path。
func resolveNativesDir(clientDir, version string) string {
	candidates := nativesDirCandidates(clientDir, version)
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}
	return candidates[0]
}
