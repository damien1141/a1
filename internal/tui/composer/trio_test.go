package composer

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/mention"
	"github.com/damien1141/a1/internal/tui/controller"
)

// Blank and duplicate submissions never reach the history bank: an accidental
// Enter on an empty composer must not pollute Up-recall, and re-running the
// last command must not stack copies.
func TestComposerPushHistory_RejectsBlankAndDuplicateOfLast_Negative(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	var stored []string
	c.SetHistoryStore(func(s string) { stored = append(stored, s) })

	c.pushHistory("")
	c.pushHistory("   \t\n  ")
	assert.Empty(t, c.history, "blank text never enters history")
	assert.Empty(t, stored, "blank text is never persisted")

	c.pushHistory("hello")
	require.Len(t, c.history, 1)
	c.pushHistory("  hello  ") // trims to the same entry as the last one
	assert.Len(t, c.history, 1, "a duplicate of the last entry is dropped")
	assert.Equal(t, []string{"hello"}, stored)

	// A duplicate of an older (non-last) entry is kept: re-running an old
	// command is a real submission, not noise.
	c.pushHistory("world")
	c.pushHistory("hello")
	assert.Len(t, c.history, 3)
	assert.Equal(t, []string{"hello", "world", "hello"}, stored)

	// Each accepted entry also resets the browse state.
	assert.Equal(t, -1, c.historyIdx)
	assert.Empty(t, c.draft)
}

// resolveMentionPath is the boundary between picker text and the filesystem.
// Hostile mention paths must resolve by filepath rules, never panic, and never
// silently drop the cwd for relative paths.
func TestComposerResolveMentionPath_HostileInputs_Negative(t *testing.T) {
	// An absolute mention overrides the cwd instead of being joined under it.
	assert.Equal(t, "/etc/passwd", resolveMentionPath("/repo", "/etc/passwd"))
	// Unclean absolute input is normalized, not rejected.
	assert.Equal(t, "/etc/passwd", resolveMentionPath("/repo", "/etc//passwd/"))
	// Whitespace around the picked path is trimmed before joining.
	assert.Equal(t, "/repo/main.go", resolveMentionPath("/repo", "  main.go\t"))
	// An empty mention resolves to the cwd itself, not "" or "/".
	assert.Equal(t, "/repo", resolveMentionPath("/repo", ""))
	// Parent traversal is resolved by Join/Clean semantics.
	assert.Equal(t, "/etc", resolveMentionPath("/repo/sub", "../../etc"))
}

// A mention result from a superseded search (or arriving after the picker
// closed) is the classic stale-async race: it must be dropped, not rendered.
func TestComposerApplyMentionResults_StaleAndClosedResultsAreDropped_Negative(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c.mention.Show()
	c.ApplyMentionResults(controller.MentionResultsMsg{Gen: c.mentionGen, Paths: []string{"first.go"}})
	require.Len(t, c.mention.Items, 1)

	stale := controller.MentionResultsMsg{Gen: c.mentionGen + 7, Paths: []string{"late.go"}}
	c.ApplyMentionResults(stale)
	assert.Len(t, c.mention.Items, 1, "a superseded result must not overwrite the list")
	assert.Equal(t, "first.go", c.mention.Items[0].Path)

	// A stale error must not scare the user either.
	c.ApplyMentionResults(controller.MentionResultsMsg{Gen: stale.Gen, ErrText: "fd exploded"})
	assert.Empty(t, c.mention.Status, "a stale error stays quiet")

	// A current-generation result arriving after the picker closed is equally dead.
	c.mention.Hide()
	c.ApplyMentionResults(controller.MentionResultsMsg{Gen: c.mentionGen, Paths: []string{"after.go"}})
	assert.Len(t, c.mention.Items, 1, "a closed picker ignores fresh results")
	assert.NotContains(t, c.mention.Items[0].Path, "after.go")
}

// History recall must never fire while a completion picker owns the composer:
// Up/Down belong to the picker, and a stray recall would stash over the query.
func TestComposerHistoryNav_RefusedWhilePickerOpen_Negative(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.pushHistory("older entry")
	c.Chat.Value = "half-typed"
	c.Chat.Cursor = len(c.Chat.Value)

	open := map[string]func(){
		"mention":  func() { c.Chat.MentionOpen = true },
		"slash":    func() { c.Chat.SlashOpen = true },
		"question": func() { c.Chat.QuestionOpen = true },
	}
	for name, openPicker := range open {
		openPicker()
		assert.False(t, c.historyNav(-1), "%s picker open: Up is not history's", name)
		assert.Equal(t, "half-typed", c.Chat.Value, "%s picker open: input survives", name)
		assert.Equal(t, -1, c.historyIdx, "%s picker open: browse index untouched", name)
		c.Chat.MentionOpen, c.Chat.SlashOpen, c.Chat.QuestionOpen = false, false, false
	}

	// With the pickers closed the same key recalls history.
	require.True(t, c.historyNav(-1))
	assert.Equal(t, "older entry", c.Chat.Value)
}

