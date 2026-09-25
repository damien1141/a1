// Package codepane renders a full-screen source-file pane: syntax-highlighted
// reading with a caret, a line-wise selection, and one handoff back to the chat
// composer.
package codepane

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pulseaiclub/xui"

	"github.com/damien1141/a1/internal/components"
	"github.com/damien1141/a1/internal/components/chat"
	"github.com/damien1141/a1/internal/components/chrome"
	"github.com/damien1141/a1/internal/components/codeview"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/util"
)

const (
	// maxFileBytes keeps a stray multi-gigabyte log from freezing the reader.
	maxFileBytes = 8 << 20
	// binarySniffSize: a NUL byte in the head is the cheapest binary tell.
	binarySniffSize = 8 << 10
	// selLineWide marks the moving end of a line-wise selection. It only has to
	// exceed any real line, so the tint is clipped at the frame edge instead of
	// stopping mid-line.
	selLineWide = 1 << 20
)

// sensitivePaths is a snapshot of the gate's deny list. The viewer reads files
// itself, so it checks the same list rather than becoming the one hole in it.
var sensitivePaths = permission.SensitivePaths()

// Pane is a full-screen source-file overlay. Reading, key handling and painting
// all happen on the UI goroutine, so the state needs no lock.
type Pane struct {
	cwd     string
	onRef   func(chat.Ref)
	onToast func(string)

	theme  components.Theme
	method xui.WidthMethod

	active  bool
	abs     string
	rel     string
	lines   []string
	hl      map[int][]components.Span
	loadErr string

	line    int // 0-based cursor line
	col     int // byte offset into lines[line], always on a rune boundary
	scroll  int
	xScroll int
	viewW   int
	viewH   int

	pendingG bool

	// selecting is a line-wise visual selection anchored at selAnchor; the caret
	// (line) is the moving end, so plain movement extends it.
	selecting bool
	selAnchor int
}

// New builds an inactive pane. onRef receives the line the user picked for the
// chat input; onToast reports the outcome of an action.
func New(theme components.Theme, cwd string, onRef func(chat.Ref), onToast func(string)) *Pane {
	return &Pane{
		cwd:     cwd,
		onRef:   onRef,
		onToast: onToast,
		theme:   theme,
		method:  xui.WidthUnicode,
	}
}

// Active reports whether the overlay is showing.
func (p *Pane) Active() bool {
	if p == nil {
		return false
	}
	return p.active
}

// SetTheme updates chrome and syntax colors.
func (p *Pane) SetTheme(th components.Theme) {
	if p == nil {
		return
	}
	p.theme = th
	p.hl = codeview.Highlight(p.abs, p.lines, th)
}

// Open shows a file from the top. Relative paths resolve against the pane cwd.
func (p *Pane) Open(path string) {
	p.OpenAt(path, 0)
}

// OpenAt shows path with the cursor on line (1-based; 0 leaves it at the top).
func (p *Pane) OpenAt(path string, line int) {
	if p == nil {
		return
	}
	p.active = true
	p.pendingG = false
	p.selecting = false

	if err := p.load(path, line); err != nil {
		p.lines = nil
		p.hl = nil
		p.abs, p.rel = "", ""
		p.loadErr = loadMessage(p.cwd, err)
		p.line, p.col, p.scroll, p.xScroll = 0, 0, 0, 0
	}
}

// Close hides the overlay.
func (p *Pane) Close() {
	if p == nil {
		return
	}
	p.active = false
	p.pendingG = false
}

// Handle consumes keyboard input while the overlay is active.
func (p *Pane) Handle(ctx *components.EventContext, ev xui.Event) {
	if p == nil || !p.Active() {
		return
	}
	switch e := ev.(type) {
	case xui.KeyEvent:
		if !e.Press {
			return
		}
		p.notify(p.handleKey(ctx, e))
	default:
		ctx.Consume = true
	}
}

