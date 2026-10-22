// Package httpx 提供统一构造 HTTP 客户端的入口，并持有"是否绕过系统代理"的全局策略。
//
// 为什么需要：nexus 依赖网易线上服务，而 Go 的默认 Transport 会读取环境变量里的
// 代理（HTTP_PROXY / HTTPS_PROXY，即系统代理），代理抖动会直接表现为 TLS 握手超时。
// 系统设置里的「绕过系统代理」开关由 app 在启动与保存设置时写入本包；所有经
// NewClient 构造的客户端据此决定直连还是走代理。本包不承载任何业务状态。
package httpx

import (
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

var bypass atomic.Bool

// SetBypassProxy 更新全局代理策略：true = 绕过系统代理直连。
// 每个客户端都在构造时读取当前策略，因此运行中的开关在下次网络操作自动生效。
func SetBypassProxy(v bool) { bypass.Store(v) }

// BypassProxy 返回当前是否绕过系统代理。
func BypassProxy() bool { return bypass.Load() }

// NewClient 构造 HTTP 客户端。
//
// timeout <= 0 表示不给客户端级请求超时（依赖 ctx 与 Transport 层的握手超时）——
// 大文件下载（750MB 级）必须走这条路，给一个固定超时会半途断掉。
// 绕过代理：直连 Transport（Proxy: nil，吃不到环境代理变量）。直连 Transport 不
// 能是零值 —— 零值没有默认拨号/握手超时与连接复用，比 http.DefaultTransport 更慢
// 更容易挂起，这里补齐与默认值对齐的字段。
// 否则：默认 Transport（Go 读环境代理，即系统代理）。
func NewClient(timeout time.Duration) *http.Client {
	if bypass.Load() {
		return &http.Client{Timeout: timeout, Transport: directTransport()}
	}
	return &http.Client{Timeout: timeout}
}

// directTransport 返回一个直连、行为对齐 http.DefaultTransport 的 Transport。
func directTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
