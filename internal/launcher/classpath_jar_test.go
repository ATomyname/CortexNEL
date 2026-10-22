package launcher

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResolveClassPathArgFallsBackToJarWhenTooLong 超长 classpath 必须改走
// classpath jar，-cp 只留一个短 jar 路径。
//
// 真实触发场景（2026-09-22 实测）：MC 1.12.2 的清单是旧格式（无 arguments.jvm
// 模板），classpath 走 libraries 全量展开得 39,561 字符，argv 总长 40,630，
// 超过 Windows CreateProcess 的 32,767 上限。Go 的 fork/exec 只回报含糊的
// "The filename or extension is too long"，很容易误判成 exe 路径有问题。
func TestResolveClassPathArgFallsBackToJarWhenTooLong(t *testing.T) {
	workDir := t.TempDir()
	// 造一段确定超限的 classpath（路径形状贴近真实 libraries 布局）。
	var paths []string
	for i := 0; len(joinClassPath(paths)) <= maxCommandLineForClassPath+1024; i++ {
		paths = append(paths, filepath.Join(workDir, "libraries", "com", "example",
			"some-quite-long-artifact-id", "1.0."+strconv.Itoa(i),
			"some-quite-long-artifact-id-1.0."+strconv.Itoa(i)+".jar"))
	}
	if len(paths) < 50 {
		t.Fatalf("夹具应造出足够多的段，实得 %d", len(paths))
	}

	jarPath, cpValue := resolveClassPathArg(paths, workDir)
	if jarPath == "" {
		t.Fatal("超长时应生成 classpath jar")
	}
	if cpValue != jarPath {
		t.Fatalf("-cp 应只剩 jar 路径，实得 %q", cpValue)
	}
	if len(cpValue) > 512 {
		t.Fatalf("-cp 取值仍过长（%d），失去规避意义", len(cpValue))
	}
	if filepath.Base(jarPath) != classPathJarName {
		t.Fatalf("jar 名 = %q，期望 %q", filepath.Base(jarPath), classPathJarName)
	}

	// 内容没变时复用同一文件（不重复写盘）。
	jar2, cp2 := resolveClassPathArg(paths, workDir)
	if jar2 != jarPath || cp2 != cpValue {
		t.Fatal("相同 classpath 应复用已生成的 jar")
	}
}

// TestResolveClassPathArgInlinesShortClassPath 短 classpath 直接内联，
// 不产生额外文件（多数现代版本走这条路）。
func TestResolveClassPathArgInlinesShortClassPath(t *testing.T) {
	workDir := t.TempDir()
	paths := []string{
		filepath.Join(workDir, "a.jar"),
		filepath.Join(workDir, "b.jar"),
	}
	jarPath, cpValue := resolveClassPathArg(paths, workDir)
	if jarPath != "" {
		t.Fatalf("短 classpath 不应生成 jar，实得 %q", jarPath)
	}
	if cpValue != joinClassPath(paths) {
		t.Fatalf("应原样内联，实得 %q", cpValue)
	}
	if _, err := os.Stat(filepath.Join(workDir, classPathJarName)); !os.IsNotExist(err) {
		t.Fatal("短 classpath 不应留下 jar 文件")
	}
}

