package transcript

import (
	"os"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/block"
	"github.com/damien1141/a1/internal/components/status"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/todo"
	"github.com/damien1141/a1/internal/tools"
)

// Mapper converts session.Snapshot items into transcript widgets.
// It owns expand-state and has no dependency on Editor / xui / agent.
type Mapper struct {
	theme        components.Theme
	spinner      *status.Spinner
	expanded     map[string]bool
	onInvalidate func() // e.g. MessageList.InvalidateHeights
	// Children returns nested sub-agent tool rows for a parent tool_use id.
	Children func(parentToolUseID string) []block.ChildTool
	// ChildrenByJob returns nested rows keyed by job id (fallback for spawn/task).
	ChildrenByJob func(jobID string) []block.ChildTool

	// AutoExpand opens file-editing rows, so diffs are visible without a
	// keypress. Read/grep and bash rows keep their compact collapsed forms
	// either way — see autoExpandFor for the policy. A row the user collapses
	// still wins, because m.expanded records the user's choice once they touch it.
	AutoExpand bool

	// snap is the last snapshot handed to Sync, read when a collapsed group's
	// expansion resolves its member rows. Never mutated here.
	snap session.Snapshot

	// Checklist is the project task file rendered as a live progress row. Nil
	// disables the row entirely, which is the default for callers that have no
	// project root (tests, the web surface).
	Checklist *ChecklistSource
}

// ChecklistSource polls a task file and caches its parse. The zero value is
// inert; use NewChecklistSource.
//
// The row is synthesized by the Mapper rather than projected from the session,
// because a checklist is workspace state, not conversation content: it has to
// exist before the first message and survive a replay. File identity is
// (mtime, size) rather than content, so a frame that changes nothing does no
// work beyond one stat.
type ChecklistSource struct {
	// Path is the task file to read.
	Path string
	// StaleFor counts file-editing tool rows since the checklist last moved.
	// Non-zero renders a staleness warning on the row.
	StaleFor int

	lastMod   time.Time
	lastSize  int64
	items     []todo.Item
	changedAt time.Time
	loaded    bool
}

// ChecklistSourceEntryID is the synthetic item id of the checklist row.
const ChecklistSourceEntryID = "checklist"

// NewMapper builds a Mapper with the given theme, spinner, and invalidation callback.
func NewMapper(theme components.Theme, spinner *status.Spinner, onInvalidate func()) *Mapper {
	return &Mapper{
		theme:        theme,
		spinner:      spinner,
		expanded:     make(map[string]bool),
		onInvalidate: onInvalidate,
	}
}

// SetTheme updates the theme used for newly built and patched widgets.
func (m *Mapper) SetTheme(theme components.Theme) {
	if m != nil {
		m.theme = theme
	}
}

