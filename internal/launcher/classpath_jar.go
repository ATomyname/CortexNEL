// classpath_jar.go — 用 Class-Path 清单属性承载超长 classpath。
//
// 为什么需要：Windows CreateProcess 的命令行上限是 32,767 字符，而
// `-cp` 全部展开成绝对路径后轻松超过。实测（2026-09-22，MC 1.12.2）：
// libraries 全量展开得 39,561 字符，argv 总长 40,630 —— Go 的 fork/exec
// 只回报含糊的 "The filename or extension is too long"，很容易误判成
// exe 路径有问题（实际那个路径只有 47 字符）。
//
// 为什么用 Class-Path 而不是裁剪库列表：裁剪会漏库，直接后果是
// ClassNotFoundException，而且漏哪支取决于版本，无法静态推断。
// Class-Path 是 JVM 标准机制（jar 清单的相对 URL 列表），把长度从
// 命令行转移到文件里，语义与 -cp 完全一致。
//
// 备选的 @argfile（JVM 9+）不可用：MC 1.12.2 那批老版本跑在 Java 8 上，
// 不支持 @argfile。
package launcher

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// maxCommandLineForClassPath 是 -cp 直接进命令行的安全长度阈值。
// 留出余量给其余 argv（-D 参数、主类、userProperties 等）：实测那些
// 合计约 1KB，32,767 减去后取 24KB 作为 -cp 的上限。
const maxCommandLineForClassPath = 24 * 1024

// classPathJarName 是生成的 classpath jar 文件名（固定，便于复用）。
const classPathJarName = "nexus-classpath.jar"

// resolveClassPathArg 决定 -cp 以哪种形式交付，返回 (jar 路径或空, -cp 取值)。
//
// 短 campus 直接内联（不产生额外文件）；超长时生成 classpath jar，
// 命令行只留 `nexus-classpath.jar` 一个短路径。
func resolveClassPathArg(paths []string, workDir string) (jarPath string, cpValue string) {
	joined := joinClassPath(paths)
	if len(joined) <= maxCommandLineForClassPath {
		return "", joined
	}
	// 生成 jar 失败时退回到内联：让 JVM 因命令行超长而失败，
	// 错误可读，好过静默丢一半库。
	jar, err := writeClassPathJar(paths, workDir)
	if err != nil || jar == "" {
		return "", joined
	}
	return jar, jar
}

// writeClassPathJar 把 classpath 写进一个 jar 的 Class-Path 清单属性。
//
// 落点与 workDir 同级（沙盒的 .minecraft 下）：jar 清单的相对 URL 相对
// **jar 自身所在目录**解析，而库路径是相对客户端根的绝对路径，所以这里
// 全部写成绝对 file:/// URL —— 相对推导在不同沙盒深度下会错。
func writeClassPathJar(paths []string, workDir string) (string, error) {
	if workDir == "" {
		return "", os.ErrInvalid
	}
	jarPath := filepath.Join(workDir, classPathJarName)

	// 内容没变就复用：避免每次启动都重写（也免去无谓的磁盘磨损）。
	manifest := buildClassPathManifest(paths)
	if existing, err := os.ReadFile(jarPath); err == nil && bytes.Equal(existing, manifest) {
		return jarPath, nil
	}
	if err := os.WriteFile(jarPath, manifest, 0o644); err != nil {
		return "", err
	}
	return jarPath, nil
}

// buildClassPathManifest 构造只含 META-INF/MANIFEST.MF 的最小 jar。
//
// 手工拼而不是用 archive/zip：清单有两条硬性格式要求（行宽 ≤72 字节、
// 每行以单个空格续行），archive/zip 不知道这些语义。末尾必须是空行，
// 否则 JVM 解析不到 Class-Path。
func buildClassPathManifest(paths []string) []byte {
	var body bytes.Buffer
	body.WriteString("Manifest-Version: 1.0\r\n")
	body.WriteString("Created-By: nexus\r\n")

	var cp bytes.Buffer
	for _, p := range paths {
		u := fileURLForClassPath(p)
		if u == "" {
			continue
		}
		if cp.Len() > 0 {
			cp.WriteString(" ")
		}
		cp.WriteString(u)
	}
	if cp.Len() > 0 {
		writeManifestEntry(&body, "Class-Path", cp.String())
	}
	body.WriteString("\r\n")

	// 拼成 zip：一个 store（不压缩）条目即可，jar 对内容没有额外要求。
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, err := zw.CreateHeader(&zip.FileHeader{
		Name:   "META-INF/MANIFEST.MF",
		Method: zip.Store,
	})
	if err != nil {
		return nil
	}
	if _, err := fw.Write(body.Bytes()); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

// writeManifestEntry 按 jar 清单规范写一条：行宽 ≤72 字节，续行以单空格开头。
func writeManifestEntry(buf *bytes.Buffer, name, value string) {
	buf.WriteString(name)
	buf.WriteString(": ")
	lineLen := len(name) + 2
	for _, r := range value {
		buf.WriteRune(r)
		lineLen++
		// 72 字节是字节数不是字符数；URL 全是 ASCII，按字节算即按字符算。
		if lineLen >= 72 {
			buf.WriteString("\r\n ")
			lineLen = 1
		}
	}
	buf.WriteString("\r\n")
}

// fileURLForClassPath 把 Windows 路径转成 jar 清单可用的 file URL。
// 反斜杠必须转成斜杠，否则 JVM 把它当转义字符。
func fileURLForClassPath(p string) string {
	if p == "" {
		return ""
	}
	slashed := filepath.ToSlash(p)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + slashed
}

// classPathFingerprint 供测试与日志判断 classpath 内容是否变化。
func classPathFingerprint(paths []string) string {
	sum := sha256.Sum256([]byte(strings.Join(paths, "\n")))
	return hex.EncodeToString(sum[:])
}
