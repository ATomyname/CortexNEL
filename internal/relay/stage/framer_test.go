package stage

import (
	"errors"
	"testing"

	"nexus/internal/relay/codec"
)

func TestFramerSplitAndNeedMore(t *testing.T) {
	w := codec.NewWriter()
	w.VarInt(3)
	w.Fixed([]byte{1, 2, 3})
	frame := w.Bytes()

	f := &Framer{}
	f.Push(frame[:2])
	if _, err := f.Next(); !errors.Is(err, ErrNeedMore) {
		t.Fatalf("半帧应返回 ErrNeedMore: %v", err)
	}
	f.Push(frame[2:])
	got, err := f.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if !bytesEqual(got, []byte{1, 2, 3}) {
		t.Fatalf("帧体 = %v", got)
	}
	if _, err := f.Next(); !errors.Is(err, ErrNeedMore) {
		t.Fatalf("空缓冲应返回 ErrNeedMore: %v", err)
	}
}

func TestFramerRejectsOversize(t *testing.T) {
	w := codec.NewWriter()
	w.VarInt(codec.MaxFramePayload + 1)
	f := &Framer{}
	f.Push(w.Bytes())
	if _, err := f.Next(); !errors.Is(err, codec.ErrStringTooLong) {
		t.Fatalf("超限帧应拒绝: %v", err)
	}
}
