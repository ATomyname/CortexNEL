package heypixel

import (
	"bytes"
	"testing"

	"nexus/internal/relay/codec"
)

func TestTeamDropCollideRule(t *testing.T) {
	p := &setPlayerTeam{}
	w := codec.NewWriter()
	w.String("collideRule_blue")
	w.Fixed([]byte{0xAA, 0xBB})

	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	cancel, err := p.Handle(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cancel {
		t.Fatal("collideRule_ 队伍包应被丢弃")
	}
}

func TestTeamPassesNormalTeam(t *testing.T) {
	p := &setPlayerTeam{}
	w := codec.NewWriter()
	w.String("normal_team")
	w.Fixed([]byte{0xAA})

	if err := p.Read(nil, codec.NewReader(w.Bytes())); err != nil {
		t.Fatal(err)
	}
	cancel, err := p.Handle(nil)
	if err != nil || cancel {
		t.Fatalf("普通队伍包不应丢弃: cancel=%v err=%v", cancel, err)
	}
	out := codec.NewWriter()
	if err := p.Write(out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), w.Bytes()) {
		t.Fatal("普通队伍包应原样透传")
	}
}
