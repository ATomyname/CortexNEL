package netease

import (
	"bytes"
	"net"
	"testing"
)

func TestChaChaFrameRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x77}, 32)
	payload := []byte("join payload bytes")
	frame := packChaChaFrame(9, payload, key)

	// 长度前缀明文，其余加密。
	if frame[6] == 9 {
		t.Fatal("type 字节应处于密文区")
	}
	gotType, gotPayload, err := unpackChaChaFrame(frame[2:], key)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if gotType != 9 {
		t.Fatalf("type = %d", gotType)
	}
	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload = %q", gotPayload)
	}
}

func TestChaChaFrameRejectsTamperedCRC(t *testing.T) {
	key := bytes.Repeat([]byte{0x77}, 32)
	frame := packChaChaFrame(9, []byte("x"), key)
	frame[len(frame)-1] ^= 0xFF
	if _, _, err := unpackChaChaFrame(frame[2:], key); err == nil {
		t.Fatal("篡改载荷应触发 CRC 失败")
	}
}

func TestLEFrameRoundTripOnPipe(t *testing.T) {
	// readLEFrame/writeLEFrame 用 net.Pipe 验证（无真实网络依赖）。
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- writeLEFrame(server, []byte("hello frame"))
	}()
	body, err := readLEFrame(client)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, []byte("hello frame")) {
		t.Fatalf("body = %q", body)
	}
}
