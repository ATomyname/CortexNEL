// Package install 补齐本地缺失的客户端本体与服务器组件。
//
// 为什么需要它：nexus 原先假定"基础客户端与组件由网易启动器预先落盘"，于是本地没有
// 某服的组件时只能退回默认客户端 —— 实测表现为"点 A 服却起了 B 服的客户端"。
// 本包按参考实现（Fantnel InstallerService）的模型补齐缺口：
//
//	MD5 标记文件判"已装" → 下载（带进度）→ 校验 MD5 → 解压（带进度）→ 写标记
//
// 产物落在 internal/clientdata 选址的版本数据目录（可用空间最大的固定盘根下
// <盘>:\CortexNEL\client），不写用户的网易安装目录。
package install

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nexus/internal/archive"
	"nexus/internal/httpx"
	"nexus/internal/netease"
)

// Stage 是安装阶段（进度事件与外层文案共用同一组取值）。
type Stage string

const (
	StageDownload Stage = "download"
	StageVerify   Stage = "verify"
	StageExtract  Stage = "extract"
	StageDone     Stage = "done"
)

// Progress 是一次安装进度。
type Progress struct {
	Stage Stage
	// Subject 是被安装对象，如 "client:1.20" 或 "component:4690372021023046883"。
	Subject string
	Done    int64
	Total   int64
	Message string
}

// Reporter 接收进度回调（可为 nil）。
type Reporter func(Progress)

// Package 是一个可下载的归档包（客户端本体包或组件包）。
type Package struct {
	Name string
	URL  string
	MD5  string
	Size int64
}

// Source 是安装所需的外部信息源（由 internal/netease.Gateway 实现）。
type Source interface {
	ClientPatchInfo(ctx context.Context, mcVersionID int) (*netease.ClientPatchInfo, error)
	GameCoreMods(ctx context.Context, mcVersion string, isRental bool) ([]netease.Mod, error)
	ComponentDetails(ctx context.Context, itemIDs []uint64) ([]netease.ComponentDownloadInfo, error)
	// ServerComponents 返回该服的权威组件下载信息（iid + md5 + 下载地址）。
	ServerComponents(ctx context.Context, serverID string) ([]netease.ComponentDownloadInfo, error)
}

// Logger 是安装器的最小日志能力（由调用方注入，避免反向依赖 telemetry；
// telemetry.Logger 的 Info/Warn 签名与此兼容）。
type Logger interface {
	Info(module, message string, kv ...any)
	Warn(module, message string, kv ...any)
}

// Installer 把远端包落到本地。
type Installer struct {
	HTTP   *http.Client
	Report Reporter
	// Log 可选：注入后，下载/校验/解压/落位/对账的每个关键节点都留痕
	// （module 固定为 "install"），排查资产问题时不必再靠进度事件倒推。
	Log Logger
}

// logInfo / logWarn 在未注入 Logger 时静默。
func (i *Installer) logInfo(message string, kv ...any) {
	if i.Log != nil {
		i.Log.Info("install", message, kv...)
	}
}

func (i *Installer) logWarn(message string, kv ...any) {
	if i.Log != nil {
		i.Log.Warn("install", message, kv...)
	}
}

// skipLogger 返回记录解压放弃条目的回调。archive 对被拒条目会判整包失败，
// 这里在错误消息之外再留一份带 subject 的日志线索（错误会被上层包装，逐条
// 原因更容易在排查时定位到具体文件名）。
func (i *Installer) skipLogger(subject string) func(name, reason string) {
	return func(name, reason string) {
		i.logWarn("解压条目无法落位", "subject", subject, "entry", name, "reason", reason)
	}
}

// New 构造安装器；httpClient 为空时用带超时的默认客户端。
func New(httpClient *http.Client) *Installer {
	if httpClient == nil {
		httpClient = httpx.NewClient(0)
	}
	return &Installer{HTTP: httpClient}
}

