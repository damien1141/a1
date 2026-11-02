package browsertool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damien1141/a1/internal/bw"
	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bwScript is one scripted daemon response.
type bwScript struct {
	// js, when non-empty, must appear in the run's -c argument.
	js string
	// out is the envelope the fake daemon prints.
	out string
	// err, when non-empty, fails the command.
	err string
	// used marks the script consumed so one response maps to one call.
	used bool
}

// fakeBW swaps the daemon for a scripted responder and opens the browser
// latch, restoring everything on cleanup.
func fakeBW(t *testing.T, scripts ...bwScript) *bwRecorder {
	t.Helper()
	rec := &bwRecorder{scripts: scripts}
	origRun, origResolve := bw.RunCommand, bw.Resolve
	bw.RunCommand = func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		rec.args = append(rec.args, args)
		js := ""
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				js = args[i+1]
			}
		}
		for i := range rec.scripts {
			s := &rec.scripts[i]
			if s.used || (s.js != "" && !strings.Contains(js, s.js)) {
				continue
			}
			s.used = true
			if s.err != "" {
				return nil, fmt.Errorf("%s", s.err)
			}
			return []byte(s.out), nil
		}
		return []byte(`{"ok":true,"result":true}`), nil
	}
	bw.Resolve = func() (string, error) { return "/usr/bin/fake-betterwright", nil }
	t.Cleanup(func() {
		bw.RunCommand, bw.Resolve = origRun, origResolve
		browserMu.Lock()
		bwOpened = false
		currentProfile = bw.DefaultProfile
		browserMu.Unlock()
	})

	browserMu.Lock()
	bwOpened = true
	browserMu.Unlock()
	return rec
}

type bwRecorder struct {
	scripts []bwScript
	args    [][]string
}

func (r *bwRecorder) lastJS() string {
	if len(r.args) == 0 {
		return ""
	}
	args := r.args[len(r.args)-1]
	for i, a := range args {
		if a == "-c" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestBWOpenNavigatesAndReportsTitle(t *testing.T) {
	rec := fakeBW(t, bwScript{
		js:  "page.goto",
		out: `{"ok":true,"result":{"url":"https://example.com/","title":"Example Domain"}}`,
	})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"open","url":"https://example.com"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Opened https://example.com")
	assert.Contains(t, res.Content, "Example Domain")
	assert.Contains(t, res.Content, "Profile: default")
	assert.Contains(t, rec.lastJS(), `'https://example.com'`)
}

func TestBWOpenWithProfileCreatesDirAndPassesFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BETTERWRIGHT_HOME", home)
	rec := fakeBW(t, bwScript{js: "page.goto", out: `{"ok":true,"result":{"url":"u","title":"t"}}`})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"open","url":"example.com","profile":"work"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Profile: work")
	assert.DirExists(t, filepath.Join(home, "browser", "profiles", "work"))
	// The profile flag rides the run.
	found := false
	for _, args := range rec.args {
		for i, a := range args {
			if a == "--profile" && i+1 < len(args) && args[i+1] == "work" {
				found = true
			}
		}
	}
	assert.True(t, found, "--profile work must reach the CLI")
}

func TestBWClickAndTypeEscapeSelectors(t *testing.T) {
	rec := fakeBW(t,
		bwScript{js: "page.click", out: `{"ok":true,"result":true}`},
		bwScript{js: "page.fill", out: `{"ok":true,"result":true}`},
	)
	_, err := runBrowser(t.Context(), json.RawMessage(`{"action":"click","selector":"#sub'mit"}`))
	require.NoError(t, err)
	assert.Contains(t, rec.lastJS(), `'#sub\'mit'`, "single quotes are escaped in JS literals")

	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"type","selector":"#q","text":"it's \\ fine"}`))
	require.NoError(t, err)
	assert.Contains(t, rec.lastJS(), `page.fill('#q', 'it\'s \\ fine')`)
}

func TestBWScreenshotCopiesArtifactAndAttachesImage(t *testing.T) {
	// The fake daemon "captures" by writing a PNG to the path it reports.
	artifact := filepath.Join(t.TempDir(), "proof.png")
	require.NoError(t, os.WriteFile(artifact, tinyPNG(t), 0o644))
	fakeBW(t, bwScript{
		js:  "screenshot",
		out: fmt.Sprintf(`{"ok":true,"result":{"kind":"proof","path":%q,"media":"MEDIA:%s"}}`, artifact, artifact),
	})

	// Image-enabled ctx: the capture rides the result as an image block.
	ctx := withImageEnabled(t.Context(), true)
	res, err := runBrowser(ctx, json.RawMessage(`{"action":"screenshot","name":"login"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Screenshot saved to")
	assert.True(t, strings.Contains(res.Content, "a1-screenshot-"), "the a1 temp path is the stable copy")
	require.Len(t, res.Images, 1)
	assert.Equal(t, "image/png", res.Images[0].MimeType)
	require.NotNil(t, res.Screenshot)
	assert.Equal(t, "login", res.Screenshot.Name)
	assert.FileExists(t, res.Screenshot.Path)

	// Blind model: no image blocks, path-only text.
	ctxBlind := withImageEnabled(t.Context(), false)
	fakeBW(t, bwScript{
		js:  "screenshot",
		out: fmt.Sprintf(`{"ok":true,"result":{"kind":"proof","path":%q,"media":"MEDIA:%s"}}`, artifact, artifact),
	})
	res2, err := runBrowser(ctxBlind, json.RawMessage(`{"action":"screenshot"}`))
	require.NoError(t, err)
	assert.Empty(t, res2.Images)
	assert.Nil(t, res2.Screenshot)
}

func TestBWContentCapsAndStrips(t *testing.T) {
	body := strings.Repeat("word ", 5000)
	fakeBW(t, bwScript{
		js:  "page.evaluate",
		out: fmt.Sprintf(`{"ok":true,"result":{"text":%q,"url":"https://example.com/page","title":"Example"}}`, body),
	})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"content"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "URL: https://example.com/page")
	assert.Contains(t, res.Content, "Title: Example")
	assert.Contains(t, res.Content, "Profile: default")
	assert.LessOrEqual(t, len(res.Content), 14000, "12K content cap plus header")
	assert.Contains(t, res.Content, "…[truncated]")
}

