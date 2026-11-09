package contextbreakdown_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/agent/prompt"
	"github.com/damien1141/a1/internal/contextbreakdown"
	"github.com/damien1141/a1/internal/session"
)

// naiveTokens is the oracle for the 4-bytes-per-token estimate: count how
// many times 4 fits, rounding up. A loop, not the package's (n+3)/4.
func naiveTokens(bytes int) int {
	var n int
	for ; bytes > 0; bytes -= 4 {
		n++
	}
	return n
}

// trioCorpus is a fixed message corpus whose bucket sizes are hand-derived:
//
//	msgs     = 1 + 3 + 4 + 13 = 21 (Text fields and BlockText)
//	thinking = 5 + 7          = 12
//	toolIO   = 8 + 9 + 10 + 4 = 31 (tool inputs plus outputs/errors)
//
// It also carries hostile shapes: an unknown Role(99), a RoleCompaction
// message, an unknown BlockType(42) whose text must be dropped, and a tool
// run with only an Error.
func trioCorpus() session.Snapshot {
	return session.Snapshot{
		Messages: []session.Message{
			{ID: "m1", Role: session.Role(99), Text: "a"},
			{ID: "m2", Role: session.RoleUser, Text: "abc"},
			{
				ID: "m3", Role: session.RoleAssistant, State: session.StateComplete,
				Content: []session.ContentBlock{
					{Type: session.BlockText, Text: "abcd"},
					{Type: session.BlockThinking, Text: "abcde"},
					{Type: session.BlockThinking, Text: "abcdefg"},
					{Type: session.BlockToolUse, ID: "t1", Name: "bash", Input: "abcdefgh"},
					{Type: session.BlockToolUse, ID: "t2", Name: "read", Input: "abcdefghi"},
					{Type: session.BlockType(42), Text: "dropped-unknown-block"},
				},
			},
			{ID: "m4", Role: session.RoleCompaction, Text: "1234567890123"},
		},
		Tools: map[string]session.ToolRun{
			"t1": {ToolUseID: "t1", Name: "bash", Status: session.ToolDone, Output: "ABCDEFGHIJ"},
			"t2": {ToolUseID: "t2", Name: "read", Status: session.ToolDone, Error: "boom"},
		},
	}
}

// naiveBucketBytes recounts the corpus with plain loops: three separate
// passes where Build switches on block type inside one pass.
func naiveBucketBytes(snap session.Snapshot) (msgs, thinking, toolIO int) {
	for _, m := range snap.Messages {
		msgs += len(m.Text)
		for _, blk := range m.Content {
			switch blk.Type {
			case session.BlockText:
				msgs += len(blk.Text)
			case session.BlockThinking:
				thinking += len(blk.Text)
			case session.BlockToolUse:
				toolIO += len(blk.Input)
			}
		}
	}
	for _, r := range snap.Tools {
		toolIO += len(r.Output) + len(r.Error)
	}
	return msgs, thinking, toolIO
}

// ---------- differential ----------

// oracle: naive reference implementation — the corpus byte buckets recounted
// by the loops above and converted through the count-by-4 loop, with the
// system-prompt row checked the same way against the prompt's own bytes.
func TestBuild_Differential_TokenArithmetic(t *testing.T) {
	snap := trioCorpus()
	msgs, thinking, toolIO := naiveBucketBytes(snap)
	assert.Equal(t, 21, msgs, "hand-derived: 1+3+4+13")
	assert.Equal(t, 12, thinking, "hand-derived: 5+7")
	assert.Equal(t, 31, toolIO, "hand-derived: 8+9+10+4")

	b := contextbreakdown.Build(snap, 1000, 100, 7)
	require.Len(t, b.Categories, 5)
	assert.Equal(t, "System prompt", b.Categories[0].Name)
	assert.Equal(t, "Tools", b.Categories[1].Name)
	assert.Equal(t, "Messages", b.Categories[2].Name)
	assert.Equal(t, "Thinking", b.Categories[3].Name)
	assert.Equal(t, "Tool output", b.Categories[4].Name)

	sysWant := naiveTokens(len(prompt.Build("", false, 0, nil)))
	assert.Equal(t, sysWant, b.Categories[0].Tokens, "system prompt bytes")
	assert.Equal(t, naiveTokens(100), b.Categories[1].Tokens, "tool schema bytes")
	assert.Equal(t, "7 registered", b.Categories[1].Note)
	assert.Equal(t, naiveTokens(msgs), b.Categories[2].Tokens)
	assert.Equal(t, naiveTokens(thinking), b.Categories[3].Tokens)
	assert.Equal(t, naiveTokens(toolIO), b.Categories[4].Tokens)

	// The Tools row carries a note, so it stays out of the window total.
	totalWant := sysWant + naiveTokens(msgs) + naiveTokens(thinking) + naiveTokens(toolIO)
	assert.Equal(t, totalWant, b.Total())
	pctWant := totalWant * 100 / 1000
	if pctWant > 999 {
		pctWant = 999
	}
	assert.Equal(t, pctWant, b.UsedPercent())
}

// oracle: stdlib len (byte length) fed to the count-by-4 loop — the estimate
// is byte-based, so multibyte text must count bytes, not runes.
func TestPromptTokens_Differential_ByteBased(t *testing.T) {
	for _, s := range []string{
		"",
		"a",
		"abcd",
		"abcde",
		"日本語テキスト", // 21 bytes, not 7 runes
		strings.Repeat("x", 101),
	} {
		assert.Equal(t, naiveTokens(len(s)), contextbreakdown.PromptTokens(s), "len(%q) = %d", s, len(s))
	}
}

