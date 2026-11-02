package browsertool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrowserNavigateTool_Definition verifies the typed schema: name,
// readable flag, required fields, and that the description tells the
// model how to use it without referencing a free-string action field.
func TestBrowserNavigateTool_Definition(t *testing.T) {
	tool := BrowserNavigateTool()
	assert.Equal(t, "browser_navigate", tool.Definition.Name)
	// Not Readable: navigation mutates the shared page.
	assert.False(t, tool.Definition.Readable, "navigate mutates the shared page — not readable")
	assert.NotNil(t, tool.Run)

	// Required: only url. Profile is optional.
	require.Contains(t, tool.Definition.Params.Required, "url",
		"url must be required — it's the only thing the model must provide")

	// Description must not mention a free-string action discriminator —
	// the whole point of the atomic tool is to remove that.
	desc := tool.Definition.Description
	assert.NotContains(t, desc, `action:`,
		"description must not reference a free-string action field")
	assert.Contains(t, desc, "browser_read",
		"description should name the next tool the model needs (read)")
}

// TestBrowserInteractTool_Definition verifies the typed schema: selector
// required, text optional (selects click vs type), no action field.
func TestBrowserInteractTool_Definition(t *testing.T) {
	tool := BrowserInteractTool()
	assert.Equal(t, "browser_interact", tool.Definition.Name)
	// Not Readable: click/type mutate page state; concurrent calls would
	// interleave against the same page.
	assert.False(t, tool.Definition.Readable, "interact mutates page state — not readable")

	require.Contains(t, tool.Definition.Params.Required, "selector",
		"selector must be required")

	desc := tool.Definition.Description
	assert.Contains(t, desc, "click", "description explains the click verb")
	assert.Contains(t, desc, "type", "description explains the type verb")
	assert.NotContains(t, desc, `action:`,
		"description must not reference a free-string action field")
}

// TestBrowserReadTool_Definition verifies the typed schema: mode selects
// screenshot vs content, no required fields (mode defaults to screenshot).
func TestBrowserReadTool_Definition(t *testing.T) {
	tool := BrowserReadTool()
	assert.Equal(t, "browser_read", tool.Definition.Name)
	// Not Readable: screenshot/content read the shared page that
	// navigate/interact mutate; concurrent calls would race.
	assert.False(t, tool.Definition.Readable, "read operates on the shared page — not readable")
	assert.Empty(t, tool.Definition.Params.Required,
		"no required fields — mode defaults to screenshot")

	desc := tool.Definition.Description
	assert.Contains(t, desc, "screenshot", "description explains screenshot mode")
	assert.Contains(t, desc, "content", "description explains content mode")
	assert.NotContains(t, desc, `action:`,
		"description must not reference a free-string action field")
}

// TestBrowserNavigateTool_DetailFromArgs verifies the detail line uses
// the URL, not a generic "browser_navigate" — the row stays glanceable.
func TestBrowserNavigateTool_DetailFromArgs(t *testing.T) {
	tool := BrowserNavigateTool()

	raw, _ := json.Marshal(navigateInput{URL: "https://example.com"})
	d := tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_navigate https://example.com", d)

	// Empty URL → bare identity.
	raw, _ = json.Marshal(navigateInput{})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_navigate", d)
}

// TestBrowserInteractTool_DetailFromArgs verifies the detail line shows
// click vs type based on whether text was provided.
func TestBrowserInteractTool_DetailFromArgs(t *testing.T) {
	tool := BrowserInteractTool()

	// No text → click.
	raw, _ := json.Marshal(interactInput{Selector: "#submit"})
	d := tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_interact click #submit", d)

	// With text → type.
	raw, _ = json.Marshal(interactInput{Selector: "input[name=q]", Text: "hello"})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_interact type input[name=q]", d)

	// Empty selector → bare identity.
	raw, _ = json.Marshal(interactInput{})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_interact", d)
}

// TestBrowserReadTool_DetailFromArgs verifies the detail line shows the
// mode (defaulting to screenshot when empty).
func TestBrowserReadTool_DetailFromArgs(t *testing.T) {
	tool := BrowserReadTool()

	// Empty mode → screenshot.
	raw, _ := json.Marshal(readInput{})
	d := tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_read screenshot", d)

	// Explicit content.
	raw, _ = json.Marshal(readInput{Mode: "content"})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_read content", d)

	// Explicit screenshot.
	raw, _ = json.Marshal(readInput{Mode: "screenshot"})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_read screenshot", d)

	// Case-insensitive.
	raw, _ = json.Marshal(readInput{Mode: "CONTENT"})
	d = tool.DetailFromArgs(raw)
	assert.Equal(t, "browser_read content", d)
}

// TestRunBrowserRead_InvalidMode verifies an unknown mode returns an
// error rather than silently defaulting — the model should know it sent
// a bad value.
func TestRunBrowserRead_InvalidMode(t *testing.T) {
	raw, _ := json.Marshal(readInput{Mode: "html"})
	_, err := runBrowserRead(nil, raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown mode")
	// The error must name the bad value so the model can correct it.
	assert.Contains(t, err.Error(), "html")
}

// TestAtomicTools_RouteThroughLegacy verifies the atomic tools share the
// same locked dispatch path as the legacy `browser` tool — they don't
// bypass the mutex or the browser-is-open guard. We can't actually launch
// Firefox in a unit test, but we can verify the "browser not open"
// guard fires (which proves the tool reached runBrowser, the same path
// the legacy tool uses).
func TestAtomicTools_RouteThroughLegacy(t *testing.T) {
	// Navigate → action="open". The browser-is-open guard in runBrowser
	// returns an error because no page is open in the test environment.
	// (open requires a browser; runBrowser checks pages map first.)
	// Actually, browserOpen calls ensureBrowser, which would launch
	// Firefox — we can't do that here. So instead verify the click/type
	// path (which requires the browser to already be open) returns the
	// "browser is not open" guard, proving it went through runBrowser.
	raw, _ := json.Marshal(interactInput{Selector: "#x"})
	_, err := runBrowserInteract(nil, raw)
	require.Error(t, err)
	// The guard message is the same one runBrowser returns; this proves
	// the atomic tool routed through the legacy locked path rather than
	// implementing its own dispatch.
	assert.Contains(t, strings.ToLower(err.Error()), "browser is not open",
		"interact must route through runBrowser's guard")
}

// TestRunBrowserRead_NoBrowser verifies browser_read also routes through
// the legacy guard (it requires a page to screenshot/read).
func TestRunBrowserRead_NoBrowser(t *testing.T) {
	raw, _ := json.Marshal(readInput{Mode: "screenshot"})
	_, err := runBrowserRead(nil, raw)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "browser is not open",
		"read must route through runBrowser's guard")

	raw, _ = json.Marshal(readInput{Mode: "content"})
	_, err = runBrowserRead(nil, raw)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "browser is not open")
}
