package httpx

import (
	"net/http"
	"testing"
	"time"
)

// TestBypassProxyUsesDirectTransport 绕过代理时必须吃不到环境代理
// （Transport.Proxy 为 nil = 直连），否则"绕过"形同虚设。
func TestBypassProxyUsesDirectTransport(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:7897")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:7897")

	SetBypassProxy(true)
	defer SetBypassProxy(false)

	c := NewClient(20 * time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("绕过代理时应使用自定义直连 Transport，实得 %T", c.Transport)
	}
	if tr.Proxy != nil {
		t.Fatal("绕过代理时 Proxy 必须为 nil（否则仍会走环境代理）")
	}
	// 直连 Transport 不能是零值：必须带拨号/握手超时，否则大包下载更容易挂起。
	if tr.TLSHandshakeTimeout <= 0 {
		t.Fatal("直连 Transport 应带 TLS 握手超时")
	}
}

// TestDefaultClientUsesEnvironmentProxy 未绕过时使用默认 Transport
// （Go 读环境代理 = 系统代理），保持既有行为。
func TestDefaultClientUsesEnvironmentProxy(t *testing.T) {
	SetBypassProxy(false)
	c := NewClient(20 * time.Second)
	if c.Transport != nil {
		t.Fatalf("未绕过时应使用默认 Transport，实得 %T", c.Transport)
	}
	if c.Timeout != 20*time.Second {
		t.Fatalf("Timeout = %v，期望 20s", c.Timeout)
	}
}

// TestNewClientZeroTimeoutNoClientLimit 大文件下载必须没有客户端级超时
// （Timeout=0），由 ctx 与 Transport 层的握手超时兜底，否则 750MB 级下载会被掐断。
func TestNewClientZeroTimeoutNoClientLimit(t *testing.T) {
	SetBypassProxy(false)
	if c := NewClient(0); c.Timeout != 0 {
		t.Fatalf("Timeout = %v，期望 0（无客户端超时）", c.Timeout)
	}
}
