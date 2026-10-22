package heypixel

import (
	"bytes"
	"testing"

	"nexus/internal/relay/codec"
)

func TestBrandRewriteToForge(t *testing.T) {
	p := &brandPluginMessage{}
	w := codec.NewWriter()
	w.String("minecraft:brand")
	w.ByteArray([]byte{0x07, 'v', 'a', 'n', 'i', 'l', 'l', 'a'})

	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	cancel, err := p.Handle(nil)
	if err != nil || cancel {
		t.Fatalf("Handle: cancel=%v err=%v", cancel, err)
	}
	out := codec.NewWriter()
	if err := p.Write(out); err != nil {
		t.Fatal(err)
	}
	r := codec.NewReader(out.Bytes())
	ch, err := r.String()
	if err != nil || ch != "minecraft:brand" {
		t.Fatalf("channel = %q err=%v", ch, err)
	}
	payload, err := r.ByteArray()
	if err != nil {
		t.Fatal(err)
	}
	// brandForge = MC string 帧（0x05 长度前缀 + "forge"），ByteArray 解码后即字符串本体
	//（与 Nirvana 的 base64 "BWZvcmdl" 语义一致）。
	if string(payload) != "forge" {
		t.Fatalf("brand 载荷 = %q, want %q", payload, "forge")
	}
}

func TestBrandPassesOtherChannels(t *testing.T) {
	p := &brandPluginMessage{}
	w := codec.NewWriter()
	w.String("forge:something")
	w.ByteArray([]byte{1, 2})
	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(nil); err != nil {
		t.Fatal(err)
	}
	// Read 以 Rest() 保存原始载荷（含 varint 长度前缀），透传语义下不应被改写。
	if !bytes.Equal(p.payload, []byte{0x02, 1, 2}) {
		t.Fatalf("非 brand 通道的载荷不应被改写: % x", p.payload)
	}
}
