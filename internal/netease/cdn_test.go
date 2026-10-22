// CDN 下载根的判据：默认走实测主机，环境变量可整体切换到镜像。
package netease

import "testing"

func TestCDNBaseDefaultsToVerifiedHost(t *testing.T) {
	t.Setenv("NEXUS_NETEASE_CDN", "")
	if got := CDNBase(); got != "https://x19.gdl.netease.com" {
		t.Fatalf("CDNBase() = %q，期望实测主机", got)
	}
}

func TestCDNBaseHonoursOverride(t *testing.T) {
	t.Setenv("NEXUS_NETEASE_CDN", "https://mirror.example.com/netease/")
	if got := CDNBase(); got != "https://mirror.example.com/netease" {
		t.Fatalf("CDNBase() = %q，期望覆盖值且无尾随斜杠", got)
	}
	// 纯空白视为未设置，不能把空白拼进下载 URL。
	t.Setenv("NEXUS_NETEASE_CDN", "   ")
	if got := CDNBase(); got != "https://x19.gdl.netease.com" {
		t.Fatalf("空白覆盖应回退默认主机，实得 %q", got)
	}
}
