package kamitool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damien1141/a1/internal/bw"
)

// renderPDF generates a PDF from an HTML string using the BetterWright
// browser daemon. The HTML is written to a temp file, loaded via file://,
// printed with page.pdf into the session's artifact directory, and copied
// to outputPath.
//
// Kami CSS (warm parchment background, ink-blue accents, HarmonyOS Sans
// font) is already embedded in the HTML by the caller — this function just
// renders it. PrintBackground must be true so the parchment background
// shows in the PDF (the default is white-only). page.pdf requires headless
// Chromium, which is the daemon's default launch mode.
//
// paperSize is "A4" or "Letter".
func renderPDF(ctx context.Context, html string, outputPath string, paperSize string) error {
	// Write HTML next to the output so the daemon can load it via file://.
	// Inline content (page.setContent) works but is less reliable for
	// @font-face with file:// URLs — the browser may not resolve local
	// font paths from an inline context.
	tmpDir := filepath.Dir(outputPath)
	htmlFile, err := os.CreateTemp(tmpDir, "kami-*.html")
	if err != nil {
		return fmt.Errorf("create temp html: %w", err)
	}
	htmlPath := htmlFile.Name()
	defer os.Remove(htmlPath)
	if _, err := htmlFile.WriteString(html); err != nil {
		_ = htmlFile.Close()
		return fmt.Errorf("write temp html: %w", err)
	}
	_ = htmlFile.Close()

	js := fmt.Sprintf(`await page.goto(%s);
await page.evaluate(() => document.fonts.ready);
const out = artifactPath('a1.pdf');
await page.pdf({path: out, format: %s, printBackground: true, margin: {top: '0', bottom: '0', left: '0', right: '0'}});
return out;`, bw.JSString("file://"+htmlPath), bw.JSString(paperSize))

	env, err := bw.Run(ctx, "", false, js)
	if err != nil {
		return fmt.Errorf("render pdf: %w", err)
	}
	var artifact string
	if jerr := json.Unmarshal(env.Result, &artifact); jerr != nil || artifact == "" {
		return fmt.Errorf("render pdf: unexpected daemon result %.200s", string(env.Result))
	}
	pdfBytes, err := os.ReadFile(artifact)
	if err != nil {
		return fmt.Errorf("read pdf artifact: %w", err)
	}
	if err := os.WriteFile(outputPath, pdfBytes, 0o644); err != nil {
		return fmt.Errorf("write pdf: %w", err)
	}
	return nil
}