// TestClassPathJarManifestFormat jar 清单必须符合规范：72 字节行宽、
// 续行以单空格开头、末尾空行，且 file URL 数与入参一致。
func TestClassPathJarManifestFormat(t *testing.T) {
	paths := []string{
		`C:\client\libraries\com\example\artifact\1.0\artifact-1.0.jar`,
		`C:\client\libraries\org\other\thing\2.3.4\thing-2.3.4.jar`,
	}
	jarPath, err := writeClassPathJar(paths, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("生成的应是合法 zip/jar: %v", err)
	}
	manifest := readZipEntry(t, zr, "META-INF/MANIFEST.MF")
	if !strings.Contains(manifest, "Class-Path: ") {
		t.Fatalf("清单缺少 Class-Path：%q", manifest)
	}
	for _, line := range strings.Split(manifest, "\r\n") {
		if len(line) > 72 {
			t.Fatalf("清单行超 72 字节（%d）：%q", len(line), line)
		}
	}
	// 末尾必须是空行，否则 JVM 解析不到 Class-Path。
	if !strings.HasSuffix(manifest, "\r\n\r\n") {
		t.Fatalf("清单须以空行结尾：%q", manifest)
	}
	joined := strings.ReplaceAll(manifest, "\r\n", "")
	if got := strings.Count(joined, "file:///"); got != len(paths) {
		t.Fatalf("file URL 数 = %d，期望 %d", got, len(paths))
	}
	if strings.Contains(joined, `\`) {
		t.Fatal("清单里不应残留反斜杠（JVM 会当转义字符）")
	}
}

// probeClassBytes 是最小探针类 demo.Probe 的 class 文件字节。
//
// 来源：Zulu 21 javac 编译 `package demo; public class Probe { public static void
// main(String[] a){} }`（260 字节，class file version 65.0）。内联而不是现场编译，
// 是为了让测试不依赖机器上有没有 javac。
//
// 版本 65 高于 Java 8 的上限 52，这正好用作判别器：类被找到时会报
// UnsupportedClassVersionError，没找到则报 ClassNotFoundException —— 两种错误
// 明确可区分，都能证明"Class-Path 有没有生效"。
var probeClassBytes = []byte{
	0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00, 0x00, 0x41, 0x00, 0x0F, 0x0A, 0x00, 0x02, 0x00, 0x03,
	0x07, 0x00, 0x04, 0x0C, 0x00, 0x05, 0x00, 0x06, 0x01, 0x00, 0x10, 0x6A, 0x61, 0x76, 0x61,
	0x2F, 0x6C, 0x61, 0x6E, 0x67, 0x2F, 0x4F, 0x62, 0x6A, 0x65, 0x63, 0x74, 0x01, 0x00, 0x06,
	0x3C, 0x69, 0x6E, 0x69, 0x74, 0x3E, 0x01, 0x00, 0x03, 0x28, 0x29, 0x56, 0x07, 0x00, 0x08,
	0x01, 0x00, 0x0A, 0x64, 0x65, 0x6D, 0x6F, 0x2F, 0x50, 0x72, 0x6F, 0x62, 0x65, 0x01, 0x00,
	0x04, 0x43, 0x6F, 0x64, 0x65, 0x01, 0x00, 0x0F, 0x4C, 0x69, 0x6E, 0x65, 0x4E, 0x75, 0x6D,
	0x62, 0x65, 0x72, 0x54, 0x61, 0x62, 0x6C, 0x65, 0x01, 0x00, 0x04, 0x6D, 0x61, 0x69, 0x6E,
	0x01, 0x00, 0x16, 0x28, 0x5B, 0x4C, 0x6A, 0x61, 0x76, 0x61, 0x2F, 0x6C, 0x61, 0x6E, 0x67,
	0x2F, 0x53, 0x74, 0x72, 0x69, 0x6E, 0x67, 0x3B, 0x29, 0x56, 0x01, 0x00, 0x0A, 0x53, 0x6F,
	0x75, 0x72, 0x63, 0x65, 0x46, 0x69, 0x6C, 0x65, 0x01, 0x00, 0x0A, 0x50, 0x72, 0x6F, 0x62,
	0x65, 0x2E, 0x6A, 0x61, 0x76, 0x61, 0x00, 0x21, 0x00, 0x07, 0x00, 0x02, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x05, 0x00, 0x06, 0x00, 0x01, 0x00, 0x09, 0x00, 0x00,
	0x00, 0x1D, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x05, 0x2A, 0xB7, 0x00, 0x01, 0xB1,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x0A, 0x00, 0x00, 0x00, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x00, 0x09, 0x00, 0x0B, 0x00, 0x0C, 0x00, 0x01, 0x00, 0x09, 0x00, 0x00, 0x00, 0x19,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0xB1, 0x00, 0x00, 0x00, 0x01, 0x00, 0x0A,
	0x00, 0x00, 0x00, 0x06, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x0D, 0x00,
	0x00, 0x00, 0x02, 0x00, 0x0E,
}

// TestClassPathJarIsHonoredByRealJvm 端到端：本机真实 java 必须能靠
// Class-Path 找到只写在清单里、不在 -cp 上的类。
//
// 这是"jar 真能被 JVM 识别"的唯一可靠证据——清单格式对不对，看字节看不出来。
func TestClassPathJarIsHonoredByRealJvm(t *testing.T) {
	java, err := DetectJavaForVersion("1.17")
	if err != nil || java == "" {
		t.Skip("本机探测不到 Java")
	}
	dir := t.TempDir()

	// 库 jar：装着探针类。它绝不出现在 -cp 里，只能靠 Class-Path 被找到。
	libJar := filepath.Join(dir, "lib.jar")
	if err := writeJarWithEntry(libJar, "demo/Probe.class", probeClassBytes, nil); err != nil {
		t.Fatal(err)
	}

	// 把 classpath 撑到超限，迫使走 jar 路径（用真实长度的路径形状）。
	var paths []string
	for i := 0; len(joinClassPath(paths)) <= maxCommandLineForClassPath+1024; i++ {
		paths = append(paths, filepath.Join(dir, "libraries", "com", "example",
			"artifact", strconv.Itoa(i), "artifact-"+strconv.Itoa(i)+".jar"))
	}
	paths = append(paths, libJar)

	cpJar, cpValue := resolveClassPathArg(paths, dir)
	if cpJar == "" {
		t.Fatal("超长 classpath 应改走 jar")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, java, "-cp", cpValue, "demo.Probe").CombinedOutput()
	combined := string(out)

	switch {
	case strings.Contains(combined, "UnsupportedClassVersionError"):
		// 类被找到并加载，仅因版本太新而拒 —— 正是 Class-Path 生效的证据。
	case strings.Contains(combined, "ClassNotFoundException"),
		strings.Contains(combined, "Could not find or load main class"):
		t.Fatalf("JVM 未按 Class-Path 找到 demo.Probe，jar 未生效：%s", combined)
	default:
		// 其他输出也说明类已加载（例如真的跑起来了）。
		if combined == "" {
			t.Fatalf("JVM 无任何输出，无法判定")
		}
	}
}

// writeJarWithEntry 写一个只含单个条目的 jar（manifest 可选）。
func writeJarWithEntry(path, entryName string, content []byte, manifest []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if manifest != nil {
		w, err := zw.Create("META-INF/MANIFEST.MF")
		if err != nil {
			return err
		}
		if _, err := w.Write(manifest); err != nil {
			return err
		}
	}
	w, err := zw.Create(entryName)
	if err != nil {
		return err
	}
	if _, err := w.Write(content); err != nil {
		return err
	}
	return zw.Close()
}

// readZipEntry 读出 zip 内某个条目的文本内容。
func readZipEntry(t *testing.T, zr *zip.Reader, name string) string {
	t.Helper()
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		buf := new(bytes.Buffer)
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	t.Fatalf("zip 内缺少条目 %s", name)
	return ""
}
