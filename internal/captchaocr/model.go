// CaptchaModel: pure-Go inference for the 4399register CNN
// (4×conv+BN+ReLU(+maxpool) → adaptive avgpool (1,4) → 2×linear),
// bit-faithful to the PyTorch module it was exported from.
package captchaocr

import (
	_ "embed"
	"fmt"
	"math"
)

//go:embed model/captcha_model.pth
var modelPth []byte

const (
	imgW       = 120
	imgH       = 40
	captchaLen = 4
	numClasses = 37 // 36 chars + blank(36), blank unused at inference
	Charset    = "abcdefghijklmnopqrstuvwxyz0123456789"
)

type convBlock struct {
	w   []float32 // [out,in,kh,kw]
	bnW []float32
	bnB []float32
	mu  []float32
	va  []float32
}

type linear struct {
	w []float32 // [out,in]
	b []float32
}

// Recognizer holds the loaded weights.
type Recognizer struct {
	c1, c2, c3, c4 convBlock
	l1, l2         linear
	LastLogits     [][]float32 // per-position score rows (debug)
}

func New() (*Recognizer, error) {
	sd, err := LoadStateDictBytes(modelPth)
	if err != nil {
		return nil, err
	}
	// self-check: known-sane conv weights must look like small init values
	if w, ok := sd["features.0.weight"]; ok {
		fl, _ := w.Floats()
		bad := 0
		for _, v := range fl {
			if v != v || v > 10 || v < -10 {
				bad++
			}
		}
		if bad > 0 {
			return nil, fmt.Errorf("weight sanity check failed (%d/%d bad)", bad, len(fl))
		}
	}
	get := func(name string) *Tensor {
		return sd[name]
	}
	mkConv := func(ci int) (convBlock, error) {
		var cb convBlock
		var err error
		if cb.w, err = get(fmt.Sprintf("features.%d.weight", ci)).Floats(); err != nil {
			return cb, err
		}
		if cb.bnW, err = get(fmt.Sprintf("features.%d.weight", ci+1)).Floats(); err != nil {
			return cb, err
		}
		if cb.bnB, err = get(fmt.Sprintf("features.%d.bias", ci+1)).Floats(); err != nil {
			return cb, err
		}
		if cb.mu, err = get(fmt.Sprintf("features.%d.running_mean", ci+1)).Floats(); err != nil {
			return cb, err
		}
		if cb.va, err = get(fmt.Sprintf("features.%d.running_var", ci+1)).Floats(); err != nil {
			return cb, err
		}
		return cb, nil
	}
	mkLinear := func(idx string) (linear, error) {
		var l linear
		var err error
		if l.w, err = get("classifier." + idx + ".weight").Floats(); err != nil {
			return l, err
		}
		if l.b, err = get("classifier." + idx + ".bias").Floats(); err != nil {
			return l, err
		}
		return l, nil
	}

	r := &Recognizer{}
	if r.c1, err = mkConv(0); err != nil {
		return nil, err
	}
	if r.c2, err = mkConv(4); err != nil {
		return nil, err
	}
	if r.c3, err = mkConv(8); err != nil {
		return nil, err
	}
	if r.c4, err = mkConv(12); err != nil {
		return nil, err
	}
	if r.l1, err = mkLinear("2"); err != nil {
		return nil, err
	}
	if r.l2, err = mkLinear("5"); err != nil {
		return nil, err
	}
	return r, nil
}

// Recognize decodes an image, preprocesses exactly like the training
// pipeline (grayscale → bicubic resize 120x40 → /255 → (x-0.5)/0.5)
// and returns the 4-character code.
func (r *Recognizer) Recognize(imgBytes []byte) (string, error) {
	x := Preprocess(imgBytes)
	if x == nil {
		return "", fmt.Errorf("image decode failed")
	}
	return r.RecognizePlaneFrom(x)
}

// RecognizePlane runs the network on a raw uint8 40x120 grayscale plane.
func (r *Recognizer) RecognizePlane(plane []uint8) (string, error) {
	if len(plane) != imgH*imgW {
		return "", fmt.Errorf("plane size %d != %d", len(plane), imgH*imgW)
	}
	x := make([][]float32, imgH)
	for y := 0; y < imgH; y++ {
		row := make([]float32, imgW)
		for xx := 0; xx < imgW; xx++ {
			v := float32(plane[y*imgW+xx]) / 255.0
			row[xx] = (v - 0.5) / 0.5
		}
		x[y] = row
	}
	return r.RecognizePlaneFrom(x)
}

// RecognizePlaneFrom runs the network on an already-normalized plane.
func (r *Recognizer) RecognizePlaneFrom(x [][]float32) (string, error) {
	logits := r.Forward(x)
	r.LastLogits = r.LastLogits[:0]
	out := make([]byte, captchaLen)
	for i := 0; i < captchaLen; i++ {
		row := logits[i*numClasses : (i+1)*numClasses]
		cp := make([]float32, numClasses)
		copy(cp, row)
		r.LastLogits = append(r.LastLogits, cp)
		best := 0
		for j := 1; j < numClasses; j++ {
			if row[j] > row[best] {
				best = j
			}
		}
		if best >= len(Charset) { // blank / out of charset
			return "", fmt.Errorf("position %d: non-char class %d", i, best)
		}
		out[i] = Charset[best]
	}
	return string(out), nil
}