// The question picker's filter is the package's other pure text helper. Hostile
// queries must yield an empty list (so the picker can say "no match") rather
// than a fallthrough to the full list.
func TestComposerFilterQuestionItems_HostileQuery_Negative(t *testing.T) {
	all := questionShortcutItems()
	require.NotEmpty(t, all)

	assert.Empty(t, filterQuestionItems("zzz-no-such-shortcut", all), "no match yields an empty list")
	assert.Empty(t, filterQuestionItems("!@#$%^&*", all), "punctuation-only queries match nothing")

	// Whitespace-only behaves like empty: the full list.
	assert.Len(t, filterQuestionItems("   ", all), len(all))
	assert.Len(t, filterQuestionItems("", all), len(all))

	// Case is the only normalization; a shouted query still matches.
	got := filterQuestionItems("CTRL", all)
	require.NotEmpty(t, got)
	for _, it := range got {
		assert.Contains(t, strings.ToLower(it.Path), "ctrl")
	}

	// Wired end to end, a no-match query surfaces the picker's status line.
	c := NewComposerPane(components.DefaultTheme(), "m", "/tmp")
	c.onQuestionChange(true, "zzz-no-such-shortcut")
	assert.Equal(t, "No matching shortcuts", c.question.Status)
	assert.True(t, c.Chat.QuestionOpen)
}

// acceptMention with no live @-token falls back to inserting at the caret
// instead of panicking or eating text. Hostile carets are part of the
// contract: ReplaceRange clamps both bounds (the old code panicked on a
// cursor beyond len(Value) here — fixed in the chat package, now pinned from
// the composer side too).
func TestComposerAcceptMention_NoActiveMentionFallsBackToCaret_Negative(t *testing.T) {
	c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())

	// No @ anywhere: the caret position itself is the insert point.
	c.Chat.Value = "hello"
	c.Chat.Cursor = len(c.Chat.Value)
	c.acceptMention(mention.Item{Path: "no-such-file.go"})
	assert.Equal(t, "hello@no-such-file.go ", c.Chat.Value)
	assert.False(t, c.mention.Open, "the picker closes even on the fallback path")
	assert.False(t, c.Chat.MentionOpen)

	// Mid-string caret: insertion splits the text, it does not overwrite it.
	c2 := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c2.Chat.Value = "ab"
	c2.Chat.Cursor = 1
	c2.acceptMention(mention.Item{Path: "no-such-file.go"})
	assert.Equal(t, "a@no-such-file.go b", c2.Chat.Value)

	// A cursor far past the end with no live token: clamps to the end, so
	// the mention appends instead of panicking inside ReplaceRange.
	c2b := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c2b.Chat.Value = "ab"
	c2b.Chat.Cursor = 999
	c2b.acceptMention(mention.Item{Path: "no-such-file.go"})
	assert.Equal(t, "ab@no-such-file.go ", c2b.Chat.Value)

	// A negative cursor with no live token: clamps to zero, so the mention
	// lands at the start.
	c2c := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c2c.Chat.Value = "ab"
	c2c.Chat.Cursor = -9
	c2c.acceptMention(mention.Item{Path: "no-such-file.go"})
	assert.Equal(t, "@no-such-file.go ab", c2c.Chat.Value)

	// A cursor far past the end while a mention token is live: the token scan
	// clamps to len(Value), so the replace lands on the token, not past it.
	c3 := NewComposerPane(components.DefaultTheme(), "m", t.TempDir())
	c3.Chat.Value = "@go"
	c3.Chat.Cursor = 999
	c3.acceptMention(mention.Item{Path: "no-such-file.go"})
	assert.Equal(t, "@no-such-file.go ", c3.Chat.Value)
}

// The pane is constructed before Editor assembly finishes wiring it, and half
// of its methods are called from teardown paths — a nil receiver must be a
// no-op, never a panic.
func TestComposerPane_NilReceiverGuards_Negative(t *testing.T) {
	var c *ComposerPane
	c.HideCompleters()
	c.HidePalette()
	c.ClearInput()
	c.SetInput("text")
	c.LoadHistory([]string{"a"})
	c.SyncBashBorder("!bang")
	c.CloseMentionSlash()
	c.SetBashBorderActive(true)
	c.ClearPendingImages()
	c.ClearPendingSkills()
	c.ClearPendingRefs()
	c.SetModelLabel("m", "high")
	c.SetBranchLabel("br")
	c.PushPalette("title", nil)
	c.SetTheme(components.DefaultTheme())

	assert.Nil(t, c.PendingSkills())
	assert.Nil(t, c.PendingImages())
	assert.Nil(t, c.PendingRefs())
	assert.Equal(t, 5, c.PreferredHeight(80, xui.WidthUnicode), "nil pane reports the minimum height")
	assert.Nil(t, c.PickerOverlays(components.DrawContext{}, 3, 80))

	_, ok := c.ListOverlay(components.DrawContext{})
	assert.False(t, ok)
	_, ok = c.PaletteOverlay(components.DrawContext{})
	assert.False(t, ok)
	assert.Equal(t, components.Surface{}, c.DrawChat(components.DrawContext{}, 40, 5))
}
