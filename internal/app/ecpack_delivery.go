// EC 资源包交付：把服务端进服时下发的资源包原样经本地 HTTP 交付给镜像客户端。
//
// 不做任何转换（2026-10-03 起）：服务端下发的 ViaBedrock 包本身就是原版合规包
// （pack.mcmeta pack_format=84=1.21.8，assets/viabedrock|minecraft|bedrock 加
// bedrock/*.mcpack 原件），白端直接进服加载的就是它。nexus 只做「下载 → 本地分发」，
// 由 relay 把服务端推送地址改写为本机分发地址（hash/required 兜底见
// internal/relay/packet/ecpack），交付内容与服务端逐字节一致。
//
// 双源（2026-10-03 实测服务端并非每次进服都推 ResourcePackPush）：
// 服务端推送 → 按推送地址下载并记为已知包；服务端没推 → 用已知包兜底
// （本地缓存优先，其次按 manifest 的内容寻址 URL 拉取），由 JoinGame 注入器
// 主动推给客户端 —— 只改写「已存在的推送」在没推时会什么都不发生。
package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nexus/internal/archive"
	"nexus/internal/clientdata"
	"nexus/internal/ecpack"
	"nexus/internal/httpx"
	"nexus/internal/store"
	"nexus/internal/telemetry"
)

// ecPackRetryCooldown 是「准备失败后的重试冷却」。
//
// EC 会话每帧都可能调 PackURL，而资源包是 70MB 级下载，失败不能无节流重试；
// 但也不能永久放弃（服务端推送可能晚到），故按冷却节流。
const ecPackRetryCooldown = 30 * time.Second

// ecPackDelivery 惰性准备并交付 EC 资源包，实现 relay 侧 ecpack.Provider。
//
// 惰性有两层意义：① 只有 EC 会话会调用 PackURL（注册键按 EC GameID 作用域），
// 非 EC 服零开销；② 首次调用才定位/打包/起服务，之后走缓存与已登记结果。
type ecPackDelivery struct {
	cacheDir func() (string, error)
	logger   *telemetry.Logger

	mu           sync.Mutex
	server       *ecpack.Server
	serverURL    string
	url          string
	hash         string
	ready        bool
	usedFallback bool
	nextTry      time.Time
}

// newECPackDelivery 组装交付器（cacheDir 由调用方给出，便于测试注入替身）。
func newECPackDelivery(cacheDir func() (string, error), logger *telemetry.Logger) *ecPackDelivery {
	return &ecPackDelivery{cacheDir: cacheDir, logger: logger}
}

// PackURL 实现 relay 侧 Provider：未准备好时返回 ok=false，relay 据此原样透传服务端资源包。
func (d *ecPackDelivery) PackURL() (string, string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.ready {
		return d.url, d.hash, true
	}
	now := time.Now()
	if now.Before(d.nextTry) {
		return "", "", false
	}
	d.nextTry = now.Add(ecPackRetryCooldown)

	if err := d.prepareLocked(); err != nil {
		// 只记日志、不介入：本机没有该目录（没跑过 EC 客户端）是常态，不是错误路径。
		d.logger.Info("ecpack", "EC 资源包尚未就绪，本次不介入", "err", err.Error())
		return "", "", false
	}
	return d.url, d.hash, true
}

// prepareLocked 准备资源包并分发登记（调用方持锁）。
//
// 双源：① 有服务端推送 → 按推送地址下载原包并记为已知包；
// ② 服务端没推 → 用已知包兜底（见 knownPack），交给 JoinGame 注入器推送。
func (d *ecPackDelivery) prepareLocked() error {
	cache, err := d.cacheDir()
	if err != nil {
		return err
	}
	pack := filepath.Join(cache, "server-pack.zip")
	if d.serverURL != "" {
		if err := downloadURL(d.serverURL, pack); err != nil {
			return fmt.Errorf("下载服务端资源包失败: %w", err)
		}
		d.rememberPack(cache, pack)
		d.usedFallback = false
	} else {
		p, err := d.knownPack(cache, pack)
		if err != nil {
			return err
		}
		pack = p
		d.usedFallback = true
		d.logger.Info("ecpack", "服务端本次未推送资源包，用已知包兜底注入", "pack", pack)
	}
	sum, err := fileHash(pack)
	if err != nil {
		return err
	}
	zipPath := pack
	if d.server == nil {
		srv, serr := ecpack.Start("127.0.0.1:0")
		if serr != nil {
			return serr
		}
		d.server = srv
	}
	url, err := d.server.Publish(zipPath, sum)
	if err != nil {
		return err
	}
	d.url, d.hash, d.ready = url, sum, true
	d.logger.Info("ecpack", "EC 资源包已就绪（服务端原包，未转换），relay 将把服务端资源包改写为本地分发",
		"pack", zipPath, "sha1", sum, "url", url)
	return nil
}

