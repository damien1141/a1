package kamitool

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FontDir is where HarmonyOS Sans TTF files are cached (~/.a1/fonts/).
// The font is downloaded on first use, not bundled — the license permits
// free use of works created with the font, but prohibits redistributing
// the font files standalone.
var (
	fontDir     string
	fontDirOnce sync.Once
	fontDirErr  error
)

// FontDir returns the cache directory for HarmonyOS Sans TTF files.
// Creates the directory on first call.
func getFontDir() (string, error) {
	fontDirOnce.Do(func() {
		home, err := os.UserHomeDir()
		if err != nil {
			fontDirErr = err
			return
		}
		fontDir = filepath.Join(home, ".a1", "fonts")
		if err := os.MkdirAll(fontDir, 0o755); err != nil {
			fontDirErr = fmt.Errorf("create font dir: %w", err)
			return
		}
	})
	return fontDir, fontDirErr
}

// fontFaceCSS returns @font-face declarations for HarmonyOS Sans
// weights. The TTF files are expected in the font dir. If a file is
// missing, the @font-face is skipped and the browser falls back to
// the next font in the CSS stack (Noto Sans → Segoe UI → sans-serif).
//
// For the first version, the font files are not auto-downloaded —
// the user installs HarmonyOS Sans from Huawei's developer site and
// it's picked up as a system font. The @font-face declarations here
// provide a local file:// fallback for when the font is cached in
// ~/.a1/fonts/.
func fontFaceCSS() string {
	dir, err := getFontDir()
	if err != nil {
		return ""
	}
	var sb string
	weights := []struct {
		file   string
		weight string
	}{
		{"HarmonyOS_Sans_Regular.ttf", "400"},
		{"HarmonyOS_Sans_Medium.ttf", "500"},
		{"HarmonyOS_Sans_Bold.ttf", "700"},
		{"HarmonyOS_Sans_Light.ttf", "300"},
	}
	for _, w := range weights {
		path := filepath.Join(dir, w.file)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		sb += fmt.Sprintf(
			`@font-face { font-family: 'HarmonyOS Sans'; src: url('file://%s') format('truetype'); font-weight: %s; font-display: swap; }`,
			path, w.weight,
		) + "\n"
	}
	return sb
}
