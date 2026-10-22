package stage

import (
	"bytes"
	"testing"
)

func bytesEqual(a, b []byte) bool { return bytes.Equal(a, b) }

func TestCompressorRoundTrip(t *testing.T) {
	c := &Compressor{Threshold: 256}

	small := []byte("tiny frame")
	enc, err := c.Encode(small)
	if err != nil {
		t.Fatalf("Encode small: %v", err)
	}
	if enc[0] != 0x00 {
		t.Fatalf("小于阈值应 dataLen=0 直通: % x", enc)
	}
	dec, err := c.Decode(enc)
	if err != nil || !bytes.Equal(dec, small) {
		t.Fatalf("small roundtrip: %v", err)
	}

	sizes := []int{1, 10, 127, 128, 129, 255, 256, 257, 512, 1024, 2048, 4096, 10000}
	for _, sz := range sizes {
		data := bytes.Repeat([]byte{byte(sz % 256)}, sz)
		enc, err := c.Encode(data)
		if err != nil {
			t.Fatalf("Encode size %d: %v", sz, err)
		}
		dec, err := c.Decode(enc)
		if err != nil {
			t.Fatalf("Decode size %d: %v", sz, err)
		}
		if !bytes.Equal(dec, data) {
			t.Fatalf("Payload mismatch at size %d", sz)
		}
	}
}
