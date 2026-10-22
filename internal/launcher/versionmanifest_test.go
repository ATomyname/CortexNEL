package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadVersionManifestToleratesTrailingComma 网易基础包的清单带尾逗号，
// 必须能被解析出来。
//
// 这是 2026-09-22 一次真实故障的护栏：1.12.2.json 的 libraries 数组末尾有
// 一处 `,\r\n \r\n\t]`。RFC 8259 不允许尾逗号，encoding/json 整体解析失败，
// readVersionManifest 返回 hasManifest=false；调用方于是拿不到 libraries 与
// mainClass，classpath 退化成递归扫描全版本 libraries 目录，把 1.20.1/1.21 的
// 客户端 jar（Main.class 为 Java 17/21）混进 1.12.2 的 classpath，
// 最终 UnsupportedClassVersionError。
//
// 附带教训：PowerShell 的 ConvertFrom-Json 宽容接受尾逗号，用它做前置检查
// 会误判"清单正常"——校验 JSON 必须用与生产同等的严格解析器。
func TestReadVersionManifestToleratesTrailingComma(t *testing.T) {
	clientDir := t.TempDir()
	verDir := filepath.Join(clientDir, "versions", "1.12.2")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 形状取自真实 1.12.2.json：libraries 数组末尾尾逗号，minecraftArguments
	// 里也含逗号（不能被误剥）。
	raw := `{
  "id": "1.12.2",
  "mainClass": "net.minecraft.client.main.Main",
  "libraries": [
    {"name": "org.lwjgl.lwjgl:lwjgl:2.9.4-nightly-20150209",
     "rules": [{"action":"allow"},{"action":"disallow","os":{"name":"osx"}}]},
    {"name": "net.minecraft:launchwrapper:1.12"},
  ],
  "minecraftArguments": "--username ${auth_player_name}, --version ${version_name}"
}`
	if err := os.WriteFile(filepath.Join(verDir, "1.12.2.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	m, ok := readVersionManifest(clientDir, "1.12.2")
	if !ok {
		t.Fatal("带尾逗号的清单应能解析")
	}
	if len(m.Libraries) != 2 {
		t.Fatalf("libraries 应解析出 2 条，实得 %d", len(m.Libraries))
	}
	if m.MainClass != "net.minecraft.client.main.Main" {
		t.Fatalf("mainClass 解析错: %q", m.MainClass)
	}
	// 字符串里的逗号必须原样保留。
	if m.GameArgumentTemplate != "--username ${auth_player_name}, --version ${version_name}" {
		t.Fatalf("minecraftArguments 里的逗号被误剥: %q", m.GameArgumentTemplate)
	}
}

// TestStripTrailingCommasKeepsStringContents 只剥结构性的尾逗号，
// 字符串字面量（含转义引号）内容不动。
func TestStripTrailingCommasKeepsStringContents(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"数组尾逗号", `[1,2,]`, `[1,2]`},
		{"对象尾逗号", `{"a":1,}`, `{"a":1}`},
		{"嵌套尾逗号", `{"a":[1,],}`, `{"a":[1]}`},
		{"逗号后换行空白", "[\n1,\n\t]", "[\n1\n\t]"},
		{"正常逗号保留", `[1,2]`, `[1,2]`},
		{"字符串内逗号", `["a,b",]`, `["a,b"]`},
		{"字符串内右括号", `["a]",]`, `["a]"]`},
		{"字符串内逗号加括号", `["a,]",]`, `["a,]"]`},
		{"转义引号", `["a\",]",]`, `["a\",]"]`},
		{"空输入", ``, ``},
		{"无逗号", `{"a":1}`, `{"a":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(stripTrailingCommas([]byte(c.in))); got != c.want {
				t.Fatalf("stripTrailingCommas(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestResolveClassPathUsesManifestLibrariesNotFullScan 清单可读时，
// classpath 必须只取该版本自己的 libraries，不得递归扫全版本目录。
//
// 这是上面那次故障的另一半护栏：libraries 目录是跨版本共享的
// （实测 387 个 jar 混放 1.8.9/1.12.2/1.20/1.21），全量扫描必然把其他版本的
// 客户端 jar 拖进来，Main 类因此被替换成 Java 17/21 编译的那份。
func TestResolveClassPathUsesManifestLibrariesNotFullScan(t *testing.T) {
	clientDir := t.TempDir()
	verDir := filepath.Join(clientDir, "versions", "1.12.2")
	if err := os.MkdirAll(verDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 清单：一条带 artifact.path 的库，一条只有 maven 坐标的库。
	raw := `{
  "id": "1.12.2",
  "mainClass": "net.minecraft.client.main.Main",
  "libraries": [
    {"name": "com.example:only:1.0",
     "downloads": {"artifact": {"path": "com/example/only/1.0/only-1.0.jar"}}},
    {"name": "org.other:coord:2.0"}
  ]
}`
	if err := os.WriteFile(filepath.Join(verDir, "1.12.2.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	libsDir := filepath.Join(clientDir, "libraries")
	// mustWrite（launch_test.go）不建父目录，这里先建好。
	for _, rel := range []string{
		filepath.Join("com", "example", "only", "1.0", "only-1.0.jar"),
		filepath.Join("org", "other", "coord", "2.0", "coord-2.0.jar"),
		filepath.Join("net", "minecraft", "client", "1.20.1-x", "client-1.20.1-x.jar"),
	} {
		mustMkdirAll(t, filepath.Join(libsDir, filepath.Dir(rel)))
	}
	mustWrite(t, filepath.Join(libsDir, "com", "example", "only", "1.0", "only-1.0.jar"), "x")
	mustWrite(t, filepath.Join(libsDir, "org", "other", "coord", "2.0", "coord-2.0.jar"), "x")
	// 一个不属于本清单的库：必须被排除。
	mustWrite(t, filepath.Join(libsDir, "net", "minecraft", "client", "1.20.1-x", "client-1.20.1-x.jar"), "x")

	manifest, ok := readVersionManifest(clientDir, "1.12.2")
	if !ok {
		t.Fatal("清单应可读")
	}
	paths := resolveClassPath(clientDir, "1.12.2", manifest, ok)

	// 只应有 manifest 声明的两条库。版本 jar 不存在，且已有库路径时
	// resolveClassPath 不追加核心 jar（那是"一个库都没有"时的兜底）。
	if len(paths) != 2 {
		t.Fatalf("classpath 应只含 manifest 的 2 条库，实得 %d: %v", len(paths), paths)
	}
	for _, p := range paths {
		if filepath.Base(p) == "client-1.20.1-x.jar" {
			t.Fatalf("classpath 混进了不属于本版本的 jar: %s", p)
		}
	}
}
