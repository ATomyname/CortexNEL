package relay

import (
	"net"
	"testing"

	"nexus/internal/telemetry"
)

// 函数名中的 AckWaits 会被占位标记模式误判，确认为误报。
func TestConfigurationFinishAckWaitsForServerFinish(t *testing.T) { // slop-ignore: code-placeholder
	c := NewConn("test", &Registry{}, nil, nil)
	c.SwitchState(StateConfiguration)
	c.AcknowledgeConfigurationFinish()
	if got := c.State(); got != StateConfiguration {
		t.Fatalf("ack before finish moved state to %v", got)
	}
	c.MarkConfigurationFinishSent()
	if got := c.State(); got != StatePlay {
		t.Fatalf("finish after ack left state at %v", got)
	}
}

func TestConfigurationCanRepeatAfterPlay(t *testing.T) {
	c := NewConn("test", &Registry{}, nil, nil)
	c.SwitchState(StatePlay)
	c.BeginConfiguration()
	if got := c.State(); got != StateConfiguration {
		t.Fatalf("begin configuration state = %v, want configuration", got)
	}
	c.MarkConfigurationFinishSent()
	c.AcknowledgeConfigurationFinish()
	if got := c.State(); got != StatePlay {
		t.Fatalf("second configuration ack state = %v, want play", got)
	}
}

// newPipedConn 构造双腿已装配的 Conn（net.Pipe 无真实网络，读错误即时返回）。
func newPipedConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	clientSide, relaySide := net.Pipe()
	c := NewConn("c1", &Registry{}, nil, log)
	c.client = &leg{name: "client", conn: relaySide, dir: ServerBound, c: c}
	c.server = &leg{name: "server", conn: clientSide, dir: ClientBound, c: c}
	return c, clientSide
}

// TestClientLegReadErrorCarriesOrigin 客户端腿读取终止必须携带来源与错误：
// 编排层据此发 session.client.lost 而非 upstream.lost（EVENTS.md §1）。
func TestClientLegReadErrorCarriesOrigin(t *testing.T) {
	c, clientSide := newPipedConn(t)
	disco := make(chan error, 1)
	c.OnDisconnect = func(*Conn, error) { _, err := c.LostLeg(); disco <- err }
	go c.client.readLoop()
	_ = clientSide.Close() // 触发 client 腿 Read 错误
	if err := <-disco; err == nil {
		t.Fatal("OnDisconnect 应携带 client 腿读取错误，实得 nil")
	}
	leg, err := c.LostLeg()
	if leg != "client" {
		t.Fatalf("LostLeg = %q, 期望 client", leg)
	}
	if err == nil {
		t.Fatal("LostLeg 错误为 nil")
	}
}

// TestServerLegReadErrorCarriesOrigin 上游腿同理。
func TestServerLegReadErrorCarriesOrigin(t *testing.T) {
	c, clientSide := newPipedConn(t)
	legName := make(chan string, 1)
	c.OnDisconnect = func(*Conn, error) { l, _ := c.LostLeg(); legName <- l }
	go c.server.readLoop()
	_ = clientSide.Close() // server 腿的对端断开 → server 腿 Read 错误
	if got := <-legName; got != "server" {
		t.Fatalf("LostLeg = %q, 期望 server", got)
	}
}

// TestLifecycleCloseHasNoOrigin 生命周期关闭（Stop 路径）不带来源：
// 编排层据此不发 lost 事件，只按会话结束处理。
func TestLifecycleCloseHasNoOrigin(t *testing.T) {
	c, _ := newPipedConn(t)
	legName := make(chan string, 1)
	c.OnDisconnect = func(*Conn, error) { l, _ := c.LostLeg(); legName <- l }
	c.Close()
	if got := <-legName; got != "" {
		t.Fatalf("生命周期关闭 LostLeg = %q, 期望空串", got)
	}
	if _, err := c.LostLeg(); err != nil {
		t.Fatalf("生命周期关闭错误应为 nil，实得 %v", err)
	}
}

// TestCloseFromLegIsIdempotent 双路径竞争关闭时先到先得，且 OnDisconnect 只触发一次。
func TestCloseFromLegIsIdempotent(t *testing.T) {
	c, _ := newPipedConn(t)
	calls := make(chan struct{}, 2)
	c.OnDisconnect = func(*Conn, error) { calls <- struct{}{} }
	readDone := make(chan struct{})
	go func() { defer close(readDone); c.client.readLoop() }()
	c.Close()
	<-calls
	select {
	case <-calls:
		t.Fatal("OnDisconnect 触发超过一次")
	default:
	}
	if leg, _ := c.LostLeg(); leg != "" {
		t.Fatalf("生命周期关闭先到应胜出，LostLeg = %q", leg)
	}
	// 等读取循环退出：其收尾日志仍在写文件，TempDir 清理不得与之竞争。
	<-readDone
}

// TestLostLegBeforeDisconnectNil 防御路径：未断开时 LostLeg 返回零值不 panic。
func TestLostLegBeforeDisconnectNil(t *testing.T) {
	c, _ := newPipedConn(t)
	if leg, err := c.LostLeg(); leg != "" || err != nil {
		t.Fatalf("活动连接 LostLeg = (%q, %v)，期望零值", leg, err)
	}
}
