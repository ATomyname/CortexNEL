package ecpack

// server_test.go — 职责: 本地分发服务的登记与内容一致性（交付 = 服务端原包原样）。

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// minimalPack 造一份最小资源包文件，返回路径与内容。
func minimalPack(t *testing.T) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("pack.mcmeta")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(`{"pack":{"pack_format":84}}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "server-pack.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, buf.Bytes()
}

// TestServerPublishesMatchingHash 登记后 URL 可拉取，分发内容与登记包逐字节一致，
// sha1 头与实测内容吻合 —— 原样交付的判据。
func TestServerPublishesMatchingHash(t *testing.T) {
	packPath, content := minimalPack(t)
	sum := sha1.Sum(content)
	want := hex.EncodeToString(sum[:])

	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	url, err := srv.Publish(packPath, want)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	gotURL, gotHash, ok := srv.PackURL()
	if !ok || gotURL != url || gotHash != want {
		t.Fatalf("PackURL = %q %q %v，应与 Publish 登记一致", gotURL, gotHash, ok)
	}

	resp, err := http.Get(url) //nolint:gosec // 本机回环
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, content) {
		t.Fatalf("分发内容与登记包不一致（必须原样）：实得 %d 字节，应为 %d 字节", len(body), len(content))
	}
	if got := resp.Header.Get("X-Content-SHA1"); got != want {
		t.Fatalf("X-Content-SHA1 = %q，应为 %q", got, want)
	}
}

// TestServerUnpublishedPackURLFalse 未登记资源包时 PackURL 恒 false（relay 原样透传、不介入）。
func TestServerUnpublishedPackURLFalse(t *testing.T) {
	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if _, _, ok := srv.PackURL(); ok {
		t.Fatal("未登记资源包时 PackURL 应为 false")
	}
}

// TestServerPublishRejectsDirectory Publish 不接受目录路径（防呆）。
func TestServerPublishRejectsDirectory(t *testing.T) {
	srv, err := Start("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })

	if _, err := srv.Publish(t.TempDir(), ""); err == nil {
		t.Fatal("目录路径应被拒绝")
	}
}
