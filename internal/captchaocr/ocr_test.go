package captchaocr

import (
	_ "embed"
	"testing"
)

//go:embed model/pre_xerf.bin
var preXerf []byte

// The plane is the PIL-processed real 4399 captcha
// (E:\Downloads\4399.txt.captcha.png → convert('L') → resize(120,40)).
// The Go forward pass was independently cross-checked against a numpy
// reimplementation of the same weights and produced identical argmax
// ("xerf") at every position.
func TestRecognizePlane(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	code, err := r.RecognizePlane(preXerf)
	if err != nil {
		t.Fatalf("RecognizePlane: %v", err)
	}
	if code != "xerf" {
		t.Fatalf("got %q, want xerf", code)
	}
	// determinism
	code2, _ := r.RecognizePlane(preXerf)
	if code2 != code {
		t.Fatalf("non-deterministic: %q vs %q", code, code2)
	}
}

func TestWeightsSanity(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// first-stage conv weights must be conventional small values
	fl := r.c1.w
	for i, v := range fl {
		if v != v || v > 2 || v < -2 {
			t.Fatalf("unexpected weight[%d]=%v", i, v)
		}
	}
}
