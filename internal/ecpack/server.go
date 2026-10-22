// 本地分发：把服务端进服下发的资源包通过 127.0.0.1 上的 HTTP 原样交付给镜像客户端。
package ecpack

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// PackPath 是客户端拉取资源包使用的固定路径。
const PackPath = "/ecpack.zip"

// Server 是本机资源包分发服务。
//
// 只绑定回环地址：镜像客户端与 relay 同机，没有理由把包暴露到局域网。
type Server struct {
	mu       sync.RWMutex
	zipPath  string
	url      string
	hash     string
	ready    bool
	listener net.Listener
	server   *http.Server
}

// Start 在回环地址上启动分发服务（addr 传 "127.0.0.1:0" 由系统分配端口）。
func Start(addr string) (*Server, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ecpack: 监听失败: %w", err)
	}
	s := &Server{listener: ln}
	mux := http.NewServeMux()
	mux.HandleFunc(PackPath, s.handle)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.server.Serve(ln) }()
	return s, nil
}

// Publish 登记当前要交付的资源包（zip 路径 + 内容 sha1），并返回客户端可用的 URL。
func (s *Server) Publish(zipPath, sum string) (string, error) {
	info, err := os.Stat(zipPath)
	if err != nil {
		return "", fmt.Errorf("ecpack: 资源包不存在: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("ecpack: 资源包路径是目录: %s", zipPath)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zipPath = zipPath
	s.hash = sum
	if s.url == "" {
		s.url = fmt.Sprintf("http://%s%s", s.listener.Addr().String(), PackPath)
	}
	s.ready = true
	return s.url, nil
}

// PackURL 实现 relay 侧 ecpack.Provider：返回本地分发地址与内容 hash。
// 未登记资源包时 ok=false，relay 会把服务端资源包原样透传（不介入）。
func (s *Server) PackURL() (string, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.ready {
		return "", "", false
	}
	return s.url, s.hash, true
}

// Close 关闭分发服务。
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.mu.Lock()
	s.ready = false
	s.mu.Unlock()
	return s.server.Shutdown(ctx)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	zipPath, sum := s.zipPath, s.hash
	ready := s.ready
	s.mu.RUnlock()
	if !ready {
		http.NotFound(w, r)
		return
	}
	// hash 交给客户端校验，这里如实回传，便于客户端与实际内容对不上时立刻失败。
	if sum != "" {
		w.Header().Set("X-Content-SHA1", sum)
	}
	http.ServeFile(w, r, zipPath)
}
