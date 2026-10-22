package install

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nexus/internal/netease"
)

// ---- 夹具 ----

// zipBytes 造一个 ZIP（归档内路径 -> 内容）。
func zipBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func md5hex(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

// pkgServer 提供一个内存 HTTP 服务：路径 -> 字节；并统计各路径请求次数。
type pkgServer struct {
	srv   *httptest.Server
	files map[string][]byte
	mu    sync.Mutex
	hits  map[string]int
}

func newPkgServer(t *testing.T, files map[string][]byte) *pkgServer {
	t.Helper()
	p := &pkgServer{files: files, hits: map[string]int{}}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.hits[r.URL.Path]++
		p.mu.Unlock()
		body, ok := p.files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Length", itoa(len(body)))
		_, _ = w.Write(body)
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *pkgServer) url(path string) string { return p.srv.URL + path }

func (p *pkgServer) hitCount(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.hits[path]
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// fakeSource 伪造网关：包信息与组件详情都在内存里，并记录调用次数。
type fakeSource struct {
	base    *netease.ClientPatchInfo
	version *netease.ClientPatchInfo
	mods    []netease.Mod
	details []netease.ComponentDownloadInfo
	server  []netease.ComponentDownloadInfo

	baseCalls, verCalls, detailCalls, serverCalls int
}

func (f *fakeSource) ClientPatchInfo(_ context.Context, id int) (*netease.ClientPatchInfo, error) {
	if id == 0 {
		f.baseCalls++
		return f.base, nil
	}
	f.verCalls++
	return f.version, nil
}

func (f *fakeSource) GameCoreMods(_ context.Context, _ string, _ bool) ([]netease.Mod, error) {
	return f.mods, nil
}

func (f *fakeSource) ComponentDetails(_ context.Context, _ []uint64) ([]netease.ComponentDownloadInfo, error) {
	f.detailCalls++
	return f.details, nil
}

func (f *fakeSource) ServerComponents(_ context.Context, _ string) ([]netease.ComponentDownloadInfo, error) {
	f.serverCalls++
	return f.server, nil
}

// ---- 客户端本体 ----

// TestEnsureClientInstallsMarksAndSkips 首次装、二次跳过：
// 基础包 + 版本包都落位，标记写好，第二次不再下载（大包不能重复下）。
func TestEnsureClientInstallsMarksAndSkips(t *testing.T) {
	// 夹具布局取实测归档形态：基础包/版本包条目都在 .minecraft/ 前缀下。
	base := zipBytes(t, map[string][]byte{".minecraft/libraries/base.jar": []byte("base")})
	ver := zipBytes(t, map[string][]byte{
		".minecraft/versions/1.20/1.20.json": []byte("{}"),
		".minecraft/versions/1.20/1.20.jar":  []byte("core"),
	})
	srv := newPkgServer(t, map[string][]byte{"/base.7z": base, "/1.20.7z": ver})

	src := &fakeSource{
		base:    &netease.ClientPatchInfo{Name: "base.7z", URL: srv.url("/base.7z"), MD5: md5hex(base), Size: int64(len(base))},
		version: &netease.ClientPatchInfo{Name: "1.20.7z", URL: srv.url("/1.20.7z"), MD5: md5hex(ver), Size: int64(len(ver))},
	}
	root := t.TempDir()
	var stages []Stage
	inst := New(srv.srv.Client())
	inst.Report = func(p Progress) { stages = append(stages, p.Stage) }

	mcRoot, err := inst.EnsureClient(context.Background(), src, root, "1.20")
	if err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}
	if mcRoot != filepath.Join(root, ".minecraft") {
		t.Fatalf("客户端根 = %q，期望归档的 .minecraft/ 前缀目录", mcRoot)
	}
	if _, err := os.Stat(filepath.Join(mcRoot, "versions", "1.20", "1.20.json")); err != nil {
		t.Fatalf("版本清单未落位: %v", err)
	}
	if _, err := os.Stat(filepath.Join(mcRoot, "libraries", "base.jar")); err != nil {
		t.Fatalf("基础包内容未落位: %v", err)
	}
	if !containsStage(stages, StageDownload) || !containsStage(stages, StageExtract) || !containsStage(stages, StageDone) {
		t.Fatalf("进度阶段不全: %v", stages)
	}
	// 归档不保留（解压成功即删）。
	if _, err := os.Stat(filepath.Join(root, "cache", "base.7z")); err == nil {
		t.Fatal("归档应已删除")
	}

	before := srv.hitCount("/base.7z") + srv.hitCount("/1.20.7z")
	if _, err := inst.EnsureClient(context.Background(), src, root, "1.20"); err != nil {
		t.Fatalf("二次安装失败: %v", err)
	}
	if after := srv.hitCount("/base.7z") + srv.hitCount("/1.20.7z"); after != before {
		t.Fatalf("已装包被重复下载：%d -> %d", before, after)
	}
}

// TestEnsureClientReportsActualLayout 归档布局与预期不符时，
// 错误里必须带出根目录的**实际**顶层条目 —— 离线无法预知布局，这是唯一能自证的线索。
func TestEnsureClientReportsActualLayout(t *testing.T) {
	weird := zipBytes(t, map[string][]byte{"unexpected/thing.jar": []byte("x")})
	srv := newPkgServer(t, map[string][]byte{"/base.7z": weird, "/1.20.7z": weird})
	src := &fakeSource{
		base:    &netease.ClientPatchInfo{Name: "base.7z", URL: srv.url("/base.7z"), MD5: md5hex(weird), Size: int64(len(weird))},
		version: &netease.ClientPatchInfo{Name: "1.20.7z", URL: srv.url("/1.20.7z"), MD5: md5hex(weird), Size: int64(len(weird))},
	}
	_, err := New(srv.srv.Client()).EnsureClient(context.Background(), src, t.TempDir(), "1.20")
	if err == nil {
		t.Fatal("布局不符应报错")
	}
	if !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("错误应列出实际顶层条目，实得: %v", err)
	}
}

