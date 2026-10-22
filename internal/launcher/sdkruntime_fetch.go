// SDK 运行库补件：清单里缺失的文件从网易 CDN 下载进包目录。
//
// 词汇约定（四份文件各一名一责）：清单（有哪些文件）见 sdkruntime_manifest.go，
// 来源（去哪儿拿）见 sdkruntime_source.go，补件（本文件，把缺失的下载回来），
// 落位（铺进 natives/runtime）见 sdkruntime.go。
//
// 清单里的每一项都能在 x19.gdl.netease.com/authentication/x64/ 直接下载（2026-09
// 逐项 HEAD 实测）：那是官方启动器自身 authentication\x64\ 目录的 CDN 镜像，与
// jre/jdk 分发包同一个下载面。所以 SDK 运行库不必随本产品分发 —— 不用把几十 MB
// 网易专有二进制塞进仓库或安装包，新装机器靠下载就能凑齐（能分发）。
//
// 只补缺失项：本地已有齐全副本（例如已跑通过的安装）原样保留，不被本次下载覆盖。
// 理由是 gray 组件在该路径下有多个加壳构建（实测 12,091,408 / 19,570,696 /
// 21,325,320 / 21,905,928 字节四种），逐一比对 JNI 符号契约完全一致
// （reviewWord / reviewName / GetToken / Skip32 / preInit /
// Java_com_netease_mc_mod_network_common_Library），但没有任何理由去替换一个已经
// 验证能跑起来的本地副本（能启动）。
package launcher

import (
	"context"
	"net/http"
	"os"
	"path/filepath"

	"nexus/internal/archive"
	"nexus/internal/httpx"
	"nexus/internal/netease"
)

// sdkRuntimeCDNBase 是网易 CDN 上 SDK 运行库的下载根：
// <静态 CDN 主机>/authentication/x64（官方启动器自身该目录的 CDN 镜像）。
// 主机可经 NEXUS_NETEASE_CDN 整体切镜像（见 netease.CDNBase）。
var sdkRuntimeCDNBase = netease.CDNBase() + "/authentication/x64"

// SdkRuntimeFetchReport 是一次 CDN 补件的结果。
type SdkRuntimeFetchReport struct {
	// PackDir 是补件目标（启动器置备的包目录）。
	PackDir string
	// Downloaded 是本次从 CDN 补下来的文件数。
	Downloaded int
	// Failed 是下载失败的清单项，按清单序。
	Failed []string
}

// FetchSdkRuntimePack 把客户端根下的包目录里缺失的清单项从网易 CDN 补齐。
func FetchSdkRuntimePack(ctx context.Context, clientDir string) SdkRuntimeFetchReport {
	return fetchSdkRuntimePack(ctx, clientDir, sdkRuntimeCDNBase)
}

// fetchSdkRuntimePack 从给定下载根补齐包目录。下载根作为参数而不是读常量，
// 只为让测试能指向本地桩服务，不改变生产取值。
//
// 幂等：已存在且非空的不重复下载。写入走临时文件 + 改名，失败只清临时文件 ——
// 半份 dll 绝不能顶着最终文件名留在包目录里，否则会被后续落位当成完整件
// （能启动的前提）。archive.Download 自身也原子落位，这里再兜一层改名语义。
func fetchSdkRuntimePack(ctx context.Context, clientDir, base string) SdkRuntimeFetchReport {
	report := SdkRuntimeFetchReport{}
	if clientDir == "" {
		return report
	}
	report.PackDir = filepath.Join(clientDir, sdkRuntimePackDirName)
	if err := os.MkdirAll(report.PackDir, 0o755); err != nil {
		report.Failed = append([]string{}, sdkRuntimeManifest...)
		return report
	}

	client := httpx.NewClient(0)
	for _, name := range sdkRuntimeManifest {
		dst := filepath.Join(report.PackDir, name)
		if info, err := os.Stat(dst); err == nil && info.Size() > 0 {
			continue
		}
		if fetchSdkRuntimeFile(ctx, client, base+"/"+name, dst) {
			report.Downloaded++
			continue
		}
		report.Failed = append(report.Failed, name)
	}
	return report
}

// fetchSdkRuntimeFile 下载单个清单项并原子落位，返回是否成功。
func fetchSdkRuntimeFile(ctx context.Context, client *http.Client, url, dst string) bool {
	tmp := dst + ".part"
	defer os.Remove(tmp) // 成功时已被改名带走，这里只是兜底清理。
	if err := archive.Download(ctx, client, url, tmp, nil); err != nil {
		return false
	}
	return renameIntoPlace(tmp, dst)
}