// EnsureClient 确保 mcVersion 的客户端本体已装到 root 下，返回实际客户端根目录
// （实测基础包/版本包条目在 .minecraft/ 前缀下，客户端根是 root/.minecraft，见
// package.go clientRoot；调用方应以返回值为准，不要自行拼路径）。
//
// 顺序照参考实现：基础包（mc_version=0）→ 版本包 → 版本核心库。
// 装完后校验关键文件确实存在 —— 归档内部布局无法离线预知，与其硬往下走，
// 不如把实际情况作为错误报出来（含顶层条目），第一次真机下载即可定位。
func (i *Installer) EnsureClient(ctx context.Context, src Source, root, mcVersion string) (string, error) {
	if root == "" || mcVersion == "" {
		return "", fmt.Errorf("install: 客户端本体安装缺少数据目录或版本号")
	}
	base, err := src.ClientPatchInfo(ctx, 0)
	if err != nil {
		return "", fmt.Errorf("install: 取基础包信息: %w", err)
	}
	if err := i.installPackage(ctx, packageOf(base.Name, base.URL, base.MD5, base.Size), root, "client:base"); err != nil {
		return "", err
	}

	ver, err := src.ClientPatchInfo(ctx, netease.McVersionID(mcVersion))
	if err != nil {
		return "", fmt.Errorf("install: 取 %s 版本包信息: %w", mcVersion, err)
	}
	if err := i.installPackage(ctx, packageOf(ver.Name, ver.URL, ver.MD5, ver.Size), root, "client:"+mcVersion); err != nil {
		return "", err
	}
	if ver.CoreLibURL != "" {
		pkg := packageOf(ver.CoreLibName, ver.CoreLibURL, ver.CoreLibMD5, ver.CoreLibSize)
		if err := i.installCoreLib(ctx, pkg, root, mcVersion); err != nil {
			return "", err
		}
	}

	return verifyClientLayout(root, mcVersion)
}

// EnsureComponents 确保该服进服所需的组件已落到 modsDir，返回本次新装的数量。
//
// 清单以网关权威数据为准，每次进服都对账（iid+md5）：
//
//	① 核心组件：版本级清单（网易 RPC/网络控制组件在内）；
//	② 服务器组件：该服清单（服务端自有 mod，含反作弊组件）。
//
// 已存在且 MD5 与网关一致（jar_md5）的组件跳过下载；有差异只下差异。
// 不做任何"本地已有就整批跳过"的捷径 —— 本地副本的版本与完整性无从对账。
func (i *Installer) EnsureComponents(ctx context.Context, src Source, mcVersion, serverID, modsDir string, isRental bool) (int, error) {
	if modsDir == "" {
		return 0, fmt.Errorf("install: 组件安装缺少目标目录")
	}
	installed := 0

	coreMods, err := src.GameCoreMods(ctx, mcVersion, isRental)
	if err != nil {
		return 0, fmt.Errorf("install: 取核心组件清单: %w", err)
	}
	ids := make([]uint64, 0, len(coreMods))
	for _, m := range coreMods {
		if id, ok := parseItemID(m.IID); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		core, err := src.ComponentDetails(ctx, ids)
		if err != nil {
			return 0, fmt.Errorf("install: 取核心组件详情: %w", err)
		}
		i.logInfo("核心组件清单对账", "count", len(core), "iids", ids)
		n, err := i.installComponents(ctx, core, modsDir)
		installed += n
		if err != nil {
			return installed, err
		}
	}

	if serverID != "" {
		serverComps, err := src.ServerComponents(ctx, serverID)
		if err != nil {
			return installed, fmt.Errorf("install: 取服务器组件清单: %w", err)
		}
		i.logInfo("服务器组件清单获取", "serverID", serverID, "count", len(serverComps))
		n, err := i.installServerPackages(ctx, serverComps, modsDir)
		installed += n
		if err != nil {
			return installed, err
		}
	}
	return installed, nil
}

