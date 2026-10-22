package launcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestFetchSdkRuntimePackDownloadsOnlyMissing 补件只补缺失项：本地已有的不重复下载，
// 也不被下载结果覆盖 —— gray 组件有多个加壳构建，不能把已验证能跑的本地副本换掉。
func TestFetchSdkRuntimePackDownloadsOnlyMissing(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, filepath.Base(r.URL.Path))
		_, _ = w.Write([]byte("cdn:" + filepath.Base(r.URL.Path)))
	}))
	defer server.Close()

	clientDir := t.TempDir()
	present := sdkRuntimeManifest[0]
	writeSdkFixture(t, filepath.Join(clientDir, sdkRuntimePackDirName), []string{present})

	report := fetchSdkRuntimePack(context.Background(), clientDir, server.URL)
	if report.PackDir != filepath.Join(clientDir, sdkRuntimePackDirName) {
		t.Fatalf("PackDir = %q", report.PackDir)
	}
	if report.Downloaded != len(sdkRuntimeManifest)-1 {
		t.Fatalf("补件数 = %d，期望 %d", report.Downloaded, len(sdkRuntimeManifest)-1)
	}
	if len(report.Failed) != 0 {
		t.Fatalf("不应有失败项，实得 %v", report.Failed)
	}
	if len(requested) != len(sdkRuntimeManifest)-1 {
		t.Fatalf("请求了 %d 次，期望 %d（已有的不应重复下载）", len(requested), len(sdkRuntimeManifest)-1)
	}
	for _, name := range requested {
		if name == present {
			t.Fatalf("已存在的 %s 不应被重新下载", name)
		}
	}

	// 已有副本内容必须保持不变。
	if got := readFileForTest(t, filepath.Join(clientDir, sdkRuntimePackDirName, present)); got != "pack:"+present {
		t.Fatalf("已有副本被覆盖，内容 = %q", got)
	}

	// 幂等：齐全后再跑一次不应有请求与补件。
	requested = nil
	if again := fetchSdkRuntimePack(context.Background(), clientDir, server.URL); again.Downloaded != 0 || len(again.Failed) != 0 {
		t.Fatalf("齐全后不应再补件，实得 %+v", again)
	}
	if len(requested) != 0 {
		t.Fatalf("齐全后不应再发请求，实得 %v", requested)
	}

	// 补下来的包目录必须能被识别为齐全来源，落位才有取件点。
	if src := ResolveSdkRuntimeSource(clientDir); src != filepath.Join(clientDir, sdkRuntimePackDirName) {
		t.Fatalf("补齐后应成为取件来源，实得 %q", src)
	}
}

// TestFetchSdkRuntimePackLeavesNoPartialOnFailure 下载失败不得留半份文件。
//
// 半份 dll 会被后续落位当成完整件（只看存在且非空），客户端就会在 System.load 处
// 崩 —— 所以失败必须既没有最终文件名、也没有 .part 残留。
func TestFetchSdkRuntimePackLeavesNoPartialOnFailure(t *testing.T) {
	var served int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if name == sdkRuntimeClientChecked[1] {
			// 首个清单项成功、第二个失败：验证失败项零残留且不影响其余项。
			w.Header().Set("Content-Length", "100")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(make([]byte, 10)) // 截断：body 短于 Content-Length
			return
		}
		atomic.AddInt32(&served, 1)
		_, _ = w.Write([]byte("cdn:" + name))
	}))
	defer server.Close()

	clientDir := t.TempDir()
	report := fetchSdkRuntimePack(context.Background(), clientDir, server.URL)
	if len(report.Failed) != 1 || report.Failed[0] != sdkRuntimeClientChecked[1] {
		t.Fatalf("失败项 = %v，期望只有 %s", report.Failed, sdkRuntimeClientChecked[1])
	}

	packDir := filepath.Join(clientDir, sdkRuntimePackDirName)
	if _, err := os.Stat(filepath.Join(packDir, sdkRuntimeClientChecked[1])); err == nil {
		t.Fatal("失败项不得留下最终文件")
	}
	entries, err := os.ReadDir(packDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".part" || filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("残留临时文件 %s：半份 dll 会被当成完整件", e.Name())
		}
	}
	// 其余项不受影响，包目录仍可被识别（自愈/落位只看齐全性）。
	if int(atomic.LoadInt32(&served)) != len(sdkRuntimeManifest)-1 {
		t.Fatalf("成功项数 = %d，期望 %d", served, len(sdkRuntimeManifest)-1)
	}
}

// TestFetchSdkRuntimePackSafeOnEmptyArgs 空客户端根与建目录失败不得 panic。
func TestFetchSdkRuntimePackSafeOnEmptyArgs(t *testing.T) {
	if got := fetchSdkRuntimePack(context.Background(), "", "http://127.0.0.1:0"); got.PackDir != "" || got.Downloaded != 0 {
		t.Fatalf("空客户端根应返回零值，实得 %+v", got)
	}
	// 客户端根被一个同名文件占住：包目录建不出来，全部报缺而不是静默成功。
	clientDir := t.TempDir()
	blocked := filepath.Join(clientDir, sdkRuntimePackDirName)
	if err := os.WriteFile(blocked, []byte("i-am-a-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := fetchSdkRuntimePack(context.Background(), clientDir, "http://127.0.0.1:0")
	if len(got.Failed) != len(sdkRuntimeManifest) {
		t.Fatalf("包目录建不出来应全报缺，实得 %d 项", len(got.Failed))
	}
}

// readFileForTest 读文件内容，读不到即失败。
func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
