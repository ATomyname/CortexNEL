// 下载的判据：进度汇报、状态码拒绝、取消响应。
package archive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// TestDownloadReportsProgressAndWritesFile 下载落盘并汇报进度。
// 分两种：服务器给出 Content-Length（CDN 常态）时 total 必须等于它；
// 未给出时 total 为负（未知），调用方据此展示不确定进度而不是假的百分比。
func TestDownloadReportsProgressAndWritesFile(t *testing.T) {
	body := strings.Repeat("z", 4096)
	cases := []struct {
		name       string
		withLength bool
	}{
		{"已知长度", true},
		{"未知长度", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.withLength {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				}
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			dst := filepath.Join(t.TempDir(), "nested", "out.bin")
			var lastDone, lastTotal int64
			err := Download(context.Background(), srv.Client(), srv.URL, dst, func(done, total int64) {
				lastDone, lastTotal = done, total
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(body) {
				t.Fatalf("下载字节 = %d，期望 %d", len(got), len(body))
			}
			if lastDone != int64(len(body)) {
				t.Fatalf("进度 done = %d，期望 %d", lastDone, len(body))
			}
			if c.withLength && lastTotal != int64(len(body)) {
				t.Fatalf("进度 total = %d，期望 %d", lastTotal, len(body))
			}
			if !c.withLength && lastTotal >= 0 {
				t.Fatalf("未给 Content-Length 时 total 应为未知（负数），实得 %d", lastTotal)
			}
		})
	}
}

// TestDownloadRejectsNon200 非 200 必须报错，不能把错误页当包落盘。
func TestDownloadRejectsNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := Download(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "o.bin"), nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("期望 HTTP 404 错误，实得 %v", err)
	}
}

// TestDownloadHonoursContextCancel 取消必须中断下载并浮出 ctx 错误。
func TestDownloadHonoursContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Download(ctx, srv.Client(), srv.URL, filepath.Join(t.TempDir(), "o.bin"), nil)
	if err == nil {
		t.Fatal("取消后应返回错误")
	}
}

// TestDownloadRetriesTransientFailure 传输层故障（连接被中途掐断）按上限重试：
// 第一次请求被服务器直接断开，第二次给出完整内容 —— 最终必须成功落盘。
// 这正是弱网下 197MB 级 JRE / 3.4GB 级基岩整包的真实故障形态。
func TestDownloadRetriesTransientFailure(t *testing.T) {
	var requests int32
	body := strings.Repeat("z", 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&requests, 1) == 1 {
			// 劫持连接后直接关闭：客户端在拿到任何响应前遇到 EOF。
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := Download(context.Background(), srv.Client(), srv.URL, dst, nil); err != nil {
		t.Fatalf("瞬时故障重试后应成功: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(body) {
		t.Fatalf("重试后落盘字节 = %d，期望 %d", len(got), len(body))
	}
}

// TestDownloadDoesNotRetryStatusErrors 非 200 是确定性失败：重试没有意义，
// 必须恰好请求一次就返回（对 404 重试只会把失败时间拖长三倍）。
func TestDownloadDoesNotRetryStatusErrors(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	err := Download(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "o.bin"), nil)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("期望 HTTP 404 错误，实得 %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("状态码错误不应重试，实得 %d 次请求", got)
	}
}

// TestDownloadRetriesExhaustedReportsAttempts 重试耗尽必须如实上浮，
// 错误点名尝试次数 —— 排查时要知道这不是单次偶发。
func TestDownloadRetriesExhaustedReportsAttempts(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	err := Download(context.Background(), srv.Client(), srv.URL, filepath.Join(t.TempDir(), "o.bin"), nil)
	if err == nil {
		t.Fatal("重试耗尽应返回错误")
	}
	if !strings.Contains(err.Error(), "重试") {
		t.Fatalf("错误应说明重试已耗尽: %v", err)
	}
	if got := atomic.LoadInt32(&requests); got != 3 {
		t.Fatalf("应恰好尝试 3 次，实得 %d", got)
	}
}

// TestDownloadContentLengthMismatchFails 服务器声明 100 字节却只发 5 字节：
// 传输层的定长读取器会报 unexpected EOF（可重试的瞬时故障），耗尽后必须
// 返回错误 —— 绝不能把半份内容改名落盘。
func TestDownloadContentLengthMismatchFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "o.bin")
	if err := Download(context.Background(), srv.Client(), srv.URL, dst, nil); err == nil {
		t.Fatal("字节数不足必须报错")
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Fatal("半份内容被落到了最终路径")
	}
}

// TestDownloadLeavesNoPartOnFailure 失败后目录里不得残留 .part 临时文件 ——
// 半份文件绝不顶着任何名字留在调用方目录里。
func TestDownloadLeavesNoPartOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	dir := t.TempDir()
	err := Download(context.Background(), srv.Client(), srv.URL, filepath.Join(dir, "o.bin"), nil)
	if err == nil {
		t.Fatal("403 应返回错误")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("失败后目录应干净，实得 %v", names)
	}
}