// installServerPackages 安装服务器 mod 集合包（实测 2026-09 布吉岛）：
// 服务器 item 的每个 sub_entity 指向一个归档（95MB 级，条目为
// .minecraft/mods/*.jar —— 整套该服 mod），下载并解压后把其中的 jar
// 全部落位到 modsDir，再按 item 的 sub_mod_list（iid 清单）校验完整性。
//
// jar_md5 为空（集合包整体校验靠 res_md5），单 jar 完整性以"iid 集合对齐"
// 把关：缺 jar / 多 jar 都在这里报错，不静默往下走。
func (i *Installer) installServerPackages(ctx context.Context, comps []netease.ComponentDownloadInfo, modsDir string) (int, error) {
	installed := 0
	for _, c := range comps {
		for _, sub := range c.SubEntities {
			if sub.ResURL == "" {
				i.logWarn("服务器集合包缺下载地址，跳过", "item", c.ItemID, "res_name", sub.ResName)
				continue // 网关未给下载地址：跳过而不是假装装好
			}
			subject := "component:" + c.ItemID
			archivePath := filepath.Join(modsDir, ".serverpkg", sub.ResName)
			if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
				return installed, err
			}
			// 标记判已装（与本体包同规则）；已装仍要过一遍 jar 齐全性对账，
			// 防止标记在但 modsDir 被人动过。
			marker := markerPath(modsDir, sub.ResName)
			_ = os.RemoveAll(filepath.Join(modsDir, ".serverpkg"))
			if !markerHit(marker, sub.ResMD5) {
				i.logInfo("开始下载服务器集合包", "item", c.ItemID, "name", sub.ResName,
					"url", sub.ResURL, "size", sub.ResSize)
				i.report(Progress{Stage: StageDownload, Subject: subject, Total: sub.ResSize, Message: "下载 " + sub.ResName})
				if err := archive.Download(ctx, i.HTTP, sub.ResURL, archivePath, i.progressReporter(StageDownload, subject, "下载 "+sub.ResName)); err != nil {
					return installed, fmt.Errorf("install: 下载 %s: %w", sub.ResName, err)
				}
				i.report(Progress{Stage: StageVerify, Subject: subject, Total: sub.ResSize, Message: "校验 " + sub.ResName})
				if !archive.Matches(archivePath, sub.ResMD5) {
					_ = os.Remove(archivePath)
					i.logWarn("服务器集合包 MD5 校验失败", "item", c.ItemID, "name", sub.ResName, "expect", sub.ResMD5)
					return installed, fmt.Errorf("install: %s 校验失败（MD5 不匹配），包可能已更新或下载损坏", sub.ResName)
				}
				i.logInfo("服务器集合包 MD5 校验通过", "item", c.ItemID, "name", sub.ResName)

				staging := filepath.Join(modsDir, ".serverpkg", "staging")
				i.report(Progress{Stage: StageExtract, Subject: subject, Total: sub.ResSize, Message: "解压 " + sub.ResName})
				if err := archive.Extract(archive.ExtractRequest{
					Archive:  archivePath,
					Dest:     staging,
					Ctx:      ctx,
					Progress: i.progressReporter(StageExtract, subject, "解压 "+sub.ResName),
					OnSkip:   i.skipLogger(subject),
				}); err != nil {
					return installed, fmt.Errorf("install: 解压 %s: %w", sub.ResName, err)
				}
				jars := collectJars(filepath.Join(staging, ".minecraft", "mods"))
				if len(jars) == 0 {
					_ = os.RemoveAll(filepath.Join(modsDir, ".serverpkg"))
					i.logWarn("集合包内没有 mods/*.jar", "item", c.ItemID, "name", sub.ResName)
					return installed, fmt.Errorf("install: %s 内没有 mods/*.jar（归档布局与预期不符）", sub.ResName)
				}
				for name, src := range jars {
					if err := copyFile(src, filepath.Join(modsDir, name)); err != nil {
						return installed, err
					}
				}
				installed += len(jars)
				i.logInfo("集合包 mod 落位完成", "item", c.ItemID, "jar_count", len(jars), "modsDir", modsDir)
				if err := writeMarker(marker, sub.ResMD5); err != nil {
					return installed, fmt.Errorf("install: 写安装标记 %s: %w", marker, err)
				}
				_ = os.RemoveAll(filepath.Join(modsDir, ".serverpkg"))
				_ = os.Remove(archivePath)
				i.report(Progress{Stage: StageDone, Subject: subject, Total: sub.ResSize, Done: sub.ResSize,
					Message: "完成 " + sub.ResName})
			} else {
				i.logInfo("服务器集合包标记命中，跳过下载", "item", c.ItemID, "name", sub.ResName)
			}

			// 完整性对账：落位 jar 的 iid 集合应与网关 sub_mod_list 一致。
			if len(c.SubModList) > 0 {
				if missing := missingIIDs(modsDir, c.SubModList); len(missing) > 0 {
					i.logWarn("落位后缺组件 iid", "item", c.ItemID, "missing", missing)
					return installed, fmt.Errorf("install: %s 落位后缺组件 iid %v（缓存与服务端清单不符）",
						sub.ResName, missing)
				}
				i.logInfo("集合包完整性对账通过", "item", c.ItemID, "required_iids", len(c.SubModList))
			}
		}
	}
	return installed, nil
}

