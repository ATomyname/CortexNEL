// 解压编排的判据：格式分派、越界条目处置、进度终值。
package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeZip 造一个 ZIP 夹具。entries 为「归档内路径 -> 内容」。
func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, body := range entries {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestExtractZipRoundTrip ZIP 解压落盘，内容一致。
func TestExtractZipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	writeZip(t, zipPath, map[string]string{"versions/1.20/1.20.json": "{}", "libraries/x.jar": "jar"})

	dest := filepath.Join(dir, "out")
	if err := Extract(ExtractRequest{Archive: zipPath, Dest: dest}); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"versions/1.20/1.20.json": "{}", "libraries/x.jar": "jar"} {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("读取 %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s 内容 = %q，期望 %q", rel, got, want)
		}
	}
	// 临时文件不能残留在落位目录里。
	if entries, err := os.ReadDir(dest); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), partSuffix) {
				t.Fatalf("落位后残留临时文件 %s", e.Name())
			}
		}
	}
}

// TestExtractRejectsUnknownMagic 未知魔数必须明确报错，而不是猜格式或静默产出空目录。
func TestExtractRejectsUnknownMagic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.bin")
	if err := os.WriteFile(path, []byte("not-an-archive-at-all"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Extract(ExtractRequest{Archive: path, Dest: filepath.Join(dir, "out")})
	if err == nil || !strings.Contains(err.Error(), "无法识别") {
		t.Fatalf("期望「无法识别」错误，实得 %v", err)
	}
}

// TestExtractZipSkipsPathEscape 归档内越界条目（../ 与绝对路径）不得写到 dest 之外。
// 这是本包最要紧的安全属性：归档内容来自远端，可能被影响。
//
// 同时要求整次解压判失败：调用方拿"解压成功"当安装标记的写入条件，越界条目被
// 拒绝就意味着这个包落不全，绝不能照常写标记把残缺安装判成完成。
func TestExtractZipSkipsPathEscape(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	writeZip(t, zipPath, map[string]string{
		"../escaped.txt":    "pwned",
		"ok.txt":            "fine",
		"/abs/escaped2.txt": "pwned2",
		"sub/../../up.txt":  "pwned3",
	})

	dest := filepath.Join(dir, "out")
	err := Extract(ExtractRequest{Archive: zipPath, Dest: dest})
	if err == nil || !strings.Contains(err.Error(), "无法安全落位") {
		t.Fatalf("期望「无法安全落位」错误，实得 %v", err)
	}
	// 合法条目必须解出来（能落位的不浪费）。
	if _, err := os.Stat(filepath.Join(dest, "ok.txt")); err != nil {
		t.Fatalf("合法条目未解出：%v", err)
	}
	// 越界条目必须落在 dest 内或根本不存在。
	for _, escaped := range []string{
		filepath.Join(dir, "escaped.txt"),
		filepath.Join(dir, "escaped2.txt"),
		filepath.Join(dir, "up.txt"),
		filepath.Join(dir, "abs", "escaped2.txt"),
	} {
		if _, err := os.Stat(escaped); err == nil {
			t.Fatalf("越界条目被写出到 %s", escaped)
		}
	}
}

// TestExtractReportsSkippedEntries 被放弃的条目必须留痕：
// 既转发给调用方的 OnSkip，也要进最终错误消息 —— 静默缺件只能等启动失败才暴露。
func TestExtractReportsSkippedEntries(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.zip")
	writeZip(t, zipPath, map[string]string{
		"../escaped.txt": "pwned",
		"good.txt":       "fine",
	})

	var reported []string
	err := Extract(ExtractRequest{
		Archive: zipPath,
		Dest:    filepath.Join(dir, "out"),
		OnSkip:  func(name, reason string) { reported = append(reported, name) },
	})
	if err == nil {
		t.Fatal("有条目被放弃时必须返回错误")
	}
	if len(reported) != 1 || reported[0] != "../escaped.txt" {
		t.Fatalf("OnSkip 收到 %v，期望恰好 ../escaped.txt", reported)
	}
	if !strings.Contains(err.Error(), "../escaped.txt") {
		t.Fatalf("错误消息未点名被放弃的条目: %v", err)
	}
}

// TestExtractRealDistributionArchives 用真实分发的归档验证解压全链路：
// 名字策略、声明大小核对、原子落位。严格化（拒绝条目判失败、大小不符判失败）的
// 回归风险就在"真实归档里有没有碰不出来的名字或错误声明"，必须拿真包跑一遍。
// 需要 NEXUS_ARCHIVE_PROBE_DIR 指向存放真实归档的目录；未设置时跳过 ——
// 常规测试不依赖外部文件与大流量。
func TestExtractRealDistributionArchives(t *testing.T) {
	dir := os.Getenv("NEXUS_ARCHIVE_PROBE_DIR")
	if dir == "" {
		t.Skip("需要 NEXUS_ARCHIVE_PROBE_DIR（真实分发归档目录）")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext != ".7z" && ext != ".zip" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			var skipped []string
			err := Extract(ExtractRequest{
				Archive: path,
				Dest:    t.TempDir(),
				OnSkip:  func(name, reason string) { skipped = append(skipped, name+"（"+reason+"）") },
			})
			if err != nil {
				t.Fatalf("真实归档解压失败: %v", err)
			}
			if len(skipped) > 0 {
				t.Fatalf("真实归档有条目被拒: %v", skipped)
			}
		})
	}
}