// OnServerPackURL 实现 relay 侧 ecpack.ServerPackURLSink：
// 服务端推了资源包就把 URL 记下来，并立即尝试准备（失败由 PackURL 的冷却重试兜底）。
func (d *ecPackDelivery) OnServerPackURL(url string) {
	if url == "" {
		return
	}
	d.mu.Lock()
	if d.serverURL == url {
		d.mu.Unlock()
		return
	}
	d.serverURL = url
	// 兜底态下收到真推送要刷新（服务端包才是权威源）；已就绪的正源不重复下载。
	need := !d.ready || d.usedFallback
	d.mu.Unlock()
	if need {
		// 主动触发一次准备；失败不阻塞（下个包/冷却后由 PackURL 再试）。
		d.mu.Lock()
		if err := d.prepareLocked(); err != nil {
			d.logger.Info("ecpack", "按服务端推送准备资源包未就绪", "url", url, "err", err.Error())
		}
		d.mu.Unlock()
	}
}

// packManifest 持久记录「已知资源包」（服务端不推送时的兜底源）。
// 该 CDN 为内容寻址（…/packs/<sha1>.zip，URL 文件名即包内容 SHA1），
// 地址不可变、永久可取 —— 记下 URL 就等于记下整包。
type packManifest struct {
	URL  string `json:"url"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
}

// packManifestName 是已知包记录在缓存目录中的文件名。
const packManifestName = "pack-manifest.json"

// knownPack 给出服务端未推送时可用的已知资源包：本地缓存优先（含旧名
// server-pack.7z —— 内容同为 zip），缓存缺失则按 manifest 的内容寻址 URL 拉取。
// 无任何来源时报错（PackURL 据此返回不就绪，relay 原样透传、不介入）。
func (d *ecPackDelivery) knownPack(cache, dst string) (string, error) {
	if fileExists(dst) {
		return dst, nil
	}
	legacy := filepath.Join(cache, "server-pack.7z")
	if fileExists(legacy) {
		return legacy, nil
	}
	m := loadPackManifest(cache)
	if m == nil || m.URL == "" {
		return "", fmt.Errorf("服务端未推送，且无已知资源包可兜底")
	}
	if err := downloadURL(m.URL, dst); err != nil {
		return "", fmt.Errorf("按已知包地址拉取失败: %w", err)
	}
	return dst, nil
}

// rememberPack 把本次服务端推送的包记录为已知包（manifest 落盘；写失败不影响交付）。
func (d *ecPackDelivery) rememberPack(cache, pack string) {
	sum, err := fileHash(pack)
	if err != nil {
		return
	}
	info, err := os.Stat(pack)
	if err != nil {
		return
	}
	raw, err := json.Marshal(packManifest{URL: d.serverURL, SHA1: sum, Size: info.Size()})
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(cache, packManifestName), raw, 0o644)
}

// loadPackManifest 读已知包记录（缺失或损坏返回 nil）。
func loadPackManifest(cache string) *packManifest {
	raw, err := os.ReadFile(filepath.Join(cache, packManifestName))
	if err != nil {
		return nil
	}
	var m packManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return &m
}

// fileExists 判断路径是常规文件。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// downloadURL 把 URL 内容下载到 dst（覆盖式）。
func downloadURL(url, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	return archive.Download(ctx, httpx.NewClient(0), url, dst, nil)
}

// fileHash 返回文件 sha1（十六进制小写），客户端按它校验资源包。
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Close 释放分发服务（应用退出时调用）。
func (d *ecPackDelivery) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.server != nil {
		_ = d.server.Close()
		d.server = nil
		d.ready = false
	}
}

// ecPackDeliveryFor 构造接入 app 的交付器：缓存落在版本数据目录下（与下载产物同处，便于清理与迁移）。
func ecPackDeliveryFor(paths *store.Paths, settings *store.Settings, logger *telemetry.Logger) *ecPackDelivery {
	return newECPackDelivery(
		func() (string, error) {
			root, err := clientdata.Root(paths, settings)
			if err != nil {
				return "", err
			}
			return filepath.Join(root, "ecpack"), nil
		},
		logger,
	)
}
