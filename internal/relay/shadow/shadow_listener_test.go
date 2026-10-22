package shadow

import (
	"net"
	"testing"
	"time"

	"nexus/internal/relay"
	"nexus/internal/telemetry"
)

func TestPumpBUpstreamSignalsWhenViewerDisconnects(t *testing.T) {
	c := relay.NewConn("shadow-pump-test", &relay.Registry{}, nil, nil)
	peer, leg := net.Pipe()
	v := c.AttachViewer(leg, "shadow-test")
	defer v.Close()
	defer peer.Close()

	bDone := make(chan struct{})
	go (&ShadowListener{}).pumpBUpstream(&Attachment{}, v, make(chan struct{}), bDone)

	_ = peer.Close()
	select {
	case <-bDone:
	case <-time.After(time.Second):
		t.Fatal("B 断开后上行泵未通知 handleB")
	}
}

func TestPumpBUpstreamForwardsPlayFramesWhenSenderB(t *testing.T) {
	c, _, serverPeer := newRigConn(t)
	v, bPeer := attachShadowLeg(t, c)
	log, err := telemetry.New(nil, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	gate := NewGate(nil, KeepAliveConfig{ClientBoundID: -1, ServerBoundID: -1})
	gate.SetSender(SenderB)
	bDone := make(chan struct{})
	go (&ShadowListener{}).pumpBUpstream(&Attachment{Conn: c, Gate: gate, Log: log}, v, make(chan struct{}), bDone)

	writeShadowFrame(t, bPeer, 0x03, nil) // configuration acknowledge, never forwarded
	writeShadowFrame(t, bPeer, 0x0F, []byte("move"))
	id, payload := readShadowFrame(t, serverPeer, time.Second)
	if id != 0x0F || string(payload) != "move" {
		t.Fatalf("上游收到 (%d, %q)，期望 play 帧 (15, move)", id, payload)
	}
	_ = bPeer.Close()
	select {
	case <-bDone:
	case <-time.After(time.Second):
		t.Fatal("B 断开后上行泵未退出")
	}
}