// patchZipDeclaredSize 把 ZIP 中央目录里第一个条目的未压缩大小改成 want，
// 制造"声明与实际不符"的归档：内容与 CRC 都是对的，只有声明错了。
func patchZipDeclaredSize(t *testing.T, path string, want uint32) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(raw, []byte("PK\x01\x02"))
	if i < 0 {
		t.Fatal("找不到中央目录头")
	}
	binary.LittleEndian.PutUint32(raw[i+24:i+28], want)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExtractOneRejectsSizeMismatch 直打落位层：声明 50、实际解出 100 必须报错，
// 且半份内容不落最终路径。
//
// 不能借道 zip 夹具单独验证这一道防线 —— 实测（zipdbg 对照实验）Go 的 zip 读端
// 自己就会拒掉声明与实际不符的条目；7z（bodgit）没有等价保证，全靠这里兜底。
func TestExtractOneRejectsSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.bin")
	_, err := extractOne(context.Background(), target, 50,
		func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(strings.Repeat("x", 100))), nil })
	if err == nil || !strings.Contains(err.Error(), "声明") {
		t.Fatalf("期望声明大小不符错误，实得 %v", err)
	}
	if _, statErr := os.Stat(target); statErr == nil {
		t.Fatal("大小不符的条目被落到了最终路径")
	}
}

// TestExtractDeclaredSizeMismatchNeverPlacesPartial 声明与实际不符（无论被 Go 的
// zip 读端还是本包的大小核对先发现）都必须报错，且坏条目不落最终路径。
// 解压流提前结束而 io.Copy 不报错时（7z 实现尤其可能），这个契约是"半包内容
// 被当成完整包写安装标记"的最后一道闸。
func TestExtractDeclaredSizeMismatchNeverPlacesPartial(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "short.zip")
	writeZip(t, zipPath, map[string]string{"a.bin": strings.Repeat("x", 100)})
	patchZipDeclaredSize(t, zipPath, 50)

	err := Extract(ExtractRequest{Archive: zipPath, Dest: filepath.Join(dir, "out")})
	if err == nil {
		t.Fatal("声明大小不符必须报错")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "out", "a.bin")); statErr == nil {
		t.Fatal("大小不符的条目被落到了最终路径")
	}
}

// TestExtractCleansUpPartialEntryOnCorruptData 条目中途解压失败时，半份内容不得
// 出现在最终路径 —— 落位必须原子（先写临时文件、成功才改名）。
// 直接 O_TRUNC 写最终路径会留下截断文件，或 CRC 不符但长度完整的文件，后者恰好
// 能通过"文件存在"这类判据（JRE 就是这样被毒化的）。
func TestExtractCleansUpPartialEntryOnCorruptData(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "corrupt.zip")
	writeZip(t, zipPath, map[string]string{"corrupt.bin": strings.Repeat("z", 4096)})

	// 只破坏条目的 deflate 数据本身（局部头 30 字节 + 文件名之后的前 8 字节）。
	// 不能多翻：64 字节会把数据描述符与中央目录一起毁掉，OpenReader 直接失败，
	// 测试会因"归档打不开"而假绿 —— 必须让故障发生在逐条目落位中途。
	raw, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	dataStart := 30 + len("corrupt.bin")
	copy(raw[dataStart:dataStart+8], bytes.Repeat([]byte{0xFF}, 8))
	if err := os.WriteFile(zipPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(dir, "out")
	if err := Extract(ExtractRequest{Archive: zipPath, Dest: dest}); err == nil {
		t.Fatal("损坏数据必须报错")
	}
	if _, statErr := os.Stat(filepath.Join(dest, "corrupt.bin")); statErr == nil {
		t.Fatal("半份内容被落到了最终路径")
	}
	if leftovers, readErr := os.ReadDir(dest); readErr == nil {
		for _, e := range leftovers {
			if strings.HasSuffix(e.Name(), partSuffix) {
				t.Fatalf("残留临时文件 %s", e.Name())
			}
		}
	}
}

// TestExtractZipProgress 进度按未压缩字节汇报，终值必须是 done=total。
func TestExtractZipProgress(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "p.zip")
	writeZip(t, zipPath, map[string]string{"a.bin": strings.Repeat("x", 100), "b.bin": strings.Repeat("y", 50)})

	var lastDone, lastTotal int64
	var calls int
	progress := func(done, total int64) {
		calls++
		lastDone, lastTotal = done, total
		if done < 0 || total < 0 || done > total {
			t.Fatalf("进度不合理：done=%d total=%d", done, total)
		}
	}
	if err := Extract(ExtractRequest{Archive: zipPath, Dest: filepath.Join(dir, "out"), Progress: progress}); err != nil {
		t.Fatal(err)
	}
	if calls == 0 {
		t.Fatal("没有任何进度回调")
	}
	if lastDone != lastTotal || lastTotal != 150 {
		t.Fatalf("终值 done=%d total=%d，期望 150/150", lastDone, lastTotal)
	}
}
