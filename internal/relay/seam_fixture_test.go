package relay

// seam_fixture_test.go — 职责: 接缝测试夹具（双腿可控 Conn + 测试处理器桩）。
// 只放夹具与桩，不放断言（断言在 seam_test.go）。

import (
	"net"
	"testing"
	"time"

	"nexus/internal/relay/codec"
	"nexus/internal/telemetry"
)

// seamConn 是双腿可控的测试夹具：两侧各接一条 net.Pipe，
// 便于分别观察「客户端会读到什么」与「上游会收到什么」。
type seamConn struct {
	c        *Conn
	toClient net.Conn // 读它 = 看客户端收到的字节
	toServer net.Conn // 读它 = 看上游收到的字节
}

func newSeamConn(t *testing.T, rg *Registry) *seamConn {
	t.Helper()
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })

	clientPeer, clientLeg := net.Pipe()
	serverPeer, serverLeg := net.Pipe()

	c := NewConn("seam", rg, nil, log)
	c.client = &leg{name: "client", conn: clientLeg, dir: ServerBound, c: c}
	c.server = &leg{name: "server", conn: serverLeg, dir: ClientBound, c: c}
	c.SwitchState(StatePlay)

	t.Cleanup(func() {
		_ = clientPeer.Close()
		_ = serverPeer.Close()
		_ = clientLeg.Close()
		_ = serverLeg.Close()
	})
	return &seamConn{c: c, toClient: clientPeer, toServer: serverPeer}
}

// readWithDeadline 在读不到数据时返回 nil（用于断言「什么都没写」）。
func readWithDeadline(t *testing.T, conn net.Conn, d time.Duration) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return nil
	}
	return buf[:n]
}

// rewriteStub 是测试处理器：把载荷替换为固定字节，用于验证 tap 拿到的是「最终写出字节」。
type rewriteStub struct{ NoForward }

func (p *rewriteStub) Read(_ *Conn, r *codec.Reader) error {
	_, err := r.VarInt() // 与真实包体形状无关：只消费一个 varint 便于构造
	return err
}

func (p *rewriteStub) Write(w *codec.Writer) error {
	w.Fixed([]byte("REWRITTEN"))
	return nil
}

// cancelStub 是测试处理器：丢弃该包。
type cancelStub struct{ NoForward }

func (p *cancelStub) Handle(*Conn) (bool, error) { return true, nil }

// registryWith 构造只含一个 Play/ClientBound/id=5 处理器的注册表。
func registryWith(stub Packet) *Registry {
	rg := &Registry{}
	rg.Register(Registered{
		Key:     PacketKey{State: StatePlay, Dir: ClientBound, PacketID: 5},
		Factory: func() Packet { return stub },
	})
	return rg
}
