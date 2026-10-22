// 静态 CDN 下载根：JRE/JDK 分发包、SDK 运行库、基岩整包都从这一个主机取
// （实测：x19.gdl.netease.com，与官方启动器 authentication\x64\ 与 MCLDownload
// ext 同一下载面）。
//
// 单一主机是网易侧的事实，本应用无法发明第二来源；这里提供环境变量覆盖
// （NEXUS_NETEASE_CDN，如 https://mirror.example.com/netease），让分发运维在
// 主机不可达时把三处下载一起指到同路径结构的镜像，不必重新构建。
package netease

import (
	"os"
	"strings"
)

// defaultCDNBase 是实测的网易静态 CDN 主机根。
const defaultCDNBase = "https://x19.gdl.netease.com"

// cdnBaseEnv 是覆盖下载根的环境变量。
const cdnBaseEnv = "NEXUS_NETEASE_CDN"

// CDNBase 返回静态 CDN 下载根（无尾随斜杠）。
// 设置了 NEXUS_NETEASE_CDN 时用覆盖值 —— 供镜像运维与故障切换，生产默认
// 保持实测主机不变。
func CDNBase() string {
	if overridden := strings.TrimSpace(os.Getenv(cdnBaseEnv)); overridden != "" {
		return strings.TrimRight(overridden, "/")
	}
	return defaultCDNBase
}
