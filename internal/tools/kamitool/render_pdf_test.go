package kamitool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/damien1141/a1/internal/bw"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderPDFWritesArtifactCopy fakes the daemon: the "PDF" lands in the
// artifact path the JS reports, and renderPDF must copy it to the output
// path.
func TestRenderPDFWritesArtifactCopy(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.Join(dir, "artifacts", "a1.pdf")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifact), 0o755))
	require.NoError(t, os.WriteFile(artifact, []byte("%PDF-1.4 fake"), 0o644))

	origRun, origResolve := bw.RunCommand, bw.Resolve
	var sawJS string
	bw.RunCommand = func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				sawJS = args[i+1]
			}
		}
		out, _ := json.Marshal(artifact)
		return []byte(fmt.Sprintf(`{"ok":true,"result":%s}`, out)), nil
	}
	bw.Resolve = func() (string, error) { return "/usr/bin/fake-betterwright", nil }
	t.Cleanup(func() { bw.RunCommand, bw.Resolve = origRun, origResolve })

	out := filepath.Join(dir, "doc.pdf")
	require.NoError(t, renderPDF(t.Context(), "<html><body>hi</body></html>", out, "Letter"))

	b, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "%PDF-1.4 fake", string(b))
	assert.Contains(t, sawJS, "page.pdf", "the JS prints the page")
	assert.Contains(t, sawJS, "'Letter'", "paper size rides the print options")
	assert.Contains(t, sawJS, "printBackground: true", "Kami backgrounds must survive the print")
	assert.Contains(t, sawJS, "file://", "the HTML loads via file://")
}
