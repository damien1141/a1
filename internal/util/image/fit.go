package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

// Fit defaults bound tool-produced images for model consumption. maxBytes
// rides on the encoded size (base64 inflates it ~33% on the wire); maxWidth
// keeps screenshots legible without wasting tokens on pixel density the
// model cannot use.
const (
	FitMaxBytes = 1 << 20 // 1 MiB
	FitMaxWidth = 1600
	fitMinWidth = 320 // never halve below this — a blur proves nothing
)

// FitForModel bounds an image so a vision model can consume it: decode,
// halve the resolution while the encode exceeds maxBytes or the width
// exceeds maxWidth, re-encode, repeat. Returns the (possibly unchanged)
// bytes plus their MIME type. Non-decodable input is an error; a JPEG input
// is re-encoded as PNG when it must shrink (PNG keeps UI text crisp), and
// returned unchanged when it already fits.
func FitForModel(data []byte, mime string, maxBytes, maxWidth int) ([]byte, string, error) {
	if maxBytes <= 0 {
		maxBytes = FitMaxBytes
	}
	if maxWidth <= 0 {
		maxWidth = FitMaxWidth
	}
	if mime == "" {
		mime = "image/png"
	}
	if len(data) <= maxBytes && mime == "image/png" {
		// Cheap pre-check: a PNG already under the cap may still exceed the
		// width bound, so decode to measure rather than returning blind.
		if cfg, err := png.DecodeConfig(bytes.NewReader(data)); err == nil && cfg.Width <= maxWidth {
			return data, mime, nil
		}
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", fmt.Errorf("fit: decode %s: %w", mime, err)
	}
	if format != "png" && format != "jpeg" {
		return nil, "", fmt.Errorf("fit: unsupported format %q", format)
	}

	b := img.Bounds()
	// Only re-encode a fitting JPEG (or the rare non-PNG) as itself; the
	// halving loop below always emits PNG because screenshots are UI text.
	if b.Dx() <= maxWidth && format == "jpeg" {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, nil); err != nil {
			return nil, "", fmt.Errorf("fit: encode jpeg: %w", err)
		}
		if buf.Len() <= maxBytes {
			return buf.Bytes(), "image/jpeg", nil
		}
	}

	for {
		b = img.Bounds()
		encoded, err := encodePNG(img)
		if err != nil {
			return nil, "", err
		}
		if len(encoded) <= maxBytes && b.Dx() <= maxWidth {
			return encoded, "image/png", nil
		}
		if b.Dx()/2 < fitMinWidth {
			// Floor reached: return the best effort rather than an unusable blur.
			return encoded, "image/png", nil
		}
		img = halve(img)
	}
}

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("fit: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

// halve downscales img by exactly 2x using a box filter (each output pixel
// averages its 2x2 input neighborhood). Halving is the only reduction this
// package needs, and box-filtered halves keep text edges far cleaner than
// nearest-neighbor drops.
func halve(img image.Image) *image.RGBA {
	src := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, src.Dx()/2, src.Dy()/2))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			var r, g, b, a uint32
			for dy := 0; dy < 2; dy++ {
				for dx := 0; dx < 2; dx++ {
					sr, sg, sb, sa := img.At(src.Min.X+2*x+dx, src.Min.Y+2*y+dy).RGBA()
					r += sr
					g += sg
					b += sb
					a += sa
				}
			}
			// RGBA() returns premultiplied values in [0, 0xffff]; averaging
			// four of them stays in range. Straight alpha in an RGBA pixel
			// is what png.Encode expects for opaque screenshots either way.
			dst.SetRGBA(x, y, color.RGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)})
		}
	}
	return dst
}
