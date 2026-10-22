package clientcontrol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestJoinFrameModernBytes 锁定高版本进服指令的线路字节。
func TestJoinFrameModernBytes(t *testing.T) {
	target := JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve", MCVersion: "1.20.1"}
	got := target.frameFor(opJoinServerModern)

	var want bytes.Buffer
	want.Write(le16(opJoinServerModern))
	want.Write(le16(uint16(len("127.0.0.1"))))
	want.WriteString("127.0.0.1")
	want.Write(le32(25565))
	want.Write(le16(uint16(len("Steve"))))
	want.WriteString("Steve")

	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("进服帧字节不符\n实得 %x\n期望 %x", got, want.Bytes())
	}
}

// TestJoinFrameLegacyHasTrailingBool 低版本帧末尾多一个 false。
func TestJoinFrameLegacyHasTrailingBool(t *testing.T) {
	target := JoinTarget{Host: "127.0.0.1", Port: 25565, RoleName: "Steve", MCVersion: "1.12.2"}
	modern := target.frameFor(opJoinServerModern)
	legacy := target.frameFor(opJoinServerLegacy)

	if len(legacy) != len(modern)+1 {
		t.Fatalf("低版本帧长 %d，期望比高版本多 1 字节（%d）", len(legacy), len(modern)+1)
	}
	if legacy[len(legacy)-1] != 0 {
		t.Fatalf("末位 = %d，期望 0（false）", legacy[len(legacy)-1])
	}
	if binary.LittleEndian.Uint16(legacy) != opJoinServerLegacy {
		t.Fatalf("操作码 = %d，期望 %d", binary.LittleEndian.Uint16(legacy), opJoinServerLegacy)
	}
}

// TestUsesModernJoinBoundary 1.18 不算高版本，1.18.1 起才算（真源是严格大于 1.18）。
func TestUsesModernJoinBoundary(t *testing.T) {
	cases := map[string]bool{
		"1.12.2": false,
		"1.18":   false,
		"1.18.1": true,
		"1.18.2": true,
		"1.20.1": true,
	}
	for version, want := range cases {
		got := JoinTarget{MCVersion: version}.usesModernJoin()
		if got != want {
			t.Errorf("usesModernJoin(%q) = %v，期望 %v", version, got, want)
		}
	}
}

func le16(v uint16) []byte {
	out := make([]byte, 2)
	binary.LittleEndian.PutUint16(out, v)
	return out
}

func le32(v int32) []byte {
	out := make([]byte, 4)
	binary.LittleEndian.PutUint32(out, uint32(v))
	return out
}
