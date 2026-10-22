package clientcontrol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestFrameRoundTrip 分帧读写互逆，且长度前缀是小端。
func TestFrameRoundTrip(t *testing.T) {
	payload := JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "张三"}.frameFor(opJoinServerModern)

	var buf bytes.Buffer
	if err := writeFrame(&buf, payload); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(buf.Bytes()); int(got) != len(payload) {
		t.Fatalf("长度前缀 = %d，期望 %d", got, len(payload))
	}

	frame, err := readFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := frameOpcode(frame); got != opJoinServerModern {
		t.Fatalf("操作码 = %d，期望 %d", got, opJoinServerModern)
	}
	if !bytes.Equal(frame, payload) {
		t.Fatalf("载荷不符\n实得 %x\n期望 %x", frame, payload)
	}
	if !bytes.Equal(frameParams(frame), payload[2:]) {
		t.Fatalf("参数区不符\n实得 %x\n期望 %x", frameParams(frame), payload[2:])
	}
}

// TestReadFrameRejectsUndersizedFrame 帧长不足以容纳操作码时报错而不是越界。
func TestReadFrameRejectsUndersizedFrame(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x01, 0x00, 0xFF})
	if _, err := readFrame(buf); err == nil {
		t.Fatal("期望报错")
	}
}

// TestReadFrameRejectsTruncatedPayload 载荷短于长度前缀时报错。
func TestReadFrameRejectsTruncatedPayload(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0x08, 0x00, 0x01, 0x02})
	if _, err := readFrame(buf); err == nil {
		t.Fatal("期望报错")
	}
}

// TestPutStringIsLengthPrefixedUTF8 字符串编码 = uint16 字节长度 + UTF-8。
// 断言的是字节长度而非字符数：中文角色名每字 3 字节。
func TestPutStringIsLengthPrefixedUTF8(t *testing.T) {
	p := &packer{}
	p.putString("张三")
	got := p.payload()

	if n := binary.LittleEndian.Uint16(got); n != 6 {
		t.Fatalf("长度前缀 = %d，期望 6（UTF-8 字节数）", n)
	}
	if string(got[2:]) != "张三" {
		t.Fatalf("内容 = %q，期望 %q", got[2:], "张三")
	}
}
