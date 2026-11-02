package webserver

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/session"
)

// The transcript renders agent output, which is untrusted text. Nothing that
// passes through renderMarkdown may emit raw markup.
func TestRenderMarkdownEscapesHTML(t *testing.T) {
	out := renderMarkdown(`<script>alert(1)</script> & <b>bold</b> "quoted"`)
	assert.NotContains(t, out, "<script>")
	assert.NotContains(t, out, "<b>")
	assert.Contains(t, out, "&lt;script&gt;")
	assert.Contains(t, out, "&amp;")
}

func TestRenderMarkdownKeepsCodeFences(t *testing.T) {
	out := renderMarkdown("before\n\n```go\nfmt.Println(\"x\")\n```\n\nafter")
	assert.Contains(t, out, "<pre><code>")
	assert.Contains(t, out, "fmt.Println")
	// The fence body must be escaped, so a code block cannot smuggle tags.
	// html.EscapeString renders quotes as numeric entities.
	assert.Contains(t, out, "&#34;x&#34;")
	assert.NotContains(t, out, "\"x\"")
}

func TestRenderMarkdownInlineSpans(t *testing.T) {
	out := renderMarkdown("use `read` for **bold** and *italic* text")
	assert.Contains(t, out, "<code>read</code>")
	assert.Contains(t, out, "<strong>bold</strong>")
	assert.Contains(t, out, "<em>italic</em>")
}

func TestRenderMarkdownUnterminatedFenceStillRenders(t *testing.T) {
	// A stream that cuts off mid-fence must not lose the code it received.
	out := renderMarkdown("```\npartial code")
	assert.Contains(t, out, "partial code")
	assert.Contains(t, out, "<pre><code>")
}

func TestTranscriptFragmentEscapesUserText(t *testing.T) {
	snap := session.Snapshot{Messages: []session.Message{{
		Role: session.RoleUser,
		Text: `</div><script>alert("xss")</script>`,
	}}}
	out := transcriptFragment(snap)
	assert.NotContains(t, out, "<script>")
	assert.NotContains(t, out, "</div><script>")
	assert.Contains(t, out, "&lt;script&gt;")
}

func TestTranscriptFragmentMarksCompaction(t *testing.T) {
	snap := session.Snapshot{Messages: []session.Message{{
		Role:         session.RoleCompaction,
		TokensBefore: 4200,
	}}}
	out := transcriptFragment(snap)
	assert.Contains(t, out, "Compacted context (4200 tokens)")
}

func TestTranscriptFragmentUsageOmitsZeroTPS(t *testing.T) {
	snap := session.Snapshot{Messages: []session.Message{{
		Role:    session.RoleAssistant,
		Content: []session.ContentBlock{{Type: session.BlockText, Text: "answer"}},
		Usage:   session.TokenUsage{PromptTokens: 10, CompletionTokens: 4},
	}}}
	out := transcriptFragment(snap)
	assert.Contains(t, out, "10 in · 4 out")
	assert.NotContains(t, out, "tok/s", "a zero TPS must not print a meaningless rate")
}

func TestTranscriptFragmentEmptySessionShowsStartPage(t *testing.T) {
	out := transcriptFragment(session.Snapshot{})
	assert.Contains(t, out, `id="start-page"`, "an empty session must match the client-side start page")
	assert.Contains(t, out, "What can I do for you?")
}

func TestSessionListFragmentEscapesPreview(t *testing.T) {
	sessions := []session.SessionMeta{{
		ID:        "abc",
		Preview:   `<img src=x onerror=alert(1)>`,
		Timestamp: "2026-10-09T10-00-00",
	}}
	out := sessionListFragment(sessions, nil)
	assert.NotContains(t, out, "<img")
	assert.Contains(t, out, "&lt;img")
	assert.Contains(t, out, `data-session-id="abc"`)
	// The row carries its own title so the topbar can show a name once the
	// server mints the id (before the sidebar has a row to read one from).
	assert.Contains(t, out, `data-title="&lt;img`)
}

func TestSessionListFragmentEmptyState(t *testing.T) {
	out := sessionListFragment(nil, nil)
	assert.Contains(t, out, "sessions-empty")
	// No Tailwind utilities: this fragment is swapped in by htmx after load,
	// and the Tailwind browser build only scans the initial document.
	assert.NotContains(t, out, `class="px-`)
	assert.NotContains(t, out, `class="text-sm`)
}

func TestSessionListFragmentPinsFloatToTop(t *testing.T) {
	sessions := []session.SessionMeta{
		{ID: "newest", Preview: "newest", Mtime: time.Unix(300, 0)},
		{ID: "middle", Preview: "middle", Mtime: time.Unix(200, 0)},
		{ID: "oldest", Preview: "oldest", Mtime: time.Unix(100, 0)},
	}
	out := sessionListFragment(sessions, []string{"oldest"})
	firstIdx := strings.Index(out, `data-session-id="oldest"`)
	require.Positive(t, firstIdx, "the pinned session must be present")
	assert.Less(t, firstIdx, strings.Index(out, `data-session-id="newest"`),
		"a pinned session sorts above the newest unpinned one")
	assert.Contains(t, out, "session-pinned")
	// The pin glyph is a filled star only for the pinned row.
	assert.Equal(t, 1, strings.Count(out, "&#9733;"))
	assert.Equal(t, 2, strings.Count(out, "&#9734;"))
}

func TestOrderByPinPreservesInputOrderWithinGroups(t *testing.T) {
	sessions := []session.SessionMeta{
		{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
	}
	got := orderByPin(sessions, pinnedSet([]string{"d", "b"}))
	assert.Equal(t, []string{"b", "d", "a", "c"}, ids(got),
		"pinned order follows the pin list; the rest keep their input order")
}

func ids(sessions []session.SessionMeta) []string {
	out := make([]string, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.ID)
	}
	return out
}

func TestToolDetailTextPrefersDetailThenError(t *testing.T) {
	long := strings.Repeat("y", 400)
	assert.Equal(t, "ls -1", toolDetailText(session.ToolRun{Detail: "ls -1", Output: long}))
	assert.Equal(t, "boom", toolDetailText(session.ToolRun{Error: "boom", Output: long}))
	trimmed := toolDetailText(session.ToolRun{Output: long})
	require.True(t, strings.HasPrefix(trimmed, "yyy"))
	assert.Contains(t, trimmed, "…", "a long output is truncated with an ellipsis")
	assert.LessOrEqual(t, len(trimmed), 160+len("…"), "a long output must be truncated to one line")
}