// Sync rebuilds the widget list from snap, reusing widgets when patchable.
// dirty lists new-entry indices whose height-relevant content changed (or are new).
func (m *Mapper) Sync(
	entries []components.Widget,
	listIDs []string,
	snap session.Snapshot,
) (newEntries []components.Widget, newIDs []string, dirty []int) {
	m.snap = snap
	items := session.Project(snap)
	n := len(items)
	byID := make(map[string]int, len(entries))
	for i, w := range entries {
		id := entryID(listIDs, i)
		if id == "" {
			continue
		}
		byID[id] = i
		switch b := w.(type) {
		case *block.ThinkingBlock:
			m.expanded[id] = b.Expanded
		case *block.ToolBlock:
			m.expanded[id] = b.Expanded
		case *block.ReadBlock:
			m.expanded[id] = b.Expanded
		case *block.ReadGroupBlock:
			m.expanded[id] = b.Expanded
		case *block.DiffBlock:
			m.expanded[id] = b.Expanded
		case *block.BashBlock:
			m.expanded[id] = b.Expanded
		case *block.AgentBlock:
			m.expanded[id] = b.Expanded
		}
	}

	// The checklist row is not part of the projected items: it leads the
	// transcript when a task file exists, because "what am I doing" is the first
	// question during a long run.
	newEntries = make([]components.Widget, 0, n+1)
	newIDs = make([]string, 0, n+1)
	// The staleness count comes from the projected rows, so it has to be
	// computed before the widget is built — it is part of what the row draws.
	// refresh() runs first because it is what flips loaded on the very first
	// sync, when the file already exists before the first frame.
	if m.Checklist != nil && m.Checklist.Path != "" {
		m.Checklist.refresh()
		m.Checklist.StaleFor = countEditTools(items)
	}
	if cl := m.checklistWidget(); cl != nil {
		// The row is rebuilt every frame because a file edit can change it
		// without any session event. Marking it dirty only when the content
		// actually moved keeps the height cache stable: a frame where the file
		// is untouched costs one stat and no relayout.
		// byID is keyed by entry id, and index 0 was captured into it before
		// this row was appended — so a present checklist resolves to whichever
		// entry previously held the id, not blindly to entries[0].
		prevIdx, seen := byID[ChecklistSourceEntryID]
		if !seen || !checklistEqual(entries[prevIdx], cl) {
			dirty = append(dirty, 0)
		}
		newEntries = append(newEntries, cl)
		newIDs = append(newIDs, ChecklistSourceEntryID)
	}
	for _, it := range items {
		idx := len(newEntries)
		newIDs = append(newIDs, it.ID)
		if oldIdx, ok := byID[it.ID]; ok {
			if ok, changed := m.patchItem(entries[oldIdx], it); ok {
				newEntries = append(newEntries, entries[oldIdx])
				if changed {
					dirty = append(dirty, idx)
				}
				continue
			}
		}
		newEntries = append(newEntries, m.widgetFor(it))
		dirty = append(dirty, idx)
	}
	return newEntries, newIDs, dirty
}

func entryID(listIDs []string, i int) string {
	if i >= 0 && i < len(listIDs) {
		return listIDs[i]
	}
	return ""
}

func (m *Mapper) patchItem(w components.Widget, it session.Item) (ok, dirty bool) {
	switch it.Kind {
	case session.ItemUser:
		u, ok := w.(*block.UserBlock)
		if !ok {
			return false, false
		}
		dirty = u.Text != it.Text || u.ItemID != it.ID
		u.Text = it.Text
		u.ItemID = it.ID
		u.Theme = m.theme
		return true, dirty
	case session.ItemAssistant:
		a, ok := w.(*block.AssistantBlock)
		if !ok {
			return false, false
		}
		dirty = a.Text != it.Text || a.State != it.State
		a.Text = it.Text
		a.State = it.State
		a.Theme = m.theme
		return true, dirty
	case session.ItemThinking:
		t, ok := w.(*block.ThinkingBlock)
		if !ok {
			return false, false
		}
		prevExp := t.Expanded
		dirty = t.Text != it.Thinking || t.Streaming != it.Streaming || t.Interrupted != it.Interrupted
		t.Text = it.Thinking
		t.Streaming = it.Streaming
		t.Interrupted = it.Interrupted
		t.Theme = m.theme
		t.Spinner = m.spinner
		if exp, ok := m.expanded[it.ID]; ok {
			t.Expanded = exp
		}
		if t.Expanded != prevExp {
			dirty = true
		}
		return true, dirty
	case session.ItemCompaction:
		c, ok := w.(*block.CompactionBlock)
		if !ok {
			return false, false
		}
		c.Theme = m.theme
		dirty := c.TokensBefore != it.TokensBefore
		c.TokensBefore = it.TokensBefore
		return true, dirty
	case session.ItemTool:
		return m.patchTool(w, it)
	}
	return false, false
}