// oracle: hand-written golden literals — the full rendered output for fixed
// breakdowns (column widths, thousands separators, cache hit rates) written
// out by hand, not derived from a run.
func TestEstimate_Differential_GoldenRendering(t *testing.T) {
	t.Run("known window with cache section", func(t *testing.T) {
		b := contextbreakdown.Breakdown{
			Categories: []contextbreakdown.Category{
				{Name: "System prompt", Tokens: 1234567},
				{Name: "Tools", Tokens: 9876543, Note: "12 registered"},
				{Name: "Messages", Tokens: 999},
			},
			ContextWindow: 10000000,
			Cache: &contextbreakdown.CacheStats{
				Last:          session.TokenUsage{PromptTokens: 50, CachedTokens: 9000, CacheWriteTokens: 100},
				SessionReads:  9000,
				SessionWrites: 1000,
				SessionFresh:  1050,
			},
		}
		want := `Context estimate (~4 bytes/token, provider counts will differ)

System prompt   1,234,567
Tools           9,876,543  12 registered
Messages              999

Total 1,235,566 / 10,000,000 (12% used)

Prompt cache (provider-reported, exact)
  last request  9,000 read · 100 write · 50 fresh  (98% hit)
  session       9,000 read · 1,000 write · 1,050 fresh  (81% hit)
`
		assert.Equal(t, want, b.Estimate())
	})

	t.Run("unknown window footer", func(t *testing.T) {
		b := contextbreakdown.Breakdown{
			Categories:    []contextbreakdown.Category{{Name: "System prompt", Tokens: 5}},
			ContextWindow: 0,
		}
		want := `Context estimate (~4 bytes/token, provider counts will differ)

System prompt           5

Total 5 (window unknown for this model)
`
		assert.Equal(t, want, b.Estimate())
	})
}

// ---------- negative ----------

// Build is role-agnostic and block-type-strict: an unknown role's text still
// counts as message bytes, while an unknown block type's text is dropped
// rather than misfiled into a bucket.
func TestBuild_Negative_UnknownRoleAndBlockType(t *testing.T) {
	b := contextbreakdown.Build(trioCorpus(), 1000, 0, 0)

	require.Len(t, b.Categories, 4, "no Tools row without a tool count")
	assert.Equal(t, naiveTokens(21), b.Categories[1].Tokens,
		"the unknown role's and compaction role's text count: 21 bytes, not 20 or 42")
	assert.Equal(t, naiveTokens(12), b.Categories[2].Tokens)
	assert.Equal(t, naiveTokens(31), b.Categories[3].Tokens,
		"the unknown block type's 21 bytes are dropped: 31, not 52")
}

func TestBuild_Negative_HostileNumbers(t *testing.T) {
	t.Run("negative window reads as unknown", func(t *testing.T) {
		b := contextbreakdown.Build(session.Snapshot{}, -5, 0, 0)
		assert.Equal(t, -5, b.ContextWindow)
		assert.Equal(t, 0, b.UsedPercent(), "a negative window is not a window")
		assert.Contains(t, b.Estimate(), "window unknown for this model")
	})

	t.Run("negative tool bytes estimate below zero", func(t *testing.T) {
		// Actual behavior, pinned: (n+3)/4 truncates toward zero, so -8
		// bytes estimate to -1 tokens rather than clamping at 0.
		b := contextbreakdown.Build(session.Snapshot{}, 1000, -8, 3)
		require.Len(t, b.Categories, 2)
		assert.Equal(t, "Tools", b.Categories[1].Name)
		assert.Equal(t, -1, b.Categories[1].Tokens)
		assert.Equal(t, "3 registered", b.Categories[1].Note)
		// The noted Tools row stays informational: the negative estimate
		// never leaks into Total.
		assert.Equal(t, b.Categories[0].Tokens, b.Total())
	})

	t.Run("zero tool count drops the tools row despite bytes", func(t *testing.T) {
		b := contextbreakdown.Build(session.Snapshot{}, 1000, 4096, 0)
		for _, c := range b.Categories {
			assert.NotEqual(t, "Tools", c.Name)
		}
	})

	t.Run("empty transcript reports no cache", func(t *testing.T) {
		b := contextbreakdown.Build(session.Snapshot{}, 200000, 0, 0)
		require.Len(t, b.Categories, 1)
		assert.Nil(t, b.Cache)
		assert.NotContains(t, b.Estimate(), "Prompt cache")
	})
}

func TestBuild_Negative_HugeCorpus(t *testing.T) {
	const mib = 1 << 20
	snap := session.Snapshot{Messages: []session.Message{
		{ID: "u1", Role: session.RoleUser, Text: strings.Repeat("x", 8*mib)},
	}}
	b := contextbreakdown.Build(snap, 1000, 0, 0)

	msgs := 0
	for _, c := range b.Categories {
		if c.Name == "Messages" {
			msgs = c.Tokens
		}
	}
	assert.Equal(t, 2*mib, msgs, "8 MiB of text at 4 bytes/token")
	assert.Equal(t, 999, b.UsedPercent(), "an over-window breakdown stays capped")
	assert.Contains(t, b.Estimate(), "2,097,152")
}