// collectJars 收集 dir 下的 jar 文件（按文件名索引绝对路径）。
func collectJars(dir string) map[string]string {
	out := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".jar") {
			out[e.Name()] = filepath.Join(dir, e.Name())
		}
	}
	return out
}

// missingIIDs 检查 modsDir 里的 jar（文件名首段为 ItemId）是否覆盖了要求的 iid 集合。
func missingIIDs(modsDir string, want []uint64) []uint64 {
	have := map[string]bool{}
	entries, _ := os.ReadDir(modsDir)
	for _, e := range entries {
		parts := strings.SplitN(e.Name(), "@", 2)
		if len(parts) == 2 {
			have[parts[0]] = true
		}
	}
	var missing []uint64
	for _, id := range want {
		if !have[strconv.FormatUint(id, 10)] {
			missing = append(missing, id)
		}
	}
	return missing
}

// copyFile 复制文件（覆盖目标）。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// installComponents 把一批组件对账落位：MD5 一致即跳过，不一致或缺失才下载。
func (i *Installer) installComponents(ctx context.Context, components []netease.ComponentDownloadInfo, modsDir string) (int, error) {
	if err := os.MkdirAll(modsDir, 0o755); err != nil {
		return 0, err
	}
	installed := 0
	for _, c := range components {
		for _, sub := range c.SubEntities {
			if sub.ResURL == "" {
				continue // 网关未给下载地址：跳过而不是假装装好
			}
			target := filepath.Join(modsDir, ComponentJarName(c.ItemID, c.MTypeID, c.EntityID))
			if sub.JarMD5 != "" && archive.Matches(target, sub.JarMD5) {
				continue
			}
			if err := i.installComponent(ctx, c, sub, modsDir); err != nil {
				return installed, err
			}
			installed++
		}
	}
	return installed, nil
}

// ComponentJarName 是组件的本地文件名：{ItemId}@{MTypeId}@{EntityId}.jar。
//
// 与 internal/netease.ParseJarName 的判据一致（服务端校验的 modInfo 用的就是这个形状），
// 也与磁盘上实测的文件名一致（4690372021023046883@3@0.jar / …@2@8.jar）。
// EntityId 缺失时按 0 处理（MTypeId=3 的核心组件实测即 @3@0）。
func ComponentJarName(itemID string, mtypeID int, entityID string) string {
	if entityID == "" {
		entityID = "0"
	}
	return fmt.Sprintf("%s@%d@%s.jar", itemID, mtypeID, entityID)
}
