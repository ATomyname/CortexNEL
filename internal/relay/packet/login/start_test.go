package login

import (
	"testing"

	"nexus/internal/relay"
	"nexus/internal/relay/codec"
)

func TestLoginStartRewritesNamePreservingTail(t *testing.T) {
	r := codec.NewReader([]byte{3, 'o', 'l', 'd', 1, 2, 3})
	p := &loginStart{}
	if err := p.Read(nil, r); err != nil {
		t.Fatal(err)
	}
	c := relay.NewConn("test", nil, nil, nil)
	c.LoginName = "role"
	if cancel, err := p.Handle(c); err != nil || cancel {
		t.Fatalf("handle login start: cancel=%v err=%v", cancel, err)
	}
	w := codec.NewWriter()
	if err := p.Write(w); err != nil {
		t.Fatal(err)
	}
	want := []byte{4, 'r', 'o', 'l', 'e', 1, 2, 3}
	if string(w.Bytes()) != string(want) {
		t.Fatalf("encoded login start = %v, want %v", w.Bytes(), want)
	}
}
