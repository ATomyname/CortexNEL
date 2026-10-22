package relay

// viewer_test.go — 职责: 影子腿（Viewer）的分流、直写、读取与溢出语义。

import (
	"bytes"
	"net"
	"testing"
	"time"

	"nexus/internal/relay/codec"
	"nexus/internal/relay/stage"
)

// attachViewer 在夹具会话上挂一条影子腿，返回该腿与「影子端一侧」的连接。
func attachViewer(t *testing.T, s *seamConn) (*Viewer, net.Conn) {
	t.Helper()
	peer, legEnd := net.Pipe()
	t.Cleanup(func() { _ = peer.Close(); _ = legEnd.Close() })
	v := s.c.AttachViewer(legEnd, "shadow")
	t.Cleanup(v.Close)
	return v, peer
}

// readFramedPacket 从影子端一侧读一帧并解出 (包 id, 载荷)。
// 用引擎自己的 stage.Framer 解帧，避免测试自带一套解析器。
func readFramedPacket(t *testing.T, conn net.Conn) (int32, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var framer stage.Framer
	buf := make([]byte, 4096)
	for {
		body, err := framer.Next()
		if err == nil {
			id, payload, serr := splitPacket(body)
			if serr != nil {
				t.Fatalf("拆包: %v", serr)
			}
			return id, payload
		}
		if err != stage.ErrNeedMore {
			t.Fatalf("解帧: %v", err)
		}
		n, rerr := conn.Read(buf)
		if n > 0 {
			framer.Push(buf[:n])
		}
		if rerr != nil {
			t.Fatalf("影子端未收到帧: %v", rerr)
		}
	}
}

// frameBytes 用引擎自己的组帧实现把明文帧体加长度前缀。
func frameBytes(body []byte) []byte { return stage.WriteFrame(body) }

// TestViewerReceivesFanout S2C 帧必须同时到达主客户端与影子腿。
func TestViewerReceivesFanout(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)

	go func() { _ = s.c.dispatch(s.c.server, 7, []byte("world")) }()

	// 主客户端一侧。
	if got := readWithDeadline(t, s.toClient, time.Second); got == nil || !bytes.Contains(got, []byte("world")) {
		t.Fatalf("主客户端未收到帧：% X", got)
	}
	// 影子端一侧。
	id, payload := readFramedPacket(t, shadowPeer)
	if id != 7 || !bytes.Contains(payload, []byte("world")) {
		t.Fatalf("影子端收到 (%d, %q)，期望 (7, world)", id, payload)
	}
	if len(s.c.Viewers()) != 1 {
		t.Fatalf("影子腿数 = %d，期望 1", len(s.c.Viewers()))
	}
	_ = v
}

// TestViewerPolicyOverridesFanout 策略非 nil 时由策略接管（影子端可用它做重放期缓冲）。
func TestViewerPolicyOverridesFanout(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)

	got := make(chan []byte, 4)
	v.SetFramePolicy(func(id int32, payload []byte) {
		got <- append([]byte(nil), payload...)
	})

	go func() { _ = s.c.dispatch(s.c.server, 9, []byte("buffered")) }()
	_ = readWithDeadline(t, s.toClient, time.Second)

	select {
	case p := <-got:
		if !bytes.Contains(p, []byte("buffered")) {
			t.Fatalf("策略收到 %q", p)
		}
	case <-time.After(time.Second):
		t.Fatal("策略未被调用")
	}
	// 策略接管后不应再直写该腿。
	_ = shadowPeer.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
	if n, err := shadowPeer.Read(make([]byte, 64)); err == nil {
		t.Fatalf("策略接管时不应直写影子腿，实得 %d 字节", n)
	}
}

