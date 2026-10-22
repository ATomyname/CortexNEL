// Preprocessing that mirrors the training pipeline exactly:
// PIL: convert('L') → resize((120, 40)) [BICUBIC default] →
// float32 / 255 → (x - 0.5) / 0.5.
package captchaocr

import (
	"bytes"
	"image"
	"math"

	_ "image/gif"  // register decoder
	_ "image/jpeg" // register decoder
	_ "image/png"  // register decoder
)

// Preprocess decodes an image (PNG/JPEG/GIF) and returns the
// normalized [40][120] plane. Returns nil when decoding fails.
func Preprocess(imgBytes []byte) [][]float32 {
	plane := DumpPreprocess(imgBytes)
	if plane == nil {
		return nil
	}
	out := make([][]float32, imgH)
	for y := 0; y < imgH; y++ {
		row := make([]float32, imgW)
		for x := 0; x < imgW; x++ {
			v := float32(plane[y*imgW+x]) / 255.0
			row[x] = (v - 0.5) / 0.5
		}
		out[y] = row
	}
	return out
}

// DumpPreprocess exposes the uint8 grayscale plane right after the PIL
// equivalent of convert('L') + resize((120,40)) — for byte-diffing
// against a reference implementation.
func DumpPreprocess(imgBytes []byte) []uint8 {
	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		return nil
	}
	b := img.Bounds()
	inW, inH := b.Dx(), b.Dy()
	gray := toLuma(img, b)
	return resizeBicubic(gray, inH, inW, imgH, imgW)
}

// toLuma converts any decoded image to PIL 'L' semantics
// (L = R*299/1000 + G*587/1000 + B*114/1000, alpha ignored).
func toLuma(img image.Image, b image.Rectangle) []float32 {
	w, h := b.Dx(), b.Dy()
	out := make([]float32, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, bl16, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			out[y*w+x] = float32(float64(r16>>8)*0.299 + float64(g16>>8)*0.587 + float64(bl16>>8)*0.114)
		}
	}
	return out
}

// cubicKernel is PIL's BICUBIC filter (Catmull-Rom variant, a=-0.5).
func cubicKernel(x float64) float64 {
	x = math.Abs(x)
	if x <= 1 {
		return ((1.5*x-2.5)*x)*x + 1
	}
	if x < 2 {
		return ((-0.5*x+2.5)*x-4)*x + 2
	}
	return 0
}

// resizeBicubic scales a single-channel plane to outH x outW,
// reproducing PIL's BICUBIC resample with clamped edge sampling.
func resizeBicubic(src []float32, inH, inW, outH, outW int) []uint8 {
	dst := make([]float64, outH*outW)
	scaleX := float64(inW) / float64(outW)
	scaleY := float64(inH) / float64(outH)

	sample := func(y, x int) float64 {
		if y < 0 {
			y = 0
		}
		if y >= inH {
			y = inH - 1
		}
		if x < 0 {
			x = 0
		}
		if x >= inW {
			x = inW - 1
		}
		return float64(src[y*inW+x])
	}

	for oy := 0; oy < outH; oy++ {
		fy := (float64(oy)+0.5)*scaleY - 0.5
		iy := math.Floor(fy)
		dy := fy - iy
		y0 := int(iy)
		wy := [4]float64{
			cubicKernel(dy + 1), cubicKernel(dy), cubicKernel(1 - dy), cubicKernel(2 - dy),
		}
		for ox := 0; ox < outW; ox++ {
			fx := (float64(ox)+0.5)*scaleX - 0.5
			ix := math.Floor(fx)
			dx := fx - ix
			x0 := int(ix)
			wx := [4]float64{
				cubicKernel(dx + 1), cubicKernel(dx), cubicKernel(1 - dx), cubicKernel(2 - dx),
			}
			var acc float64
			for j := 0; j < 4; j++ {
				var rowAcc float64
				for i := 0; i < 4; i++ {
					rowAcc += sample(y0+j-1, x0+i-1) * wx[i]
				}
				acc += rowAcc * wy[j]
			}
			dst[oy*outW+ox] = acc
		}
	}

	out := make([]uint8, outH*outW)
	for i, v := range dst {
		iv := int(math.Round(v))
		if iv < 0 {
			iv = 0
		}
		if iv > 255 {
			iv = 255
		}
		out[i] = uint8(iv)
	}
	return out
}
