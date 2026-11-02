// Package todo parses a markdown checklist out of a task file for the TUI.
//
// This is deliberately not a todo model: the file the agent writes is the source
// of truth, and this package only reads it. Anthropic's TaskListV2 renders a
// structured todo list; a1 renders checkboxes the agent already produces
// naturally, which means no schema, no persistence, and no tool for the model
// to forget to call.
package todo

import (
	"os"
	"strings"
	"time"
)

// Item is one markdown list line.
type Item struct {
	// Indent is the leading-whitespace width in columns, so nesting survives
	// both spaces and tabs.
	Indent int
	// Text is the item's wording with the bullet and box removed.
	Text string
	// Done reports whether the box is ticked.
	Done bool
	// InProgress is set when the box is "- [~]" or "- [>]"; exactly one item
	// should carry it, and it renders differently from a plain open item.
	InProgress bool
	// Detail marks an unbulleted-without-box line — a sub-point under the
	// preceding boxed item. Details never contribute to the counts, because
	// nothing has been claimed about their state.
	Detail bool
}

// IsTask reports whether the item carries a box, so belongs in the counts.
func (it Item) IsTask() bool { return !it.Detail }

// File is a parsed checklist plus the provenance the renderer needs to avoid
// re-reading the file on every frame.
type File struct {
	// Path is the file the checklist came from.
	Path  string
	Items []Item
	// ModTime is the source file's mtime at parse time.
	ModTime time.Time
	// Size is the source file's size at parse time.
	Size int64
}

// Done counts ticked tasks.
func (f File) Done() int {
	n := 0
	for _, it := range f.Items {
		if it.Done {
			n++
		}
	}
	return n
}

// Open counts tasks that are neither ticked nor in progress.
func (f File) Open() int {
	n := 0
	for _, it := range f.Items {
		if it.IsTask() && !it.Done && !it.InProgress {
			n++
		}
	}
	return n
}

// Tasks counts every boxed item, done or not.
func (f File) Tasks() int {
	n := 0
	for _, it := range f.Items {
		if it.IsTask() {
			n++
		}
	}
	return n
}

// InProgress returns the item marked in progress, or nil.
func (f File) InProgress() *Item {
	for i := range f.Items {
		if f.Items[i].InProgress {
			return &f.Items[i]
		}
	}
	return nil
}

// Parse reads a checklist file. A missing or unreadable file returns the error
// from the filesystem so the caller can tell "absent" from "empty" — an
// uncreated TODO.md is the normal first-run state, not a failure.
func Parse(path string) (File, error) {
	f := File{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return f, err
	}
	// A directory stats fine, so it has to be rejected explicitly rather than
	// falling through to a ReadFile that fails differently (or not at all).
	if info.IsDir() {
		return f, os.ErrInvalid
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	f.ModTime = info.ModTime()
	f.Size = info.Size()
	f.Items = ParseText(string(raw))
	return f, nil
}

// ParseText extracts list items from markdown text.
//
// Recognized boxes: "- [ ]" open, "- [x]" / "- [X]" done, "- [~]" / "- [>]" in
// progress. A bullet without a box ("- some detail") is kept as a Detail line
// under whichever boxed item precedes it, so nested notes survive without being
// miscounted as tasks.
//
// Anything else — prose, headings, quotes, tables, ordered lists, code-fence
// contents — is ignored. A code fence is tracked, because agents do write
// "- [ ]" samples inside examples and counting those would inflate the total.
func ParseText(text string) []Item {
	var items []Item
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		if f, ok := codeFence(line, fence); ok {
			// The marker line is never an item; tracking the open marker keeps
			// the wrong kind of fence from closing the block.
			fence = f
			continue
		}
		if fence != "" {
			continue
		}
		it, ok := parseLine(line)
		if ok {
			items = append(items, it)
		}
	}
	return items
}

// codeFence tracks whether a line is a code-fence marker and which one. The
// open marker is returned so a ~~~ block cannot be closed by a ``` line, which
// is how agents actually nest fences in examples. A closing marker returns "".
func codeFence(line, open string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if open == "" {
		for _, mark := range []string{"```", "~~~"} {
			if strings.HasPrefix(trimmed, mark) {
				return mark, true
			}
		}
		return "", false
	}
	if strings.HasPrefix(trimmed, open) {
		return "", true
	}
	return "", false
}

func parseLine(line string) (Item, bool) {
	// The byte cursor and the column count are separate: a tab is one byte but
	// four columns, and using one counter for both skips past the bullet.
	i, cols := 0, 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == '\t' {
			cols += 4
		} else {
			cols++
		}
		i++
	}
	rest := line[i:]

	bullet, ok := bulletPrefix(rest)
	if !ok {
		return Item{}, false
	}
	body := rest[len(bullet):]

	// No box: an indented detail line attaches to the boxed item above it. A
	// top-level bullet without a box is skipped instead — it has no parent to
	// attach to and nothing has been claimed about its state.
	if !hasBox(body) {
		if cols == 0 {
			return Item{}, false
		}
		text := strings.TrimSpace(body)
		return Item{Indent: cols, Text: text, Detail: true}, text != ""
	}
	mark := body[1]
	it := Item{Indent: cols, Text: strings.TrimSpace(body[3:])}
	switch mark {
	case 'x', 'X':
		it.Done = true
	case '~', '>':
		it.InProgress = true
	case ' ':
		// An open box.
	default:
		// Not a checkbox (e.g. "[1]" leftover from an ordered list).
		return Item{}, false
	}
	if it.Text == "" {
		return Item{}, false
	}
	return it, true
}

func hasBox(body string) bool {
	return len(body) >= 3 && body[0] == '[' && body[2] == ']'
}

func bulletPrefix(rest string) (string, bool) {
	for _, b := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(rest, b) {
			return b, true
		}
	}
	return "", false
}