func TestBWContentHandlesSpilledOutput(t *testing.T) {
	spill := filepath.Join(t.TempDir(), "spilled.json")
	require.NoError(
		t,
		os.WriteFile(
			spill,
			[]byte(`{"text":"full text from the spill file","url":"https://x.example/","title":"X"}`),
			0o644,
		),
	)
	fakeBW(t, bwScript{
		js:  "page.evaluate",
		out: fmt.Sprintf(`{"ok":true,"result":{"truncated":true,"preview":"…","fullOutputPath":%q}}`, spill),
	})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"content"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "full text from the spill file")
}

func TestBWChallengeBecomesHandoff(t *testing.T) {
	fakeBW(t, bwScript{
		js: "page.goto",
		out: `{"ok":true,"result":{"url":"https://shop.example/","title":"Shop"},` +
			`"challenges":[{"page":"page-1","provider":"cloudflare","url":"https://shop.example/checkout","advice":"solve before retrying"}]}`,
	})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"navigate","url":"https://shop.example/"}`))
	require.NoError(t, err)
	require.NotNil(t, res.Handoff, "a detected challenge routes to the ask flow")
	assert.Equal(t, "https://shop.example/checkout", res.Handoff.URL)
	assert.Contains(t, res.Handoff.Reason, "cloudflare")
}

func TestBWCloseResetsStateAndStopsDaemons(t *testing.T) {
	rec := fakeBW(t)
	_, err := runBrowser(t.Context(), json.RawMessage(`{"action":"close"}`))
	require.NoError(t, err)
	sawAll := false
	for _, args := range rec.args {
		for _, a := range args {
			if a == "--all" {
				sawAll = true
			}
		}
	}
	assert.True(t, sawAll, "close stops every profile's daemon with --all")

	// After close the guard is back: the model must ask the user again.
	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"content"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "browser is not open")
}

func TestBWProfileLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("BETTERWRIGHT_HOME", home)
	fakeBW(t)

	// create
	_, err := runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"create shopping"}`))
	require.NoError(t, err)
	assert.DirExists(t, filepath.Join(home, "browser", "profiles", "shopping"))

	// list shows default + shopping
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"list"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "- default (active)")
	assert.Contains(t, res.Content, "- shopping")

	// switch
	res, err = runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"switch shopping"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Switched to profile")

	// delete
	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"delete shopping"}`))
	require.NoError(t, err)
	assert.NoDirExists(t, filepath.Join(home, "browser", "profiles", "shopping"))

	// default cannot be deleted
	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"delete default"}`))
	require.Error(t, err)
}

func TestBWSandboxedProfileNames(t *testing.T) {
	fakeBW(t)
	_, err := runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"create ../escape"}`))
	require.Error(t, err, "path traversal in profile names must fail")
	_, err = runBrowser(t.Context(), json.RawMessage(`{"action":"profile","profile":"create has space"}`))
	require.Error(t, err)
}

func TestBWSearchUsesMojeekNotBlockedEngines(t *testing.T) {
	rec := fakeBW(t, bwScript{js: "mojeek", out: `{"ok":true,"result":{"url":"https://m.example/","title":"Results"}}`})
	res, err := runBrowser(t.Context(), json.RawMessage(`{"action":"search","text":"test query"}`))
	require.NoError(t, err)
	assert.Contains(t, res.Content, "Searched Mojeek")
	assert.Contains(t, rec.lastJS(), "mojeek.com/search?q=")
	assert.NotContains(t, rec.lastJS(), "duckduckgo.com", "DDG search UI is blocked by the daemon")
	assert.NotContains(t, rec.lastJS(), "google.com/search")
}

func TestEnsureBrowserWarmsAndLatches(t *testing.T) {
	rec := fakeBW(t)
	browserMu.Lock()
	bwOpened = false
	browserMu.Unlock()

	require.NoError(t, ensureBrowser(t.Context()))
	assert.True(t, rec.lastJS() != "" || len(rec.args) > 0, "a warm-up run reached the daemon")

	// Second ensure is a no-op (latched).
	before := len(rec.args)
	require.NoError(t, ensureBrowser(t.Context()))
	assert.Equal(t, before, len(rec.args))
}

// tinyPNG builds a valid 2x2 PNG for screenshot plumbing tests.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// withImageEnabled mirrors the executor's model-gate on the tool context.
func withImageEnabled(ctx context.Context, enabled bool) context.Context {
	return tooldef.WithImageEnabled(ctx, enabled)
}
