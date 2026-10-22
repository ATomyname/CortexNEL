package heypixel

import (
	"testing"

	"nexus/internal/relay/codec"
)

func TestFMLHandshakeAppendsSuffix(t *testing.T) {
	p := &fmlHandshake{}
	w := codec.NewWriter()
	w.VarInt(763)
	w.String("www.relink.vip")
	w.UShort(25565)
	w.VarInt(2)

	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatalf("Read: %v", err)
	}
	cancel, err := p.Handle(nil)
	if err != nil || cancel {
		t.Fatalf("Handle: cancel=%v err=%v", cancel, err)
	}
	out := codec.NewWriter()
	if err := p.Write(out); err != nil {
		t.Fatalf("Write: %v", err)
	}
	r := codec.NewReader(out.Bytes())
	if _, err := r.VarInt(); err != nil {
		t.Fatal(err)
	}
	host, err := r.String()
	if err != nil {
		t.Fatal(err)
	}
	if host != "www.relink.vip\x00FML3\x00" {
		t.Fatalf("host = %q", host)
	}
}

func TestFMLHandshakeKeepsExistingSuffix(t *testing.T) {
	p := &fmlHandshake{}
	w := codec.NewWriter()
	w.VarInt(763)
	w.String("www.relink.vip\x00FML3\x00")
	w.UShort(25565)
	w.VarInt(2)

	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Handle(nil); err != nil {
		t.Fatal(err)
	}
	if p.host != "www.relink.vip\x00FML3\x00" {
		t.Fatalf("已有标记被改动: %q", p.host)
	}
}
