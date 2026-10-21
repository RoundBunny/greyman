package cli

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"
)

func bigPNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func TestDownscaleTallPage(t *testing.T) {
	data := bigPNG(1920, 16000)
	start := time.Now()
	out, err := downscale(data, 800)
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := png.DecodeConfig(bytes.NewReader(out))
	t.Logf("1920x16000 -> %dx%d in %v", cfg.Width, cfg.Height, took)
	if cfg.Width != 800 || cfg.Height != 6666 {
		t.Fatalf("size = %dx%d", cfg.Width, cfg.Height)
	}
}