// handleKey runs one key against pane state. It returns a toast to raise, which
// the caller delivers once the key is done with.
func (p *Pane) handleKey(ctx *components.EventContext, e xui.KeyEvent) string {
	if e.Code == xui.KeyEscape && p.selecting {
		// Cancel the selection first: losing a ten-line pick to a close that was
		// meant to undo it is worse than one extra Esc.
		p.selecting = false
		ctx.ConsumeAndRedraw()
		return ""
	}
	if e.Code == xui.KeyEscape ||
		(e.Code == xui.KeyRune && (e.Rune == 'q' || e.Rune == 'Q') && !e.Mods.Has(xui.ModCtrl)) {
		p.active = false
		p.selecting = false // a reopened pane starts clean
		p.pendingG = false
		ctx.ConsumeAndRedraw()
		return ""
	}
	if p.handlePending(ctx, e) {
		return ""
	}

	switch e.Code {
	case xui.KeyUp:
		p.moveLine(-1)
	case xui.KeyDown:
		p.moveLine(1)
	case xui.KeyLeft:
		p.moveCol(-1)
	case xui.KeyRight:
		p.moveCol(1)
	case xui.KeyPageUp:
		p.moveLine(-p.page())
	case xui.KeyPageDown:
		p.moveLine(p.page())
	case xui.KeyHome:
		p.col = 0
	case xui.KeyEnd:
		p.col = len(p.lineText())
	case xui.KeyRune:
		if e.Mods.Has(xui.ModCtrl) {
			switch e.Rune {
			case 'd', 'D':
				p.moveLine(p.page())
			case 'u', 'U':
				p.moveLine(-p.page())
			}
			ctx.ConsumeAndRedraw()
			return ""
		}
		return p.handleRune(ctx, e.Rune)
	default:
		ctx.Consume = true
		return ""
	}
	p.clamp()
	p.reveal()
	ctx.ConsumeAndRedraw()
	return ""
}

func (p *Pane) handlePending(ctx *components.EventContext, e xui.KeyEvent) bool {
	if !p.pendingG {
		return false
	}
	p.pendingG = false
	if e.Code != xui.KeyRune || e.Rune != 'g' {
		return false
	}
	p.line, p.col, p.scroll, p.xScroll = 0, 0, 0, 0
	ctx.ConsumeAndRedraw()
	return true
}

func (p *Pane) handleRune(ctx *components.EventContext, r rune) string {
	switch r {
	case 'j':
		p.moveLine(1)
	case 'k':
		p.moveLine(-1)
	case 'h':
		p.moveCol(-1)
	case 'l':
		p.moveCol(1)
	case 'g':
		p.pendingG = true
		ctx.ConsumeAndRedraw()
		return ""
	case 'G':
		p.line = max(len(p.lines)-1, 0)
	case '0':
		p.col = 0
	case '$':
		p.col = len(p.lineText())
	case 'v':
		p.toggleSelect()
	case 'a':
		return p.addRef()
	default:
		ctx.Consume = true
		return ""
	}
	p.clamp()
	p.reveal()
	ctx.ConsumeAndRedraw()
	return ""
}

func (p *Pane) notify(msg string) {
	if msg != "" && p.onToast != nil {
		p.onToast(msg)
	}
}

// load reads path and moves the cursor to line (1-based, 0 = top).
func (p *Pane) load(path string, line int) error {
	abs := p.resolve(path)
	lines, err := readFileLines(abs)
	if err != nil {
		return err
	}

	p.abs = abs
	p.rel = relPath(p.cwd, abs)
	p.lines = lines
	p.hl = codeview.Highlight(abs, lines, p.theme)
	p.loadErr = ""
	p.selecting = false
	if !p.active {
		return nil
	}
	p.line = clampLine(line-1, len(lines))
	// Land the caret on the code, not in the indentation: an open at :line
	// should show the line, not its leading whitespace.
	p.col = firstNonBlank(p.lineText())
	p.scroll = 0
	p.xScroll = 0
	p.clamp()
	return nil
}

func (p *Pane) resolve(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(p.cwd, path))
}