func (m *Mapper) patchTool(w components.Widget, it session.Item) (ok, dirty bool) {
	run := it.ToolRun
	name := strings.ToLower(run.Name)
	if name == "bash" {
		b, ok := w.(*block.BashBlock)
		if !ok {
			return false, false
		}
		st := uiToolStatus(run.Status)
		prevExp := b.Expanded
		dirty = b.Command != run.Detail || b.Output != run.Output || b.Status != st || b.ExitCode != run.ExitCode
		b.Command = run.Detail
		b.Output = run.Output
		b.Status = st
		b.ExitCode = run.ExitCode
		b.Theme = m.theme
		nextExp := m.autoExpandFor(it.ID, run.Expanded, run)
		b.Expanded = nextExp
		if b.Expanded != prevExp {
			dirty = true
		}
		return true, dirty
	}
	if isAgentTreeTool(name) {
		a, ok := w.(*block.AgentBlock)
		if !ok {
			return false, false
		}
		prev := agentHeightSnap{
			Name:     a.Name,
			Detail:   a.Detail,
			Status:   a.Status,
			Error:    a.Error,
			Summary:  a.Summary,
			Expanded: a.Expanded,
			Children: a.Children,
			Activity: a.Activity,
		}
		m.fillAgentBlock(a, it)
		// Activity is in the dirty check because it is the live header text:
		// without it a sub-agent that moved from grep to read would keep
		// showing the stale tool in an already-mounted row.
		dirty = prev.Name != a.Name || prev.Detail != a.Detail || prev.Status != a.Status ||
			prev.Error != a.Error || prev.Summary != a.Summary || prev.Expanded != a.Expanded ||
			prev.Activity != a.Activity ||
			!childToolsEqual(prev.Children, a.Children)
		return true, dirty
	}
	if isReadGroupTool(name) {
		g, ok := w.(*block.ReadGroupBlock)
		if !ok {
			return false, false
		}
		prevDetail, prevStatus, prevExp, prevHint := g.Detail, g.Status, g.Expanded, g.Hint
		prevMembers := g.Members()
		g.Detail = run.Detail
		g.Status = uiToolStatus(run.Status)
		g.Hint = run.Group.Hint
		g.Theme = m.theme
		g.Spinner = m.spinner
		nextExp := m.autoExpandFor(it.ID, run.Expanded, run)
		g.Expanded = nextExp
		// Members are resolved at draw time, so their state can change without
		// any field on the group itself moving — an expanded row would keep
		// showing a stale "running" member until something else dirtied it.
		return true, prevDetail != g.Detail || prevStatus != g.Status ||
			prevExp != nextExp || prevHint != g.Hint ||
			!childToolsEqual(prevMembers, g.Members())
	}
	if isReadTool(name) {
		r, ok := w.(*block.ReadBlock)
		if !ok {
			return false, false
		}
		prevName, prevPath, prevDetail, prevBody, prevErr, prevExp := r.Name, r.Path, r.Detail, r.Body, r.Error, r.Expanded
		r.Name = run.Name
		r.Path, r.Detail = readRowTarget(run)
		r.Body = stripReadHeader(run.Output)
		r.Error = run.Error
		r.Status = uiToolStatus(run.Status)
		r.Theme = m.theme
		r.Spinner = m.spinner
		nextExp := m.autoExpandFor(it.ID, run.Expanded, run)
		r.Expanded = nextExp
		return true, prevName != r.Name || prevPath != r.Path || prevDetail != r.Detail ||
			prevBody != r.Body || prevErr != r.Error || nextExp != prevExp
	}
	if isEditTool(name) {
		d, ok := w.(*block.DiffBlock)
		if !ok {
			return false, false
		}
		prevPath, prevDiff, prevErr, prevExp := d.Path, d.Diff, d.Error, d.Expanded
		d.Path, d.Diff, d.Error = m.editToolFields(it)
		d.Status = uiToolStatus(run.Status)
		d.Theme = m.theme
		d.Spinner = m.spinner
		nextExp := m.autoExpandFor(it.ID, run.Expanded, run)
		d.Expanded = nextExp
		return true, prevPath != d.Path || prevDiff != d.Diff || prevErr != d.Error || nextExp != prevExp
	}
	t, ok := w.(*block.ToolBlock)
	if !ok {
		return false, false
	}
	st := uiToolStatus(run.Status)
	prevExp := t.Expanded
	dirty = t.Name != run.Name || t.Detail != run.Detail || t.Summary != run.Summary ||
		t.Output != run.Output || t.Error != run.Error || t.Status != st
	t.Name = run.Name
	t.Detail = run.Detail
	t.Summary = run.Summary
	t.Output = run.Output
	t.Error = run.Error
	t.Status = st
	t.Theme = m.theme
	t.Spinner = m.spinner
	if exp, ok := m.expanded[it.ID]; ok {
		t.Expanded = exp
	} else if run.Expanded {
		t.Expanded = true
	} else if t.Status == status.ToolRunning && t.Output != "" {
		t.Expanded = true
	}
	if t.Expanded != prevExp {
		dirty = true
	}
	return true, dirty
}

