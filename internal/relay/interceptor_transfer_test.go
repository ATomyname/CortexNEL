// interceptor_transfer_test.go — 职责: 守护 Interceptor 对转服缝的装配 ——
// conn.RelayAddr 必须取客户端实际拨号的本地地址（改写 Transfer 包的回连目标），
// conn.OnTransferRequest 必须接通 Acceptance 提供的回调。接线断了，转服拦截
// 会静默退化为原样转发（不比没有拦截的旧故障好多少）。
package relay

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"nexus/internal/telemetry"
)

func newTransferSeamLogger(t *testing.T) *telemetry.Logger {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

// TestInterceptorWiresTransferSeam 走真实 handleClient 装配路径：
// 假上游 + 假客户端各占一个内存 socket，断言两条缝都接通。
func TestInterceptorWiresTransferSeam(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// 假上游必须**持有**接受的连接：立即关闭会让 server 腿读到 EOF，
	// conn 被从 conns 表摘除，后续断言就不成立了。
	var heldMu sync.Mutex
	var held []net.Conn
	go func() {
		for {
			conn, err := upstream.Accept()
			if err != nil {
				return
			}
			heldMu.Lock()
			held = append(held, conn)
			heldMu.Unlock()
		}
	}()
	t.Cleanup(func() {
		heldMu.Lock()
		for _, c := range held {
			_ = c.Close()
		}
		heldMu.Unlock()
		_ = upstream.Close()
	})

	recorded := make(chan string, 1)
	i := NewInterceptor("127.0.0.1:0", &Registry{}, telemetry.NewBus(), newTransferSeamLogger(t),
		func(net.Conn) (*Acceptance, error) {
			return &Acceptance{
				ID:           "transfer-seam",
				UpstreamAddr: upstream.Addr().String(),
				OnTransferRequest: func(_ *Conn, addr string) {
					recorded <- addr
				},
			}, nil
		})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = i.ListenAndServe(ctx) }()
	t.Cleanup(i.Stop)

	client, err := net.Dial("tcp", waitListenerAddr(t, i))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	var conn *Conn
	deadline := time.Now().Add(2 * time.Second)
	for conn == nil {
		if conn = i.ConnByID("transfer-seam"); conn != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("2s 内未完成会话装配")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// RelayAddr = 客户端拨号的本机地址（accepted 连接的 LocalAddr == 客户端的 RemoteAddr）。
	if conn.RelayAddr == "" {
		t.Fatal("conn.RelayAddr 未装配：Transfer 包将无法改写回连目标")
	}
	if conn.RelayAddr != client.RemoteAddr().String() {
		t.Fatalf("RelayAddr = %s，要客户端实际拨号地址 %s", conn.RelayAddr, client.RemoteAddr().String())
	}

	// OnTransferRequest 必须接通 Acceptance 回调。
	conn.OnTransferRequest(conn, "game.example.net:25566")
	select {
	case got := <-recorded:
		if got != "game.example.net:25566" {
			t.Fatalf("OnTransferRequest 透传目标 = %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnTransferRequest 未接通 Acceptance 回调")
	}
}

// waitListenerAddr 轮询等待监听就绪并返回实际地址。
func waitListenerAddr(t *testing.T, i *Interceptor) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if addr := i.Addr(); addr != nil {
			return addr.String()
		}
		if time.Now().After(deadline) {
			t.Fatal("2s 内监听未就绪")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
