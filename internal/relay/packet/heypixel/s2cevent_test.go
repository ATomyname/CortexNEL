package heypixel

import (
	"bytes"
	"encoding/json"
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

func TestAppendUvarintEvidence(t *testing.T) {
	// 字节级证据：c0877 fn3 实测（forge_registry.txt）
	cases := map[uint32]string{9: "09", 300: "ac02", 0x7FFFFFFF: "ffffffff07", 1: "01", 0: "00"}
	for v, hex := range cases {
		got := AppendUvarint(nil, v)
		if fmt := fmtHex(got); fmt != hex {
			t.Fatalf("uvarint(%d) = %s, want %s", v, fmt, hex)
		}
		back, off, err := ParseUvarint(got, 0)
		if err != nil || back != v || off != len(got) {
			t.Fatalf("roundtrip %d failed: %d %v", v, back, err)
		}
	}
}

func TestBuildFramesSingle(t *testing.T) {
	frames := BuildFrames(PktMapReport, []byte(`{"a":1}`), 7)
	if len(frames) != 1 {
		t.Fatalf("frames=%d want 1", len(frames))
	}
	id, rest, err := ParseFrame(frames[0])
	if err != nil || id != PktMapReport || string(rest) != `{"a":1}` {
		t.Fatalf("parse=%d %q %v", id, rest, err)
	}
}

func TestBuildFramesFragmented(t *testing.T) {
	payload := bytes.Repeat([]byte{0x41}, 70000) // > 30000 → 3 片
	frames := BuildFrames(PktHexClick, payload, 42)
	if len(frames) != 3 {
		t.Fatalf("frames=%d want 3", len(frames))
	}
	total := 1 + len(payload) // uvarint(9) + payload
	for i, f := range frames {
		magic, off, err := ParseUvarint(f, 0)
		if err != nil || magic != fragMagic {
			t.Fatalf("frag %d magic=%x err=%v", i, magic, err)
		}
		ver, off, _ := ParseUvarint(f, off)
		sess, off, _ := ParseUvarint(f, off)
		tot, off, _ := ParseUvarint(f, off)
		idx, off, _ := ParseUvarint(f, off)
		cnt, off, _ := ParseUvarint(f, off)
		ln, off, _ := ParseUvarint(f, off)
		if ver != fragVersion || sess != 42 || tot != uint32(total) || idx != uint32(i) || cnt != 3 {
			t.Fatalf("frag %d header v=%d s=%d tot=%d idx=%d cnt=%d", i, ver, sess, tot, idx, cnt)
		}
		if ln != uint32(len(f)-off) {
			t.Fatalf("frag %d chunkLen=%d body=%d", i, ln, len(f)-off)
		}
	}
	// 还原校验：分片体拼接应等于原始帧
	var joined []byte
	frame0 := AppendUvarint(nil, uint32(PktHexClick))
	frame0 = append(frame0, payload...)
	for _, f := range frames {
		off := 0
		for i := 0; i < 7; i++ {
			_, off, _ = ParseUvarint(f, off)
		}
		joined = append(joined, f[off:]...)
	}
	if !bytes.Equal(joined, frame0) {
		t.Fatal("fragment reassembly mismatch")
	}
}

func TestPayloadHexClick(t *testing.T) {
	p := PayloadHexClick(0x1234, "click", 3, "k")
	var m map[string]any
	if err := json.Unmarshal(p, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"sessionId", "actionType", "slotIndex", "key"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("missing key %s in %s", k, p)
		}
	}
}

func TestS2cSyncPassthrough(t *testing.T) {
	// 非 s2cevent 通道：Read → Handle(nil) → Write 字节不变
	w := codec.NewWriter()
	w.String("minecraft:brand")
	w.Fixed([]byte{0x05, 'f', 'o', 'r', 'g', 'e'})
	src := w.Bytes()
	h := &s2cSyncHandler{}
	if err := h.Read(nil, codec.NewReader(src)); err != nil {
		t.Fatal(err)
	}
	if h.channel != "minecraft:brand" {
		t.Fatalf("channel=%s", h.channel)
	}
	cancel, err := h.Handle(nil)
	if cancel || err != nil {
		t.Fatalf("cancel=%v err=%v", cancel, err)
	}
	out := codec.NewWriter()
	if err := h.Write(out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), src) {
		t.Fatalf("passthrough mismatch: %x vs %x", out.Bytes(), src)
	}
}

func TestS2cMirrorClientGated(t *testing.T) {
	// 仅代理门控：白端（MirrorClient=true）会话的 s2cevent 帧必须原样透传，
	// 不审计、不捕获、不伪装注入（白端原生上报）。
	c := relay.NewConn("t", nil, nil, nil)
	c.MirrorClient = true
	w := codec.NewWriter()
	w.String(s2ceventChannel)
	w.Fixed([]byte{0x01, 0x02})
	src := w.Bytes()
	h := &s2cSyncHandler{}
	if err := h.Read(c, codec.NewReader(src)); err != nil {
		t.Fatal(err)
	}
	cancel, err := h.Handle(c)
	if cancel || err != nil {
		t.Fatalf("cancel=%v err=%v", cancel, err)
	}
	out := codec.NewWriter()
	if err := h.Write(out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), src) {
		t.Fatalf("mirror passthrough mismatch: %x vs %x", out.Bytes(), src)
	}
}

func TestForgeDisabledNoSideEffect(t *testing.T) {
	// ParseFrame 对未知 id 不做校验（枚举在发送侧），仅解析
	id, rest, err := ParseFrame(AppendUvarint(nil, 108))
	if err != nil || id != 108 || len(rest) != 0 {
		t.Fatalf("id=%d rest=%d err=%v", id, len(rest), err)
	}
	_ = relay.VersionAny
}

func fmtHex(b []byte) string {
	const hexd = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, hexd[c>>4], hexd[c&0x0F])
	}
	return string(out)
}