// readFileLines reads a text file into display lines. Sensitive paths, binary
// files and oversized files are refused rather than half-rendered.
func readFileLines(abs string) ([]string, error) {
	if permission.IsSensitivePath(abs, sensitivePaths) {
		return nil, fmt.Errorf("%s is a sensitive path", filepath.Base(abs))
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fmt.Errorf("%s is a directory", abs)
	}
	if st.Size() > maxFileBytes {
		return nil, fmt.Errorf("file is %s; the viewer caps at %s", humanBytes(st.Size()), humanBytes(maxFileBytes))
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	head := raw
	if len(head) > binarySniffSize {
		head = head[:binarySniffSize]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, fmt.Errorf("%s looks binary", filepath.Base(abs))
	}
	lines := strings.Split(util.NormalizeLF(string(raw)), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines, nil
}

// loadMessage renders a load failure the way the status row wants it: the path
// relative to cwd, not an absolute path the status row would truncate before
// the reason becomes legible. The reason itself is mapped to short, OS-neutral
// text — Windows syscall strings are long and hard to scan.
func loadMessage(cwd string, err error) string {
	if pe, ok := errors.AsType[*os.PathError](err); ok {
		return fmt.Sprintf("%s: %s", relPath(cwd, pe.Path), statReason(pe.Err))
	}
	return err.Error()
}

func statReason(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no such file"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	default:
		return err.Error()
	}
}

// relPath renders abs for the UI with forward slashes: titles, status rows and
// pasted references are shown on every platform, and "src\\a.go:2" is worthless
// in a terminal or editor on the other OS.
func relPath(cwd, abs string) string {
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (p *Pane) lineText() string {
	if p.line < 0 || p.line >= len(p.lines) {
		return ""
	}
	return p.lines[p.line]
}

func (p *Pane) moveLine(delta int) {
	p.line += delta
	p.clamp()
	p.col = clampCol(p.lineText(), p.col)
	p.reveal()
}

func (p *Pane) moveCol(delta int) {
	s := p.lineText()
	if delta < 0 {
		if p.col > 0 {
			_, size := utf8.DecodeLastRuneInString(s[:p.col])
			p.col -= size
		}
	} else if p.col < len(s) {
		_, size := utf8.DecodeRuneInString(s[p.col:])
		p.col += size
	}
	p.reveal()
}

func (p *Pane) page() int {
	h := p.viewH / 2
	if h < 1 {
		h = 8
	}
	return h
}

func (p *Pane) clamp() {
	p.line = clampLine(p.line, len(p.lines))
	p.col = clampCol(p.lineText(), p.col)
}

// reveal scrolls so the cursor line and column are inside the viewport.
func (p *Pane) reveal() {
	if p.viewH > 0 {
		if p.line < p.scroll {
			p.scroll = p.line
		}
		if bottom := p.scroll + p.viewH - 1; p.line > bottom {
			p.scroll = p.line - p.viewH + 1
		}
	}
	if p.viewW > 0 {
		col := displayCol(p.lineText(), p.col, p.method)
		codeW := max(p.viewW-gutterWidth(len(p.lines)), 1)
		if col < p.xScroll {
			p.xScroll = col
		}
		if col >= p.xScroll+codeW {
			p.xScroll = col - codeW + 1
		}
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	if p.xScroll < 0 {
		p.xScroll = 0
	}
}

func clampLine(line, total int) int {
	if total <= 0 {
		return 0
	}
	return min(max(line, 0), total-1)
}

// firstNonBlank is the column an open at :line lands on: the first character
// that is not indentation, so the caret sits on the code rather than in
// whitespace.
func firstNonBlank(s string) int {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != ' ' && r != '\t' {
			break
		}
		i += size
	}
	return i
}

func clampCol(s string, col int) int {
	if col <= 0 {
		return 0
	}
	if col > len(s) {
		return len(s)
	}
	for col < len(s) && !utf8.RuneStart(s[col]) {
		col--
	}
	return col
}

// displayCol is the screen column of a byte offset, counting wide glyphs.
func displayCol(s string, col int, method xui.WidthMethod) int {
	if col <= 0 {
		return 0
	}
	if col > len(s) {
		col = len(s)
	}
	return xui.StringWidth(s[:clampCol(s, col)], method)
}

func gutterWidth(lines int) int {
	return len(strconv.Itoa(max(lines, 1))) + 3
}

// toggleSelect starts or drops a line-wise selection. The caret is the moving
// end, so the ordinary movement keys extend it without any extra state.
func (p *Pane) toggleSelect() {
	p.selecting = !p.selecting
	p.selAnchor = p.line
}

// selectionLines returns the inclusive, ordered line span of the selection.
func (p *Pane) selectionLines() (lo, hi int) {
	lo, hi = p.selAnchor, p.line
	if lo > hi {
		lo, hi = hi, lo
	}
	return clampLine(lo, len(p.lines)), clampLine(hi, len(p.lines))
}

// addRef hands the selected lines (or the caret line alone) to the chat input
// and reports the outcome as a toast.
func (p *Pane) addRef() string {
	ref, ok := p.selectedRef()
	if !ok {
		return "no file open"
	}
	if p.onRef == nil {
		return "chat input is unavailable"
	}
	p.selecting = false
	p.onRef(ref)
	return fmt.Sprintf("added %s to chat", ref.Label())
}

// selectedRef cuts the current selection out of the open file.
func (p *Pane) selectedRef() (chat.Ref, bool) {
	if p.abs == "" || len(p.lines) == 0 {
		return chat.Ref{}, false
	}
	lo, hi := p.line, p.line
	if p.selecting {
		lo, hi = p.selectionLines()
	}
	return chat.Ref{
		Path:  p.rel,
		Start: lo + 1,
		End:   hi + 1,
		Lang:  fenceLang(p.abs),
		Text:  strings.Join(p.lines[lo:hi+1], "\n"),
	}, true
}

// fenceLang is the markdown fence hint for a path: "main.go" -> "go".
func fenceLang(path string) string {
	return strings.TrimPrefix(filepath.Ext(path), ".")
}

// Draw paints the overlay.
func (p *Pane) Draw(ctx components.DrawContext) components.Surface {
	if p == nil {
		return components.NewSurface(ctx.Max.Width, ctx.Max.Height, nil)
	}
	p.method = ctx.Method
	p.viewW = ctx.Max.Width
	p.viewH = max(ctx.Max.Height-2, 1)
	p.reveal()
	return codeview.Paint(ctx, p.snapshot(ctx))
}

func (p *Pane) snapshot(ctx components.DrawContext) codeview.Model {
	status := p.location()
	switch {
	case p.loadErr != "":
		status = p.loadErr
	case p.selecting:
		lo, hi := p.selectionLines()
		status = selectionStatus(hi - lo + 1)
	}
	m := codeview.Model{
		Theme:      p.theme,
		Title:      "code" + chrome.Sep + p.title(),
		Status:     status,
		Hint:       hintLine,
		Path:       p.abs,
		Lines:      p.lines,
		Highlight:  p.hl,
		CursorLine: p.line,
		CursorCol:  displayCol(p.lineText(), p.col, ctx.Method),
		Scroll:     p.scroll,
		XScroll:    p.xScroll,
		ViewH:      p.viewH,
		Empty:      p.emptyText(),
	}
	if p.selecting {
		// Line-wise: the moving end is pinned past any real line so the
		// tint is clipped at the frame edge rather than stopping mid-line.
		lo, hi := p.selectionLines()
		m.Selecting = true
		m.SelStart = components.Point{X: 0, Y: lo}
		m.SelEnd = components.Point{X: selLineWide, Y: hi}
	}
	return m
}

// selectionStatus is the status row while v is active.
func selectionStatus(n int) string {
	unit := "lines"
	if n == 1 {
		unit = "line"
	}
	return fmt.Sprintf("%d %s selected%s a to add", n, unit, chrome.Sep)
}

func (p *Pane) title() string {
	if p.rel == "" {
		return "no file"
	}
	return p.rel
}

// location is the resting status text: path, caret, line count.
func (p *Pane) location() string {
	if p.abs == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d%s%d lines", p.rel, p.line+1, p.col+1, chrome.Sep, len(p.lines))
}

func (p *Pane) emptyText() string {
	if p.loadErr != "" {
		return p.loadErr
	}
	if p.abs == "" {
		return "usage: /code <path>"
	}
	if len(p.lines) == 0 {
		return "empty file"
	}
	return ""
}

// hintLine is advertised at the bottom of the pane.
var hintLine = strings.Join([]string{
	"esc close",
	"j/k move",
	"h/l ←/→",
	"gg/G top/bottom",
	"v select",
	"a add",
}, chrome.Sep)