type agentHeightSnap struct {
	Name     string
	Detail   string
	Status   status.ToolStatus
	Error    string
	Summary  string
	Expanded bool
	Children []block.ChildTool
	Activity string
}

func childToolsEqual(a, b []block.ChildTool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *Mapper) widgetFor(it session.Item) components.Widget {
	exp := m.expanded[it.ID]
	id := it.ID
	switch it.Kind {
	case session.ItemUser:
		return &block.UserBlock{Text: it.Text, Theme: m.theme, ItemID: it.ID}
	case session.ItemThinking:
		return &block.ThinkingBlock{
			Text:        it.Thinking,
			Streaming:   it.Streaming,
			Interrupted: it.Interrupted,
			Expanded:    exp || it.Streaming,
			Theme:       m.theme,
			Spinner:     m.spinner,
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
	case session.ItemCompaction:
		return &block.CompactionBlock{Theme: m.theme, TokensBefore: it.TokensBefore}
	case session.ItemTool:
		return m.toolWidget(it, exp)
	default:
		return &block.AssistantBlock{Text: it.Text, State: it.State, Theme: m.theme}
	}
}

// autoExpandFor decides whether a fresh tool row starts open.
//
// The user's own toggle always wins (recorded in m.expanded once they touch a
// row). After that the policy is per tool family:
//
//   - read / grep start closed as a one-line summary, even with auto-expand on.
//     They are the highest-volume, least-urgent rows; a 30-file exploration is
//     30 cards of file content otherwise.
//   - bash starts closed too, but a closed bash row still renders a tail
//     preview of its output (BashCollapsedTailLines) — closing it is about
//     bounding volume, not hiding the outcome.
//   - write / edit start open with auto-expand: a diff is the one thing that
//     must not be missed.
//   - a row that streams output opens as it arrives, so running progress is
//     visible without a keypress.
func (m *Mapper) autoExpandFor(id string, exp bool, run session.ToolRun) bool {
	if _, touched := m.expanded[id]; touched {
		return m.expanded[id]
	}
	if exp || run.Local || run.Expanded {
		return true
	}
	if !m.AutoExpand {
		return run.Status == session.ToolInProgress && run.Output != ""
	}
	// Reads and bash keep their own compact forms under auto-expand: opening
	// them is how a transcript becomes a wall of file contents and logs.
	return !collapsesUnderAutoExpand(run.Name)
}

// collapsesUnderAutoExpand names the rows that keep a bounded collapsed form
// even when auto_expand_tool_output is on. A collapsed read row is a one-line
// summary; a collapsed bash row still shows a tail of its output, so closing
// them costs no information the user asked to see.
func collapsesUnderAutoExpand(name string) bool {
	switch strings.ToLower(name) {
	case "read", "grep", "bash":
		return true
	default:
		return false
	}
}

func (m *Mapper) toolWidget(it session.Item, exp bool) components.Widget {
	run := it.ToolRun
	autoExp := m.autoExpandFor(it.ID, exp, run)
	id := it.ID
	if strings.EqualFold(run.Name, "bash") {
		return &block.BashBlock{
			Command:  run.Detail,
			Output:   run.Output,
			Status:   uiToolStatus(run.Status),
			ExitCode: run.ExitCode,
			Expanded: autoExp,
			Theme:    m.theme,
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
	}
	if isAgentTreeTool(run.Name) {
		a := &block.AgentBlock{
			Theme:   m.theme,
			Spinner: m.spinner,
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
		m.fillAgentBlock(a, it)
		return a
	}
	if isReadGroupTool(run.Name) {
		return &block.ReadGroupBlock{
			Detail:   run.Detail,
			Hint:     run.Group.Hint,
			Status:   uiToolStatus(run.Status),
			Expanded: autoExp,
			Theme:    m.theme,
			Spinner:  m.spinner,
			// Resolves against the mapper's latest snapshot on every draw, so a
			// member arriving after the row was built still shows up. The item
			// id is captured, not the run itself: capturing the run would freeze
			// the group's members at creation time.
			Members: func() []block.ChildTool {
				if run.Group == nil {
					return nil
				}
				return m.groupMembersFrom(it.ID)
			},
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
	}
	if isReadTool(run.Name) {
		path, detail := readRowTarget(run)
		return &block.ReadBlock{
			Name:     run.Name,
			Path:     path,
			Detail:   detail,
			Body:     stripReadHeader(run.Output),
			Error:    run.Error,
			Status:   uiToolStatus(run.Status),
			Expanded: autoExp,
			Theme:    m.theme,
			Spinner:  m.spinner,
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
	}
	if isEditTool(run.Name) {
		path, diff, errText := m.editToolFields(it)
		return &block.DiffBlock{
			Path:     path,
			Diff:     diff,
			Error:    errText,
			Status:   uiToolStatus(run.Status),
			Expanded: autoExp,
			Theme:    m.theme,
			Spinner:  m.spinner,
			OnToggle: func(expanded bool) {
				m.expanded[id] = expanded
				if m.onInvalidate != nil {
					m.onInvalidate()
				}
			},
		}
	}
	return &block.ToolBlock{
		Name:     run.Name,
		Detail:   run.Detail,
		Summary:  run.Summary,
		Output:   run.Output,
		Error:    run.Error,
		Status:   uiToolStatus(run.Status),
		Expanded: autoExp,
		Theme:    m.theme,
		Spinner:  m.spinner,
		OnToggle: func(expanded bool) {
			m.expanded[id] = expanded
			if m.onInvalidate != nil {
				m.onInvalidate()
			}
		},
	}
}

func isAgentTreeTool(name string) bool {
	switch strings.ToLower(name) {
	case "agent_spawn", "agent_wait":
		return true
	default:
		return false
	}
}

// isEditTool reports whether name is a file-editing tool whose output is a
// unified diff. These rows render as a DiffBlock so the change is visible
// without expanding, the way Claude Code shows edits.
func isEditTool(name string) bool {
	switch strings.ToLower(name) {
	case "write", "edit":
		return true
	default:
		return false
	}
}

// isReadTool reports whether name is a file-reading tool whose output is
// numbered lines. These rows render as a ReadBlock, the same card shape as
// DiffBlock minus the +/- gutter.
func isReadTool(name string) bool {
	switch strings.ToLower(name) {
	case "read", "grep":
		return true
	default:
		return false
	}
}

// stripLeadingVerb removes a leading tool name from an arg-derived phrase.
func stripLeadingVerb(detail, name string) string {
	detail = strings.TrimSpace(detail)
	if name == "" || detail == "" {
		return detail
	}
	lower := strings.ToLower(detail)
	prefix := strings.ToLower(name)
	if !strings.HasPrefix(lower, prefix) {
		return detail
	}
	rest := strings.TrimSpace(detail[len(prefix):])
	if rest == "" {
		return detail
	}
	// Only strip a whole-word match, so "grep" does not eat "grepfile".
	if isWordBoundary(rest[0]) {
		return rest
	}
	return detail
}

// isWordBoundary reports whether r starts a new word rather than continuing
// the one before it.
func isWordBoundary(r byte) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	default:
		return true
	}
}

// isReadGroupTool identifies the collapsed run of read-only calls.
func isReadGroupTool(name string) bool {
	return strings.EqualFold(name, "read_group")
}

// readRowTarget splits a read/grep run into the path shown by CopyText and the
// arg-derived phrase that identifies the search. DetailFromArgs gives grep its
// pattern phrase; once the run finishes it leaves Detail untouched (the match
// count lives in the row stat), so the phrase survives to the transcript.
func readRowTarget(run session.ToolRun) (path, detail string) {
	detail = strings.TrimSpace(run.Detail)
	detail = strings.TrimPrefix(detail, "@file ")
	// A bare path is both the path and the target.
	if !strings.ContainsAny(detail, " \"") {
		return detail, ""
	}
	return "", detail
}

// stripReadHeader drops the leading "@file path" line a read row carries. The
// path is already the card header, so repeating it costs a row on a narrow
// terminal. Other leading prose (grep's notices) is kept.
func stripReadHeader(body string) string {
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "@file ") {
			rest := append(append([]string(nil), lines[:i]...), lines[i+1:]...)
			return strings.TrimLeft(strings.Join(rest, "\n"), "\n")
		}
		return body
	}
	return body
}

func (m *Mapper) fillAgentBlock(a *block.AgentBlock, it session.Item) {
	run := it.ToolRun
	a.Name = run.Name
	a.Detail = run.Detail
	a.Status = uiToolStatus(run.Status)
	a.Theme = m.theme
	a.Spinner = m.spinner
	a.Error = run.Error

	parsed := tools.ParseAgentResult(run.Output)
	if sum := parsed.RenderableSummary(); sum != "" {
		a.Summary = sum
	} else {
		a.Summary = ""
	}

	// agent_wait: summary only — the live tree already lives on agent_spawn.
	// agent_spawn: nested child tools from SubagentStore.
	a.Children = nil
	a.Activity = ""
	if !strings.EqualFold(run.Name, "agent_wait") && m.Children != nil {
		a.Children = m.Children(run.ToolUseID)
		if len(a.Children) == 0 && parsed.JobID != "" && m.ChildrenByJob != nil {
			a.Children = m.ChildrenByJob(parsed.JobID)
		}
	}
	// The header names the child currently in flight. Children arrive in
	// completion order, so the last non-terminal one is the live one; when all
	// have finished the last child is still the most recent thing it did.
	a.Activity = block.LastRunningChild(a.Children)

	if exp, ok := m.expanded[it.ID]; ok {
		a.Expanded = exp
	} else if a.Status == status.ToolRunning || len(a.Children) > 0 || a.Summary != "" {
		a.Expanded = true
	}
}

// groupMembersFrom resolves a collapsed group's member rows from the live
// snapshot. It re-projects the item so it works from the row's id alone, which
// is all the widget closure carries.
func (m *Mapper) groupMembersFrom(itemID string) []block.ChildTool {
	group := m.findGroup(itemID)
	if group == nil || len(group.Members) == 0 {
		return nil
	}
	out := make([]block.ChildTool, 0, len(group.Members))
	for _, id := range group.Members {
		member := m.memberRun(id)
		if member.Name == "" {
			continue
		}
		out = append(out, block.ChildTool{
			Name:    member.Name,
			Detail:  stripLeadingVerb(member.Detail, member.Name),
			Summary: member.Summary,
			Status:  uiToolStatus(member.Status),
		})
	}
	return out
}

// findGroup locates the group for a row id in the current snapshot. A missing
// group returns nil so a stale widget renders as an empty row rather than
// panicking.
func (m *Mapper) findGroup(itemID string) *session.ToolGroup {
	for _, it := range session.Project(m.snap) {
		if it.ID == itemID && it.ToolRun.Group != nil {
			return it.ToolRun.Group
		}
	}
	return nil
}

// memberRun finds one member's ToolRun. It is passed the group's own item so a
// caller without the snapshot can still resolve live state through the mapper's
// cached snapshot; the session map is the authority.
func (m *Mapper) memberRun(id string) session.ToolRun {
	run, ok := m.snap.Tools[id]
	if !ok {
		return session.ToolRun{}
	}
	return run
}

// editToolFields extracts the display path and unified diff for a file-editing
// tool row. The write/edit tools already embed the diff in their output, so
// nothing has to be recomputed here — the row only needs to drop the duplicated
// file header and pick the right source when the run failed.
func (*Mapper) editToolFields(it session.Item) (path, diff, errText string) {
	run := it.ToolRun
	path = run.Detail
	errText = run.Error
	if run.Error != "" && strings.TrimSpace(run.Output) == "" {
		// A failed edit reports no diff; the error text carries the reason.
		return path, "", errText
	}
	return path, stripDiffFileHeaders(run.Output), errText
}

// stripDiffFileHeaders removes the ---/+++ header pair from a tool's output.
// DiffBlock already shows the path, so repeating a/ and b/ prefixes wastes two
// lines of a narrow terminal.
func stripDiffFileHeaders(diff string) string {
	lines := strings.Split(diff, "\n")
	out := lines[:0]
	for _, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "--- a/") || strings.HasPrefix(trimmed, "+++ b/") {
			continue
		}
		out = append(out, ln)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func uiToolStatus(s session.ToolStatus) status.ToolStatus {
	switch s {
	case session.ToolDone:
		return status.ToolDone
	case session.ToolError:
		return status.ToolError
	case session.ToolCancelled:
		return status.ToolCancelled
	case session.ToolRejected:
		return status.ToolRejected
	case session.ToolQueued:
		return status.ToolQueued
	default:
		return status.ToolRunning
	}
}

// checklistWidget returns the checklist row, or nil when there is nothing to
// show. The file is re-read only when its stat result changed.
func (m *Mapper) checklistWidget() *block.ChecklistBlock {
	if m.Checklist == nil || m.Checklist.Path == "" {
		return nil
	}
	m.Checklist.refresh()
	if !m.Checklist.loaded {
		return nil
	}
	exp, touched := m.expanded[ChecklistSourceEntryID]
	if !touched {
		exp = m.AutoExpand
	}
	return &block.ChecklistBlock{
		Path:     m.Checklist.Path,
		Items:    m.Checklist.items,
		Expanded: exp,
		StaleFor: m.Checklist.StaleFor,
		Theme:    m.theme,
		OnToggle: func(expanded bool) {
			m.expanded[ChecklistSourceEntryID] = expanded
			if m.onInvalidate != nil {
				m.onInvalidate()
			}
		},
	}
}

// NewChecklistSource builds a source for a task file that may not exist yet.
// The first successful stat wins, so creating the file mid-session starts the
// row without a restart.
func NewChecklistSource(path string) *ChecklistSource {
	return &ChecklistSource{Path: path}
}

// refresh stats the file and re-parses only when it changed.
//
// mtime+size is deliberately coarse: an agent that rewrites identical content
// with the same size inside the mtime tick is indistinguishable, and re-reading
// every frame to catch that would burn IO for no visible gain.
func (c *ChecklistSource) refresh() {
	info, err := os.Stat(c.Path)
	if err != nil {
		// Absent is the normal state before the agent writes anything, and it
		// hides the row rather than showing an empty one.
		c.loaded = false
		c.items = nil
		return
	}
	if c.loaded && info.ModTime().Equal(c.lastMod) && info.Size() == c.lastSize {
		return
	}
	f, err := todo.Parse(c.Path)
	if err != nil {
		return
	}
	moved := !c.loaded || !equalItems(c.items, f.Items)
	c.items = f.Items
	c.lastMod = f.ModTime
	c.lastSize = f.Size
	c.loaded = true
	if moved {
		c.changedAt = time.Now()
		c.StaleFor = 0
	}
}

// countEditTools counts succeeded file-editing rows, which is the staleness
// signal for the checklist: a list the agent stopped updating while the code
// kept changing is lying, and the row says so instead of pretending.
func countEditTools(items []session.Item) int {
	n := 0
	for _, it := range items {
		if it.Kind != session.ItemTool {
			continue
		}
		if !isEditTool(it.ToolRun.Name) {
			continue
		}
		if it.ToolRun.Status == session.ToolDone || it.ToolRun.Status == session.ToolRejected {
			n++
		}
	}
	return n
}

// checklistEqual reports whether two checklist rows draw the same thing. Index
// 0 is assumed to be the checklist row when byID says it is present.
func checklistEqual(w components.Widget, cl *block.ChecklistBlock) bool {
	prev, ok := w.(*block.ChecklistBlock)
	if !ok {
		return false
	}
	if prev.Path != cl.Path || prev.Expanded != cl.Expanded || prev.StaleFor != cl.StaleFor {
		return false
	}
	if len(prev.Items) != len(cl.Items) {
		return false
	}
	for i := range prev.Items {
		if prev.Items[i] != cl.Items[i] {
			return false
		}
	}
	return true
}

func equalItems(a, b []todo.Item) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
