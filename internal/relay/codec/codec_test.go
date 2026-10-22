package codec

import (
	"bytes"
	"errors"
	"testing"
)

func TestVarIntRoundTrip(t *testing.T) {
	cases := []int32{0, 1, 127, 128, 255, 25565, 2097151, -1, -2147483648}
	for _, v := range cases {
		w := NewWriter()
		w.VarInt(v)
		r := NewReader(w.Bytes())
		got, err := r.VarInt()
		if err != nil {
			t.Fatalf("VarInt(%d) 读回失败: %v", v, err)
		}
		if got != v {
			t.Fatalf("VarInt = %d, want %d", got, v)
		}
	}
}

func TestVarIntRejectsFiveByteContinuation(t *testing.T) {
	// 5 字节全带续位 → 必须报错（facts §7.3：varint ≤5B）。
	r := NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80})
	if _, err := r.VarInt(); !errors.Is(err, ErrVarIntTooBig) {
		t.Fatalf("err = %v, want ErrVarIntTooBig", err)
	}
}

func TestStringRoundTripAndTruncation(t *testing.T) {
	w := NewWriter()
	w.String("布吉岛中国版")
	r := NewReader(w.Bytes())
	got, err := r.String()
	if err != nil {
		t.Fatalf("String: %v", err)
	}
	if got != "布吉岛中国版" {
		t.Fatalf("String = %q", got)
	}

	cut := w.Bytes()[:w.Len()-1]
	if _, err := NewReader(cut).String(); !errors.Is(err, ErrTruncated) {
		t.Fatalf("截断 err = %v, want ErrTruncated", err)
	}
}

func TestByteArrayAndUShort(t *testing.T) {
	w := NewWriter()
	w.ByteArray([]byte{1, 2, 3})
	w.UShort(25565)
	r := NewReader(w.Bytes())
	arr, err := r.ByteArray()
	if err != nil {
		t.Fatalf("ByteArray: %v", err)
	}
	if !bytes.Equal(arr, []byte{1, 2, 3}) {
		t.Fatalf("ByteArray = %v", arr)
	}
	p, err := r.UShort()
	if err != nil || p != 25565 {
		t.Fatalf("UShort = %d, %v", p, err)
	}
}
