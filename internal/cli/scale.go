package cli

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// downscale shrinks a PNG to maxWidth pixels wide, keeping the aspect ratio. Each output
// pixel is the average of the source block it covers, which keeps text legible. An image
// already narrow enough is returned as is.
func downscale(data []byte, maxWidth int) ([]byte, error) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode screenshot: %w", err)
	}
	b := decoded.Bounds()
	if b.Dx() <= maxWidth {
		return data, nil
	}
	// One conversion to premultiplied 8-bit RGBA (draw has fast paths for the usual
	// decoded types), then plain byte arithmetic instead of a method call per channel.
	src := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(src, src.Bounds(), decoded, b.Min, draw.Src)
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	w, h := maxWidth, max(sh*maxWidth/sw, 1)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0 := y * sh / h
		y1 := max((y+1)*sh/h, y0+1)
		for x := 0; x < w; x++ {
			x0 := x * sw / w
			x1 := max((x+1)*sw/w, x0+1)
			var sum [4]uint32
			for sy := y0; sy < y1; sy++ {
				row := src.Pix[sy*src.Stride+x0*4 : sy*src.Stride+x1*4]
				for i := 0; i < len(row); i += 4 {
					sum[0] += uint32(row[i])
					sum[1] += uint32(row[i+1])
					sum[2] += uint32(row[i+2])
					sum[3] += uint32(row[i+3])
				}
			}
			n := uint32((y1 - y0) * (x1 - x0))
			o := dst.Pix[y*dst.Stride+x*4:]
			o[0], o[1], o[2], o[3] = uint8(sum[0]/n), uint8(sum[1]/n), uint8(sum[2]/n), uint8(sum[3]/n)
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
