package image

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solidPNG encodes a w×h PNG of one color so tests can build inputs of any
// size without fixtures.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// noisyPNG encodes a w×h PNG of pseudo-random pixels: random data does not
// compress, so the encoded size is predictable (≈ 4·w·h) and byte-cap tests
// get deterministic inputs.
func noisyPNG(t *testing.T, w, h int, seed int64) []byte {
	t.Helper()
	r := rand.New(rand.NewSource(seed))
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var px [4]byte
			r.Read(px[:])
			img.SetRGBA(x, y, color.RGBA{R: px[0], G: px[1], B: px[2], A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func TestFitForModelSmallImageUnchanged(t *testing.T) {
	src := solidPNG(t, 64, 64, color.RGBA{R: 255, A: 255})
	out, mime, err := FitForModel(src, "image/png", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	// A fitting PNG with in-bound width is returned byte-identical.
	assert.Equal(t, src, out)
}

func TestFitForModelHalvesOversizedWidth(t *testing.T) {
	src := solidPNG(t, 3200, 100, color.RGBA{G: 200, A: 255})
	out, mime, err := FitForModel(src, "image/png", 0, 0)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, 1600, cfg.Width, "3200px should halve once to 1600")
	assert.Equal(t, 50, cfg.Height, "height halves with the width")
}

func TestFitForModelRespectsByteCap(t *testing.T) {
	// Random pixels do not compress: a 640px noisy PNG encodes ≈ 1.6 MiB, so
	// an 800 KiB cap forces one halving to 320px (≈ 410 KiB, fits). The 320px
	// floor stops further shrinking — a blur proves nothing.
	src := noisyPNG(t, 640, 640, 1)
	assert.Greater(t, len(src), 1<<20, "sanity: noisy source encodes large")
	out, mime, err := FitForModel(src, "image/png", 800<<10, 10000)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, 320, cfg.Width, "an 800 KiB cap halves 640px noisy once to 320px")
	assert.LessOrEqual(t, len(out), 800<<10, "the capped encode fits the cap")
}

func TestFitForModelGarbageErrors(t *testing.T) {
	_, _, err := FitForModel([]byte("not an image"), "image/png", 0, 0)
	assert.Error(t, err)
}

func TestFitForModelTinyImageNeverShrinksBelowFloor(t *testing.T) {
	src := solidPNG(t, 64, 64, color.RGBA{B: 255, A: 255})
	out, mime, err := FitForModel(src, "image/png", 1, 1)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	cfg, err := png.DecodeConfig(bytes.NewReader(out))
	require.NoError(t, err)
	assert.Equal(t, 64, cfg.Width, "the floor keeps a 64px image whole even under an impossible cap")
}
