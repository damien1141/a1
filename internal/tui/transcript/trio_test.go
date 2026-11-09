package transcript

import (
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui"
	"github.com/stretchr/testify/assert"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
)

// A nil *TranscriptPane must behave like an empty pane: zero values out of
// every accessor and no-ops out of every mutator, never a panic.
func TestTranscriptPane_NilReceiverGuards_Negative(t *testing.T) {
	var pane *TranscriptPane

	assert.Equal(t, session.Snapshot{}, pane.Snapshot())
	assert.False(t, pane.IsStreaming())
	assert.True(t, pane.IsEmpty())
	assert.Empty(t, pane.LastCopyText())
	assert.True(t, pane.AtBottom())
	assert.Equal(t, 0, pane.ListHeight())
	assert.Nil(t, pane.Entries())
	assert.Nil(t, pane.EntryIDs())
	assert.Nil(t, pane.Actions())
	assert.False(t, pane.SelectionActive())

	assert.NotPanics(t, func() {
		pane.ClearSelection()
		pane.ApplySession(session.UserAppend{Text: "x"})
		pane.LoadReplay(session.Snapshot{})
		pane.ResetSubagents()
		pane.Sync()
		pane.SetTheme(components.DefaultTheme())
		pane.SetAutoExpandToolOutput(true)
		pane.SetChecklistFile("TODO.md")
		pane.SetUsageCallback(nil)
		pane.SetCopyHandlers(nil, nil)
		pane.HandlePageKey(&components.EventContext{}, xui.KeyEvent{Press: true, Code: xui.KeyDown})
	})
	assert.Equal(t, components.Surface{}, pane.Draw(components.DrawContext{}, 10, 5))
	assert.False(t, pane.ApplyJobProgress(job.Progress{JobID: "j"}), "nil pane reports no sync need")
}

// SubagentStore with nil receivers, empty identifiers, and unknown lookups:
// every hostile shape is a no-op returning nil/false, never a panic.
func TestSubagentStore_HostileInput_Negative(t *testing.T) {
	var nilStore *SubagentStore
	assert.NotPanics(t, func() {
		nilStore.Bind("job", "parent")
		nilStore.ApplyProgress(job.Progress{JobID: "job"})
		nilStore.ApplyResult("parent", tools.AgentResult{OK: true})
	})
	assert.Nil(t, nilStore.Children("parent"))
	assert.Nil(t, nilStore.ChildrenByJob("job"))

	s := NewSubagentStore()
	s.Bind("", "parent")
	assert.Nil(t, s.Children("parent"), "a bind without a job id is dropped")
	s.Bind("job", "")
	assert.Nil(t, s.ChildrenByJob("job"), "a bind without a parent id is dropped")

	assert.False(t, s.ApplyProgress(job.Progress{}), "progress with no identifiers is ignored")
	assert.NotPanics(t, func() { s.ApplyResult("parent", tools.AgentResult{OK: false}) })
	assert.Nil(t, s.Children(""))
	assert.Nil(t, s.ChildrenByJob(""))
	assert.Nil(t, s.Children("never-bound"), "unknown parent has no children")
}

// ToolData routing only binds agent_* tools whose output parses as agent JSON;
// anything else (other tools, garbage payloads) must leave the store empty.
func TestPaneApplySession_HostileToolData_Negative(t *testing.T) {
	pane := NewTranscriptPane(components.DefaultTheme(), nil, "A1 test")

	pane.ApplySession(session.ToolData{Run: session.ToolRun{
		ToolUseID: "t1", Name: "bash", Output: `{"job_id":"j1"}`,
	}})
	assert.Nil(t, pane.subagents.Children("t1"), "non-agent tools never bind")

	pane.ApplySession(session.ToolData{Run: session.ToolRun{
		ToolUseID: "t2", Name: "agent_spawn", Output: "not json at all",
	}})
	assert.Nil(t, pane.subagents.Children("t2"), "unparseable agent output never binds")

	pane.ApplySession(session.ToolData{Run: session.ToolRun{
		ToolUseID: "t3", Name: "AGENT_WAIT", Output: "",
	}})
	assert.Nil(t, pane.subagents.Children("t3"), "empty output never binds")
}

