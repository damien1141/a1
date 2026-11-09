package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// gradientImage builds a deterministic opaque gradient so every pixel is
// distinct and any averaging or channel mix-up shows up.
func gradientImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(3 * x),
				G: uint8(5 * y),
				B: uint8(x * y),
				A: 255,
			})
		}
	}
	return img
}

// naiveHalve re-derives the 2x2 box average with an independent pixel loop:
// accumulate the four premultiplied 16-bit samples per channel and divide by
// 256 (instead of shifting) to reach the 8-bit result.
func naiveHalve(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()/2, b.Dy()/2))
	for y := 0; y < dst.Bounds().Dy(); y++ {
		for x := 0; x < dst.Bounds().Dx(); x++ {
			var sum [4]int
			for _, d := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				r, g, bl, a := img.At(b.Min.X+2*x+d[0], b.Min.Y+2*y+d[1]).RGBA()
				sum[0] += int(r)
				sum[1] += int(g)
				sum[2] += int(bl)
				sum[3] += int(a)
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(sum[0] / 256),
				G: uint8(sum[1] / 256),
				B: uint8(sum[2] / 256),
				A: uint8(sum[3] / 256),
			})
		}
	}
	return dst
}

// assertImageEquals compares two images through the color.Color interface
// (16-bit premultiplied), so straight-alpha versus NRGBA decoding
// differences cannot mask a real pixel mismatch.
func assertImageEquals(t *testing.T, got, want image.Image) {
	t.Helper()
	require.Equal(t, want.Bounds(), got.Bounds())
	var bad []string
	for y := want.Bounds().Min.Y; y < want.Bounds().Max.Y; y++ {
		for x := want.Bounds().Min.X; x < want.Bounds().Max.X; x++ {
			gr, gg, gb, ga := got.At(x, y).RGBA()
			wr, wg, wb, wa := want.At(x, y).RGBA()
			if gr != wr || gg != wg || gb != wb || ga != wa {
				bad = append(bad, fmt.Sprintf("(%d,%d): got rgba(%d,%d,%d,%d), want rgba(%d,%d,%d,%d)",
					x, y, gr, gg, gb, ga, wr, wg, wb, wa))
				if len(bad) > 8 {
					require.Empty(t, bad, "halved pixels diverge from the naive box filter (first failures)")
				}
			}
		}
	}
	assert.Empty(t, bad, "halved pixels must match the naive box filter")
}

// oracle: Go's image decoders — PNG, JPEG, and GIF fixtures are generated
// in-test and written under misleading extensions; Load's sniffed MIME type
// must equal "image/" plus the format name Go's own decoder reports for the
// same bytes, and the raw bytes must pass through untouched.
func TestLoad_MimeTypeMatchesImageDecode_Differential(t *testing.T) {
	img := gradientImage(9, 7)
	var pngBuf, jpegBuf, gifBuf bytes.Buffer
	require.NoError(t, png.Encode(&pngBuf, img))
	require.NoError(t, jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 85}))
	require.NoError(t, gif.Encode(&gifBuf, img, nil))

	cases := []struct {
		name     string
		filename string
		data     []byte
		wantErr  string
	}{
		{"png sniffed despite txt name", "pixel.txt", pngBuf.Bytes(), ""},
		{"jpeg sniffed despite dat name", "photo.dat", jpegBuf.Bytes(), ""},
		{"gif sniffed despite bin name", "anim.bin", gifBuf.Bytes(), ""},
		{"text rejected despite png name", "notes.png", []byte("definitely not an image"), "unsupported image type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			require.NoError(t, os.WriteFile(path, tc.data, 0o644))

			res, err := Load(path)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			_, format, err := image.Decode(bytes.NewReader(tc.data))
			require.NoError(t, err)
			assert.Equal(t, "image/"+format, res.MimeType)
			assert.Equal(t, tc.data, res.Data, "raw bytes pass through untouched")
		})
	}
}

// oracle: naive pixel-loop box filter — a 640px-wide gradient PNG forced
// under a small width bound must halve exactly once (the 320px floor), and
// every output pixel must equal the 2x2 average of the independently
// decoded source.
func TestFitForModel_PNGHalvingMatchesNaiveBoxFilter_Differential(t *testing.T) {
	src := gradientImage(640, 8)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, src))

	out, mime, err := FitForModel(buf.Bytes(), "image/png", 8<<20, 100)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)

	got, err := png.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 320, 4), got.Bounds(), "640px must halve exactly once to 320px")

	decodedSrc, err := png.Decode(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertImageEquals(t, got, naiveHalve(decodedSrc))
}

// oracle: naive pixel-loop box filter — an oversized JPEG must shrink to a
// PNG whose pixels equal the 2x2 average of the independently JPEG-decoded
// source (the lossy decode happens once, identically, on both paths).
func TestFitForModel_JPEGShrinkMatchesNaiveBoxFilter_Differential(t *testing.T) {
	src := gradientImage(640, 8)
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}))
	require.Greater(t, buf.Len(), 100, "sanity: the JPEG fixture has substance")

	out, mime, err := FitForModel(buf.Bytes(), "image/jpeg", 8<<20, 100)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime, "a shrinking JPEG re-encodes as PNG to keep UI text crisp")

	got, err := png.Decode(bytes.NewReader(out))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 320, 4), got.Bounds(), "640px must halve exactly once to 320px")

	decodedSrc, err := jpeg.Decode(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	assertImageEquals(t, got, naiveHalve(decodedSrc))
}