// TestViewerWritePacketReachesShadowSide 重放用的直写路径必须把帧送到影子端。
func TestViewerWritePacketReachesShadowSide(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)

	if err := v.WritePacket(42, []byte("replay")); err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
	id, payload := readFramedPacket(t, shadowPeer)
	if id != 42 || !bytes.Contains(payload, []byte("replay")) {
		t.Fatalf("影子端收到 (%d, %q)，期望 (42, replay)", id, payload)
	}
}

// TestViewerReadPacketReadsShadowUpstream 影子端发来的帧必须能被读出（B 的 C2S 入口）。
func TestViewerReadPacketReadsShadowUpstream(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)

	// 影子端一侧写一帧（明文帧体：varint id + 载荷，经长度前缀）。
	body := codec.NewWriter()
	body.VarInt(3)
	body.Fixed([]byte("jump"))
	go func() { _, _ = shadowPeer.Write(frameBytes(body.Bytes())) }()

	id, payload, err := v.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if id != 3 || string(payload) != "jump" {
		t.Fatalf("读到 (%d, %q)，期望 (3, jump)", id, payload)
	}
}

func TestViewerReadPacketDecodesCompressedShadowUpstream(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)
	v.SetCompression(256)

	body := codec.NewWriter()
	body.VarInt(3)
	body.Fixed([]byte("ack"))
	comp, err := (&stage.Compressor{Threshold: 256}).Encode(body.Bytes())
	if err != nil {
		t.Fatalf("压缩上行帧: %v", err)
	}
	go func() { _, _ = shadowPeer.Write(stage.WriteFrame(comp)) }()

	id, payload, err := v.ReadPacket()
	if err != nil {
		t.Fatalf("ReadPacket: %v", err)
	}
	if id != 3 || string(payload) != "ack" {
		t.Fatalf("读到压缩帧 (%d, %q)，期望 (3, ack)", id, payload)
	}
}

// TestViewerDetachStopsFanout 摘掉影子腿后不再分流，且该腿被关闭。
func TestViewerDetachStopsFanout(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s)
	s.c.DetachViewer(v)

	if !v.Closed() {
		t.Fatal("摘掉后影子腿应被关闭")
	}
	if len(s.c.Viewers()) != 0 {
		t.Fatalf("影子腿数 = %d，期望 0", len(s.c.Viewers()))
	}

	go func() { _ = s.c.dispatch(s.c.server, 1, []byte("after")) }()
	_ = readWithDeadline(t, s.toClient, time.Second)
	_ = shadowPeer.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
	if n, err := shadowPeer.Read(make([]byte, 64)); err == nil {
		t.Fatalf("摘掉后不应再分流，实得 %d 字节", n)
	}
}

// TestViewerSlowClientDoesNotBlockMainLeg 影子腿跟不上时只关它自己，主链路必须继续
// （白端必须活着：它是会话与上报的承载者）。
func TestViewerSlowClientDoesNotBlockMainLeg(t *testing.T) {
	s := newSeamConn(t, &Registry{})
	v, shadowPeer := attachViewer(t, s) // 影子端一侧不读，队列会满
	defer shadowPeer.Close()

	// 用策略直接塞爆队列（策略接管时不走队列）——改为走默认直写路径并压满队列。
	v.SetFramePolicy(nil)
	for i := 0; i < viewerQueue+8; i++ {
		if v.Closed() {
			break
		}
		s.c.fanoutViewers(int32(i), []byte("x"))
	}
	if !v.Closed() {
		t.Fatal("队列溢出后影子腿应被关闭")
	}
	if !v.Overflowed() {
		t.Fatal("溢出标记未置位")
	}

	// 主链路仍必须可用。
	done := make(chan []byte, 1)
	go func() { done <- readWithDeadline(t, s.toClient, 2*time.Second) }()
	go func() { _ = s.c.dispatch(s.c.server, 5, []byte("alive")) }()
	select {
	case got := <-done:
		if got == nil || !bytes.Contains(got, []byte("alive")) {
			t.Fatalf("主链路被影子端拖死：% X", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("主链路超时")
	}
}
