package evidence

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// screenshot:<name> observation: named browser captures graded against a
// stored baseline. The first observation of a name stores its capture as the
// baseline and passes; later captures diff against it — the frontend-world
// VGR (visual GUI regression) check. Baselines are artifacts, not receipts:
// they live beside the ledger's receipt file in a screenshots/ directory, so
// deleting the session directory deletes the whole story.

// screenshotPassFraction is the maximum fraction of pixels that may differ
// (per-channel delta above screenshotChannelDelta) for a capture to still
// match its baseline. 1% absorbs antialiasing, caret blinks, and ad rotators
// while failing on any real layout change.
const (
	screenshotPassFraction = 0.01
	screenshotChannelDelta = 16 // of 255
)

// validScreenshotName gates the check-id suffix: letters, digits, dot,
// dash, underscore, 1..64 chars. It names a baseline file, so the charset
// stays boring on purpose.
func validScreenshotName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return name != "." && name != ".."
}

// ObserveScreenshot grades a named capture against its declared watch. The
// caller (the executor's browser evidence hook) supplies the on-disk PNG from
// the tool result. Unknown names record nothing — the ledger stays quiet
// unless the model declared `screenshot:<name>` in a contract, exactly like
// exit:/http:/port: watches.
func (l *Ledger) ObserveScreenshot(name, path string, at time.Time) Verdict {
	if l == nil || !validScreenshotName(name) || path == "" {
		return Verdict{}
	}
	l.mu.Lock()
	w, watched := l.watches["screenshot:"+name]
	l.mu.Unlock()
	if !watched || w.kind != watchScreenshot {
		return Verdict{}
	}

	current, err := os.ReadFile(path)
	if err != nil {
		return l.commitRecords("screenshot "+name, []Record{{
			Check:  watchID(w),
			Status: Fail,
			Detail: "capture unreadable: " + err.Error(),
		}}, at)
	}

	basePath := l.screenshotBaselinePath(name)
	baseline, err := os.ReadFile(basePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return l.commitRecords("screenshot "+name, []Record{{
				Check:  watchID(w),
				Status: Fail,
				Detail: "baseline unreadable: " + err.Error(),
			}}, at)
		}
		// First observation: store the capture as the baseline and pass by
		// definition — but only if it is a decodable image; garbage must not
		// seed a baseline that nothing can ever match. A baseline that cannot
		// be written still grades (memory-only), it just cannot survive a
		// restart.
		if _, err := png.Decode(bytes.NewReader(current)); err != nil {
			return l.commitRecords("screenshot "+name, []Record{{
				Check:  watchID(w),
				Status: Fail,
				Detail: "capture is not a decodable PNG: " + err.Error(),
			}}, at)
		}
		_ = os.MkdirAll(filepath.Dir(basePath), 0o755)
		if writeErr := os.WriteFile(basePath, current, 0o644); writeErr != nil {
			return l.commitRecords("screenshot "+name, []Record{{
				Check:  watchID(w),
				Status: Pass,
				Detail: "baseline captured (not persisted: " + writeErr.Error() + ")",
			}}, at)
		}
		return l.commitRecords("screenshot "+name, []Record{{
			Check:  watchID(w),
			Status: Pass,
			Detail: "baseline captured",
		}}, at)
	}

	rec := diffScreenshotRecord(watchID(w), baseline, current)
	return l.commitRecords("screenshot "+name, []Record{rec}, at)
}

// screenshotBaselinePath resolves where a name's baseline PNG lives: next to
// the ledger's receipt file under screenshots/, or under the OS temp dir for
// a memory-only ledger (path "") so tests and ephemeral sessions still work.
func (l *Ledger) screenshotBaselinePath(name string) string {
	dir := filepath.Join(os.TempDir(), "a1-screenshot-baselines")
	if l.path != "" {
		dir = filepath.Join(filepath.Dir(l.path), "screenshots")
	}
	return filepath.Join(dir, name+".png")
}

// diffScreenshotRecord grades a capture against its baseline: same
// dimensions and ≤ screenshotPassFraction of pixels with a per-channel delta
// above screenshotChannelDelta.
func diffScreenshotRecord(check string, baseline, current []byte) Record {
	a, errA := png.Decode(bytes.NewReader(baseline))
	b, errB := png.Decode(bytes.NewReader(current))
	if errA != nil || errB != nil {
		return Record{Check: check, Status: Fail, Detail: "decode baseline/current: not valid PNG"}
	}
	ab, bb := a.Bounds(), b.Bounds()
	if ab.Dx() != bb.Dx() || ab.Dy() != bb.Dy() {
		return Record{
			Check:  check,
			Status: Fail,
			Detail: fmt.Sprintf("size changed %dx%d → %dx%d", ab.Dx(), ab.Dy(), bb.Dx(), bb.Dy()),
		}
	}
	total := ab.Dx() * ab.Dy()
	changed := 0
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			if pixelDiffers(a.At(ab.Min.X+x, ab.Min.Y+y), b.At(bb.Min.X+x, bb.Min.Y+y)) {
				changed++
			}
		}
	}
	fraction := float64(changed) / float64(total)
	detail := fmt.Sprintf("%.2f%% pixels changed (%d/%d)", fraction*100, changed, total)
	if fraction <= screenshotPassFraction {
		return Record{Check: check, Status: Pass, Detail: detail}
	}
	return Record{Check: check, Status: Fail, Detail: detail}
}

// pixelDiffers reports whether two pixels differ by more than
// screenshotChannelDelta on any channel. Alpha rides along so a fully
// transparent flip still counts.
func pixelDiffers(a, b color.Color) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return deltaAbove(ar, br) || deltaAbove(ag, bg) || deltaAbove(ab, bb) || deltaAbove(aa, ba)
}

func deltaAbove(x, y uint32) bool {
	d := int64(x>>8) - int64(y>>8)
	if d < 0 {
		d = -d
	}
	return d > screenshotChannelDelta
}