// TestEnsureClientRejectsMD5Mismatch 下载内容被篡改/损坏时必须中止，且留下可读原因。
func TestEnsureClientRejectsMD5Mismatch(t *testing.T) {
	body := zipBytes(t, map[string][]byte{"versions/1.20/1.20.json": []byte("{}")})
	srv := newPkgServer(t, map[string][]byte{"/base.7z": body})
	src := &fakeSource{base: &netease.ClientPatchInfo{
		Name: "base.7z", URL: srv.url("/base.7z"), MD5: strings.Repeat("0", 32), Size: int64(len(body)),
	}}
	_, err := New(srv.srv.Client()).EnsureClient(context.Background(), src, t.TempDir(), "1.20")
	if err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Fatalf("期望 MD5 校验失败，实得 %v", err)
	}
}

// TestEnsureClientPlacesCoreLibs 核心库包条目在 V_<ver>_libs/ 前缀下，必须按真源
// 重定位：.jar → libraries/ 里按 basename 匹配覆盖；非 .jar → versions/<ver>/。
// 官方实测：modlauncher-10.0.9.jar → libraries/cpw/mods/modlauncher/10.0.9/、
// 1.20.dat → versions/1.20/1.20.dat。
func TestEnsureClientPlacesCoreLibs(t *testing.T) {
	base := zipBytes(t, map[string][]byte{
		".minecraft/versions/1.20/1.20.json":                                      []byte("{}"),
		".minecraft/libraries/cpw/mods/modlauncher/10.0.9/modlauncher-10.0.9.jar": []byte("base-version"),
	})
	ver := zipBytes(t, map[string][]byte{".minecraft/versions/1.20/1.20.jar": []byte("core")})
	corelib := zipBytes(t, map[string][]byte{
		"V_1_20_libs/modlauncher-10.0.9.jar": []byte("patched-version"),
		"V_1_20_libs/1.20.dat":               []byte("dat-payload"),
	})
	srv := newPkgServer(t, map[string][]byte{
		"/base.7z": base, "/1.20.7z": ver, "/corelib.7z": corelib,
	})
	src := &fakeSource{
		base: &netease.ClientPatchInfo{Name: "base.7z", URL: srv.url("/base.7z"), MD5: md5hex(base), Size: int64(len(base))},
		version: &netease.ClientPatchInfo{Name: "1.20.7z", URL: srv.url("/1.20.7z"), MD5: md5hex(ver), Size: int64(len(ver)),
			CoreLibName: "corelib.7z", CoreLibURL: srv.url("/corelib.7z"), CoreLibMD5: md5hex(corelib), CoreLibSize: int64(len(corelib))},
	}
	root := t.TempDir()

	inst := New(srv.srv.Client())
	inst.Report = func(p Progress) { t.Logf("progress: %+v", p) }
	mcRoot, err := inst.EnsureClient(context.Background(), src, root, "1.20")
	if err != nil {
		t.Fatalf("安装失败: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(mcRoot, "libraries", "cpw", "mods", "modlauncher", "10.0.9", "modlauncher-10.0.9.jar"))
	if err != nil {
		t.Fatalf("核心库 jar 未按 libraries 坐标落位: %v", err)
	}
	if !bytes.Equal(got, []byte("patched-version")) {
		t.Fatal("核心库 jar 应覆盖基础包同名库")
	}
	dat, err := os.ReadFile(filepath.Join(mcRoot, "versions", "1.20", "1.20.dat"))
	if err != nil {
		t.Fatalf("核心库 dat 未按 versions/<ver> 落位: %v", err)
	}
	if !bytes.Equal(dat, []byte("dat-payload")) {
		t.Fatal("dat 内容不符")
	}
	if _, err := os.Stat(filepath.Join(root, "V_1_20_libs")); err == nil {
		t.Fatal("V_1_20_libs 中转目录不应残留在数据根")
	}
}

// ---- 组件 ----

// TestEnsureComponentsPlacesJarWithMatchingMD5 组件按 modInfo 命名落位，且跳过已装。
func TestEnsureComponentsPlacesJarWithMatchingMD5(t *testing.T) {
	jar := []byte("component-jar-bytes")
	pack := zipBytes(t, map[string][]byte{"inner/mod.jar": jar})
	srv := newPkgServer(t, map[string][]byte{"/c.7z": pack})

	src := &fakeSource{
		mods: []netease.Mod{{IID: "4690372021023046883", MD5: strings.ToUpper(md5hex(jar))}},
		details: []netease.ComponentDownloadInfo{{
			ItemID: "4690372021023046883", MTypeID: 3, EntityID: "0",
			SubEntities: []netease.ComponentDownloadSub{{
				ResName: "c.7z", ResURL: srv.url("/c.7z"), ResMD5: md5hex(pack), JarMD5: md5hex(jar),
			}},
		}},
	}
	modsDir := t.TempDir()
	inst := New(srv.srv.Client())

	n, err := inst.EnsureComponents(context.Background(), src, "1.20", "srv-1", modsDir, false)
	if err != nil {
		t.Fatalf("组件安装失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("新装数量 = %d，期望 1", n)
	}
	target := filepath.Join(modsDir, "4690372021023046883@3@0.jar")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("组件未落位: %v", err)
	}
	if !bytes.Equal(got, jar) {
		t.Fatal("落位的 jar 内容不符")
	}
	if _, err := os.Stat(filepath.Join(modsDir, ".install-tmp")); err == nil {
		t.Fatal("临时目录应已清理")
	}

	before := srv.hitCount("/c.7z")
	n, err = inst.EnsureComponents(context.Background(), src, "1.20", "srv-1", modsDir, false)
	if err != nil {
		t.Fatalf("二次安装失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("已装组件应跳过，新装数量 = %d", n)
	}
	if srv.hitCount("/c.7z") != before {
		t.Fatal("已装组件被重复下载")
	}
}

// TestEnsureComponentsRejectsJarMD5Mismatch 解出的 jar 与网关 md5 不符时放弃落位 ——
// 服务端校验的正是这个 md5，塞错的 jar 只会让进服失败得更晚更难查。
func TestEnsureComponentsRejectsJarMD5Mismatch(t *testing.T) {
	jar := []byte("wrong-jar")
	pack := zipBytes(t, map[string][]byte{"mod.jar": jar})
	srv := newPkgServer(t, map[string][]byte{"/c.7z": pack})

	src := &fakeSource{
		mods: []netease.Mod{{IID: "123"}},
		details: []netease.ComponentDownloadInfo{{
			ItemID: "123", MTypeID: 2, EntityID: "8",
			SubEntities: []netease.ComponentDownloadSub{{
				ResName: "c.7z", ResURL: srv.url("/c.7z"), JarMD5: strings.Repeat("a", 32),
			}},
		}},
	}
	modsDir := t.TempDir()
	_, err := New(srv.srv.Client()).EnsureComponents(context.Background(), src, "1.20", "srv-1", modsDir, false)
	if err == nil || !strings.Contains(err.Error(), "MD5 与网关不符") {
		t.Fatalf("期望 jar MD5 不符错误，实得 %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(modsDir, "123@2@8.jar")); statErr == nil {
		t.Fatal("MD5 不符的 jar 不应落位")
	}
}

// TestEnsureComponentsSkipsWithoutResURL 网关未给下载地址的组件跳过，
// 但不能假装装好（不落位、不报错阻塞其它组件）。
func TestEnsureComponentsSkipsWithoutResURL(t *testing.T) {
	src := &fakeSource{
		mods: []netease.Mod{{IID: "123"}},
		details: []netease.ComponentDownloadInfo{{
			ItemID: "123", MTypeID: 3, EntityID: "0",
			SubEntities: []netease.ComponentDownloadSub{{JarMD5: "abcd"}},
		}},
	}
	n, err := New(nil).EnsureComponents(context.Background(), src, "1.20", "srv-1", t.TempDir(), false)
	if err != nil {
		t.Fatalf("不应因缺地址而失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("没有下载地址时不应计入安装数，实得 %d", n)
	}
}

// TestEnsureComponentsInstallsServerPackage 服务器组件是"mod 集合包"：
// item 的 sub_entities 指向整包归档（条目 .minecraft/mods/*.jar），下载解压后
// 全部落位，并按 sub_mod_list（iid 清单）校验完整性（实测 2026-09 布吉岛）。
func TestEnsureComponentsInstallsServerPackage(t *testing.T) {
	mods := zipBytes(t, map[string][]byte{
		".minecraft/mods/4690287328241054167@2@8.jar": []byte("main-mod"),
		".minecraft/mods/4690287328189015853@2@8.jar": []byte("sub-mod"),
	})
	srv := newPkgServer(t, map[string][]byte{"/mods.7z": mods})

	src := &fakeSource{
		server: []netease.ComponentDownloadInfo{{
			ItemID: "4661334467366178884", MTypeID: 2,
			SubModList: []uint64{4690287328241054167, 4690287328189015853},
			SubEntities: []netease.ComponentDownloadSub{{
				ResName: "2026-09-04-01.7z", ResURL: srv.url("/mods.7z"),
				ResMD5: md5hex(mods), ResSize: int64(len(mods)),
			}},
		}},
	}
	modsDir := t.TempDir()
	inst := New(srv.srv.Client())

	n, err := inst.EnsureComponents(context.Background(), src, "1.20", "srv-1", modsDir, false)
	if err != nil {
		t.Fatalf("集合包安装失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("落位 jar 数 = %d，期望 2", n)
	}
	for _, name := range []string{"4690287328241054167@2@8.jar", "4690287328189015853@2@8.jar"} {
		if _, err := os.Stat(filepath.Join(modsDir, name)); err != nil {
			t.Fatalf("%s 未落位: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(modsDir, ".serverpkg")); err == nil {
		t.Fatal("集合包暂存目录不应残留")
	}

	// 复检：归档缓存命中（MD5 一致），jar 已齐 → 0 新装。
	n, err = inst.EnsureComponents(context.Background(), src, "1.20", "srv-1", modsDir, false)
	if err != nil {
		t.Fatalf("复检失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("复检新装 = %d，期望 0", n)
	}
}

// TestEnsureComponentsRejectsIncompleteServerPackage 集合包缺 jar（归档与服务端
// sub_mod_list 不符）时必须报错 —— 静默缺组件会让进服失败得更晚更难查。
func TestEnsureComponentsRejectsIncompleteServerPackage(t *testing.T) {
	mods := zipBytes(t, map[string][]byte{
		".minecraft/mods/4690287328241054167@2@8.jar": []byte("main-mod"),
	})
	srv := newPkgServer(t, map[string][]byte{"/mods.7z": mods})
	src := &fakeSource{
		server: []netease.ComponentDownloadInfo{{
			ItemID: "4661334467366178884", MTypeID: 2,
			SubModList: []uint64{4690287328241054167, 4690287328189015853},
			SubEntities: []netease.ComponentDownloadSub{{
				ResName: "mods.7z", ResURL: srv.url("/mods.7z"),
				ResMD5: md5hex(mods), ResSize: int64(len(mods)),
			}},
		}},
	}
	_, err := New(srv.srv.Client()).EnsureComponents(context.Background(), src, "1.20", "srv-1", t.TempDir(), false)
	if err == nil || !strings.Contains(err.Error(), "缺组件 iid") {
		t.Fatalf("期望缺组件错误，实得 %v", err)
	}
}

// TestComponentJarName 命名必须与 internal/netease.ParseJarName 的判据一致
// （磁盘实测：…@3@0.jar 与 …@2@8.jar）。
func TestComponentJarName(t *testing.T) {
	cases := []struct {
		itemID   string
		mtype    int
		entityID string
		want     string
	}{
		{"4690372021023046883", 3, "0", "4690372021023046883@3@0.jar"},
		{"4687456828685462969", 2, "8", "4687456828685462969@2@8.jar"},
		{"123", 3, "", "123@3@0.jar"}, // EntityId 缺失按 0
	}
	for _, c := range cases {
		if got := ComponentJarName(c.itemID, c.mtype, c.entityID); got != c.want {
			t.Fatalf("ComponentJarName(%q,%d,%q) = %q，期望 %q", c.itemID, c.mtype, c.entityID, got, c.want)
		}
	}
}

func containsStage(stages []Stage, want Stage) bool {
	for _, s := range stages {
		if s == want {
			return true
		}
	}
	return false
}