// oracle: stdlib slices.Equal — sameIDSet is order-sensitive list equality.
func TestSameIDSet_SlicesEqual_Differential(t *testing.T) {
	fixed := [][2][]string{
		{nil, nil},
		{nil, {}},
		{{"a"}, nil},
		{{"a"}, {"a"}},
		{{"a"}, {"b"}},
		{{"a", "b"}, {"b", "a"}},
		{{"a", "b"}, {"a", "b", "a"}},
		{{"", ""}, {""}},
		{{"checklist", "m1"}, {"checklist", "m1"}},
	}
	for _, c := range fixed {
		assert.Equal(t, slices.Equal(c[0], c[1]), sameIDSet(c[0], c[1]), "sameIDSet(%q, %q)", c[0], c[1])
	}

	alphabet := []string{"a", "b", "c", "d"}
	rng := rand.New(rand.NewSource(11))
	randList := func() []string {
		out := make([]string, rng.Intn(6))
		for i := range out {
			out[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return out
	}
	for range 300 {
		a, b := randList(), randList()
		assert.Equal(t, slices.Equal(a, b), sameIDSet(a, b), "sameIDSet(%q, %q)", a, b)
	}
}

// oracle: hand-derived golden table — a detail with no space or quote is the
// path (after dropping an "@file " prefix); anything else is the search phrase.
func TestReadRowTarget_GoldenTable_Differential(t *testing.T) {
	cases := []struct {
		detail string
		path   string
		phrase string
	}{
		{"", "", ""},
		{"   ", "", ""},
		{"a.go", "a.go", ""},
		{"@file a.go", "a.go", ""},
		{"  @file a.go  ", "a.go", ""},
		{"@file", "@file", ""},
		{"@file a.go b.go", "", "a.go b.go"},
		{"pattern foo", "", "pattern foo"},
		{`a.go "q"`, "", `a.go "q"`},
		{`"quoted"`, "", `"quoted"`},
		{"/abs/path/x.txt", "/abs/path/x.txt", ""},
	}
	for _, c := range cases {
		path, detail := readRowTarget(session.ToolRun{Detail: c.detail})
		assert.Equal(t, c.path, path, "path for detail %q", c.detail)
		assert.Equal(t, c.phrase, detail, "phrase for detail %q", c.detail)
	}
}

// oracle: hand-derived golden table for the verb-stripping rules — the prefix
// match is case-insensitive and the byte after the trimmed prefix must be a
// non-word byte (punctuation, slash, multibyte lead), so "grep pattern" and
// "grepfile x" both keep their verb while "grep: x" loses it.
func TestStripLeadingVerb_GoldenTable_Differential(t *testing.T) {
	cases := []struct {
		detail, name, want string
	}{
		{"grep pattern", "grep", "grep pattern"},
		{"GREP pattern", "grep", "GREP pattern"},
		{"grepfile x", "grep", "grepfile x"},
		{"grep: x", "grep", ": x"},
		{"grep -i x", "grep", "-i x"},
		{"read/a.go", "read", "/a.go"},
		{"grep", "grep", "grep"},
		{"  grep  ", "grep", "grep"},
		{"", "grep", ""},
		{"anything", "read", "anything"},
		{"anything", "", "anything"},
		{"readé", "read", "é"},
		{"read: a.go", "READ", ": a.go"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, stripLeadingVerb(c.detail, c.name), "stripLeadingVerb(%q, %q)", c.detail, c.name)
	}
}

// naiveStripReadHeader drops the first non-blank line when it is an "@file "
// header, rebuilding the remainder with a separator-first builder instead of
// slice surgery, then trims leading newlines.
func naiveStripReadHeader(body string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(ln), "@file ") {
			return body
		}
		var b strings.Builder
		first := true
		for j, rest := range lines {
			if j == i {
				continue
			}
			if !first {
				b.WriteString("\n")
			}
			b.WriteString(rest)
			first = false
		}
		return strings.TrimLeft(b.String(), "\n")
	}
	return body
}

// oracle: naive builder reference (above) over header placements and shapes;
// stripReadHeader must agree on which line counts as the header and what
// survives after it is removed.
func TestStripReadHeader_NaiveReference_Differential(t *testing.T) {
	corpus := []string{
		"", "@file a", "@file a\n", "@file a\ncode\n", "@file a\n\ncode",
		"code", "code\n", "code\nmore\n", "@file a\ncode\nmore\n",
		"\n\n@file a\nx", "\n@file a\n\nx\n", "  @file a  \nx",
		"grep notice\n@file a\nx", "@file a\n@file b\nx",
		"@filea\nx", "@file", "@file \nx", "\n\n\n", "  \n  @file a\ny\n",
	}
	for _, body := range corpus {
		assert.Equal(t, naiveStripReadHeader(body), stripReadHeader(body), "stripReadHeader(%q)", body)
	}
}
