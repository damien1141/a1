package evidence

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shotPNG writes a w×h PNG of one color and returns its path.
func shotPNG(t *testing.T, dir, name string, w, h int, c color.RGBA) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	path := filepath.Join(dir, name+".png")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

func TestScreenshotWatchBaselineThenPass(t *testing.T) {
	dir := t.TempDir()
	l := newLedgerEvery(filepath.Join(dir, "evidence.jsonl"), 0)
	require.True(t, l.Watch("screenshot:login"))

	// First observation: baseline captured, pass by definition.
	p1 := shotPNG(t, dir, "cap1", 16, 16, color.RGBA{R: 200, A: 255})
	v := l.ObserveScreenshot("login", p1, now)
	require.Len(t, v.Improved, 1)
	assert.Equal(t, "screenshot:login", v.Improved[0].Check)
	assert.Equal(t, "baseline captured", v.Improved[0].Detail)
	assert.FileExists(t, filepath.Join(dir, "screenshots", "login.png"), "baseline stored beside the receipts")

	// Identical capture: pass with the diff numbers.
	p2 := shotPNG(t, dir, "cap2", 16, 16, color.RGBA{R: 200, A: 255})
	v = l.ObserveScreenshot("login", p2, now)
	assert.Equal(t, 1, v.Neutral, "a repeat pass is neutral, not a new improvement")
	st := l.CheckStatus("screenshot:login")
	assert.Equal(t, CheckPassing, st.State)
	// CheckStatus only surfaces detail on failures; the diff numbers live
	// in the record's Detail, visible to the regression note when they
	// one day cross the threshold.
}

func TestScreenshotWatchRegressionOnVisualChange(t *testing.T) {
	dir := t.TempDir()
	l := newLedgerEvery(filepath.Join(dir, "evidence.jsonl"), 0)
	require.True(t, l.Watch("screenshot:dashboard"))

	p1 := shotPNG(t, dir, "cap1", 16, 16, color.RGBA{G: 100, A: 255})
	require.True(t, l.ObserveScreenshot("dashboard", p1, now).Any())

	// Completely different fill: every pixel differs → fail, and the
	// previously-passing check regresses.
	p2 := shotPNG(t, dir, "cap2", 16, 16, color.RGBA{B: 100, A: 255})
	v := l.ObserveScreenshot("dashboard", p2, now)
	require.Len(t, v.Regressed, 1)
	assert.Contains(t, v.Regressed[0].Detail, "100.00% pixels changed")
	st := l.CheckStatus("screenshot:dashboard")
	assert.Equal(t, CheckFailing, st.State)
}

func TestScreenshotWatchSizeChangeFails(t *testing.T) {
	dir := t.TempDir()
	l := newLedgerEvery(filepath.Join(dir, "evidence.jsonl"), 0)
	require.True(t, l.Watch("screenshot:hero"))

	p1 := shotPNG(t, dir, "cap1", 16, 16, color.RGBA{R: 10, A: 255})
	require.True(t, l.ObserveScreenshot("hero", p1, now).Any())

	p2 := shotPNG(t, dir, "cap2", 24, 16, color.RGBA{R: 10, A: 255})
	v := l.ObserveScreenshot("hero", p2, now)
	require.Len(t, v.Regressed, 1)
	assert.Contains(t, v.Regressed[0].Detail, "size changed 16x16")
}

func TestScreenshotWatchUnwatchedNameQuiet(t *testing.T) {
	dir := t.TempDir()
	l := newLedgerEvery(filepath.Join(dir, "evidence.jsonl"), 0)
	p := shotPNG(t, dir, "cap", 8, 8, color.RGBA{A: 255})
	assert.False(t, l.ObserveScreenshot("unwatched", p, now).Any(), "undeclared names record nothing")
}

func TestScreenshotWatchUnreadableCaptureFails(t *testing.T) {
	dir := t.TempDir()
	l := newLedgerEvery(filepath.Join(dir, "evidence.jsonl"), 0)
	require.True(t, l.Watch("screenshot:gone"))
	v := l.ObserveScreenshot("gone", filepath.Join(dir, "missing.png"), now)
	// A first-observation fail is neutral in ratchet terms (nothing
	// regressed), but the check itself reads failing.
	assert.Equal(t, 1, v.Neutral)
	assert.False(t, v.Regression())
	st := l.CheckStatus("screenshot:gone")
	assert.Equal(t, CheckFailing, st.State)
	assert.Contains(t, st.Detail, "capture unreadable")
}

func TestScreenshotCheckIDGrammar(t *testing.T) {
	assert.True(t, ValidCheckID("screenshot:login"))
	assert.True(t, ValidCheckID("screenshot:home-v2.1"))
	assert.False(t, ValidCheckID("screenshot:"))
	assert.False(t, ValidCheckID("screenshot:has spaces"))
	assert.False(t, ValidCheckID("screenshot:../etc"))
	assert.False(t, ValidCheckID("screenshot:"+string(make([]byte, 65))))
}
