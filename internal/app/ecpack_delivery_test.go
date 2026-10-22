package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nexus/internal/telemetry"
)

func newTestLogger(t *testing.T) *telemetry.Logger {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

// serverPackZip 造一份「服务端进服下发的资源包」样例（含 select 物品定义与 bedrock 原件条目）：
// 原样交付的判据就是这些条目必须被原封不动地分发出去。
func serverPackZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	entries := map[string]string{
		"pack.mcmeta": `{"pack":{"description":"ViaBedrock Resource Pack","pack_format":84}}`,
		"assets/viabedrock/items/item_textures/all_games.json":    `{"model":{"type":"minecraft:select","property":"minecraft:custom_model_data","cases":[{"when":"default","model":{"type":"minecraft:model","model":"viabedrock:item_textures/all_games/0"}}]}}`,
		"assets/viabedrock/models/item_textures/all_games/0.json": `{"parent":"minecraft:item/generated","textures":{"layer0":"viabedrock:item/item_textures/items/all_games"}}`,
		"bedrock/7e7afa7a-397e-424d-b7aa-12f7e09ef1f0.mcpack":     "mcpack-bytes",
	}
	for name, body := range entries {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestECPackDeliveryFromServerPush 服务端推送 URL 后：下载 → 原样分发就绪，
// 分发内容与服务端下发的包逐字节一致（不改写、不重打包）。
func TestECPackDeliveryFromServerPush(t *testing.T) {
	log := newTestLogger(t)
	packBytes := serverPackZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(packBytes)
	}))
	defer srv.Close()

	cache := t.TempDir()
	d := newECPackDelivery(func() (string, error) { return cache, nil }, log)
	t.Cleanup(d.Close)

	// 没收到推送前不介入。
	if _, _, ok := d.PackURL(); ok {
		t.Fatal("无服务端推送时不应就绪")
	}

	d.OnServerPackURL(srv.URL + "/pack.zip")

	url, sum, ok := d.PackURL()
	if !ok {
		t.Fatalf("收到推送后应就绪（url=%q sum=%q）", url, sum)
	}
	if url == "" || len(sum) != 40 {
		t.Fatalf("url=%q sha1=%q", url, sum)
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
	if !bytes.Equal(body, packBytes) {
		t.Fatalf("分发内容与服务端包不一致（必须原样）：实得 %d 字节，应为 %d 字节", len(body), len(packBytes))
	}
	// 推送交付后要落已知包记录：服务端下次没推时的兜底源。
	raw, err := os.ReadFile(filepath.Join(cache, packManifestName))
	if err != nil {
		t.Fatalf("推送交付后应写入已知包记录: %v", err)
	}
	packSum := sha1.Sum(packBytes)
	if !bytes.Contains(raw, []byte(hex.EncodeToString(packSum[:]))) {
		t.Fatalf("已知包记录缺少包 sha1：%s", raw)
	}
}

// TestECPackDeliveryFallbackInjectsCachedPack 服务端未推送时用本地缓存的已知包兜底注入：
// 缓存里躺着旧名 server-pack.7z（内容同为 zip）也必须直接可用，交付内容原样。
func TestECPackDeliveryFallbackInjectsCachedPack(t *testing.T) {
	log := newTestLogger(t)
	packBytes := serverPackZip(t)
	cache := t.TempDir()
	if err := os.WriteFile(filepath.Join(cache, "server-pack.7z"), packBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	d := newECPackDelivery(func() (string, error) { return cache, nil }, log)
	t.Cleanup(d.Close)

	url, sum, ok := d.PackURL()
	if !ok {
		t.Fatal("有本地已知包时应就绪（服务端未推送也应兜底注入）")
	}
	if url == "" || len(sum) != 40 {
		t.Fatalf("url=%q sha1=%q", url, sum)
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
	if !bytes.Equal(body, packBytes) {
		t.Fatalf("兜底分发内容与缓存包不一致（必须原样）：实得 %d 字节，应为 %d 字节", len(body), len(packBytes))
	}
}

// TestECPackDeliveryFallbackFromManifest 本地缓存缺失时按已知包记录的内容寻址 URL 拉取。
func TestECPackDeliveryFallbackFromManifest(t *testing.T) {
	log := newTestLogger(t)
	packBytes := serverPackZip(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(packBytes)
	}))
	defer srv.Close()

	cache := t.TempDir()
	manifest := fmt.Sprintf(`{"url":%q,"sha1":"x","size":%d}`, srv.URL+"/pack.zip", len(packBytes))
	if err := os.WriteFile(filepath.Join(cache, packManifestName), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	d := newECPackDelivery(func() (string, error) { return cache, nil }, log)
	t.Cleanup(d.Close)

	url, _, ok := d.PackURL()
	if !ok {
		t.Fatal("有已知包记录时应就绪（按内容寻址 URL 拉取兜底）")
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
	if !bytes.Equal(body, packBytes) {
		t.Fatalf("按记录拉取的分发内容不一致（必须原样）：实得 %d 字节，应为 %d 字节", len(body), len(packBytes))
	}
}

// TestECPackDeliveryNoURLStaysInactive 无服务端推送且无已知包时 PackURL 恒为 false
// （不介入），重复调用不产生异常。
func TestECPackDeliveryNoURLStaysInactive(t *testing.T) {
	log := newTestLogger(t)
	d := newECPackDelivery(func() (string, error) { return t.TempDir(), nil }, log)
	t.Cleanup(d.Close)
	for i := 0; i < 5; i++ {
		if _, _, ok := d.PackURL(); ok {
			t.Fatal("无推送时不应就绪")
		}
	}
}

// TestECPackDeliveryBadServerURLFailsGracefully 推送 URL 不可达时保持不介入且可重试。
func TestECPackDeliveryBadServerURLFailsGracefully(t *testing.T) {
	log := newTestLogger(t)
	d := newECPackDelivery(func() (string, error) { return t.TempDir(), nil }, log)
	t.Cleanup(d.Close)
	d.OnServerPackURL("http://127.0.0.1:1/nonexistent.zip")
	if _, _, ok := d.PackURL(); ok {
		t.Fatal("下载失败后不应就绪")
	}
}