// Forward runs the network on a normalized [40][120] input.
func (r *Recognizer) Forward(x [][]float32) []float32 {
	h := len(x)
	w := len(x[0])
	in := make([]float32, h*w)
	for i := range x {
		copy(in[i*w:(i+1)*w], x[i])
	}

	ch1 := r.convBnReluPool(in, 1, h, w, r.c1, true)       // 32 × h/2 × w/2
	ch2 := r.convBnReluPool(ch1, 32, h/2, w/2, r.c2, true) // 64
	ch3 := r.convBnReluPool(ch2, 64, h/4, w/4, r.c3, true) // 128
	fin := r.convBnReluPool(ch3, 128, h/8, w/8, r.c4, false)

	// fin: 128 channels × 5(h) × 15(w) when input is 40×120.
	ch := 128
	fh := h / 8
	fw := w / 8
	// adaptive avg pool to (1, 4): full height window, 4 overlapping width windows.
	pooled := adaptiveAvgPoolW(fin, ch, fh, fw, 4)

	// flatten (c-major): idx = c*4 + k
	z := make([]float32, 256)
	dense(pooled, r.l1.w, r.l1.b, z)
	relu(z)
	y := make([]float32, numClasses*captchaLen)
	dense(z, r.l2.w, r.l2.b, y)
	return y
}

// convBnReluPool: 3×3 pad-1 conv → BN(inference) → ReLU → optional maxpool2.
func (r *Recognizer) convBnReluPool(in []float32, inC, inH, inW int, cb convBlock, pool bool) []float32 {
	outC := len(cb.bnW)
	kH, kW := 3, 3
	outH, outW := inH, inW // padding=1 keeps size
	conv := make([]float32, outC*outH*outW)

	at := func(c, y, xx int) float32 {
		if y < 0 || y >= inH || xx < 0 || xx >= inW {
			return 0
		}
		return in[(c*inH+y)*inW+xx]
	}
	for oc := 0; oc < outC; oc++ {
		wbase := oc * inC * kH * kW
		for oy := 0; oy < outH; oy++ {
			for ox := 0; ox < outW; ox++ {
				var s float32
				for ic := 0; ic < inC; ic++ {
					wb := wbase + ic*9
					s += at(ic, oy-1, ox-1)*cb.w[wb] +
						at(ic, oy-1, ox)*cb.w[wb+1] +
						at(ic, oy-1, ox+1)*cb.w[wb+2] +
						at(ic, oy, ox-1)*cb.w[wb+3] +
						at(ic, oy, ox)*cb.w[wb+4] +
						at(ic, oy, ox+1)*cb.w[wb+5] +
						at(ic, oy+1, ox-1)*cb.w[wb+6] +
						at(ic, oy+1, ox)*cb.w[wb+7] +
						at(ic, oy+1, ox+1)*cb.w[wb+8]
				}
				conv[(oc*outH+oy)*outW+ox] = s
			}
		}
	}

	// batchnorm (eps=1e-5)
	const bnEps = float32(1e-5)
	for c := 0; c < outC; c++ {
		scale := cb.bnW[c] / float32(math.Sqrt(float64(cb.va[c])+float64(bnEps)))
		shift := cb.bnB[c] - cb.mu[c]*scale
		base := c * outH * outW
		for i := 0; i < outH*outW; i++ {
			v := conv[base+i]*scale + shift
			if v < 0 {
				v = 0
			}
			conv[base+i] = v
		}
	}

	if !pool {
		return conv
	}
	ph, pw := outH/2, outW/2
	poolOut := make([]float32, outC*ph*pw)
	for c := 0; c < outC; c++ {
		for py := 0; py < ph; py++ {
			for px := 0; px < pw; px++ {
				m := conv[(c*outH+py*2)*outW+px*2]
				if v := conv[(c*outH+py*2)*outW+px*2+1]; v > m {
					m = v
				}
				if v := conv[(c*outH+py*2+1)*outW+px*2]; v > m {
					m = v
				}
				if v := conv[(c*outH+py*2+1)*outW+px*2+1]; v > m {
					m = v
				}
				poolOut[(c*ph+py)*pw+px] = m
			}
		}
	}
	return poolOut
}

// adaptiveAvgPoolW averages each channel over full height and adaptive
// width windows (PyTorch AdaptiveAvgPool2d((1, n)) semantics).
func adaptiveAvgPoolW(in []float32, ch, h, w, outW int) []float32 {
	out := make([]float32, ch*outW)
	for c := 0; c < ch; c++ {
		for k := 0; k < outW; k++ {
			start := int(float64(k*w) / float64(outW))
			end := int(math.Ceil(float64((k+1)*w) / float64(outW)))
			if end <= start {
				end = start + 1
			}
			var s float32
			for y := 0; y < h; y++ {
				for xx := start; xx < end; xx++ {
					s += in[(c*h+y)*w+xx]
				}
			}
			out[c*outW+k] = s / float32(h*(end-start))
		}
	}
	return out
}

func dense(x, w, b, out []float32) {
	nIn := len(x)
	nOut := len(out)
	for o := 0; o < nOut; o++ {
		var s float32
		row := w[o*nIn : (o+1)*nIn]
		for i, xv := range x {
			s += xv * row[i]
		}
		out[o] = s + b[o]
	}
}

func relu(v []float32) {
	for i := range v {
		if v[i] < 0 {
			v[i] = 0
		}
	}
}
