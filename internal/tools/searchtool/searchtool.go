// Package searchtool implements the combined `search` tool — one call that
// fans out to three search planes (text via ripgrep, symbol via the import
// graph, file via fd) and returns ranked, grouped results. The aim is to
// collapse the model's grep → read → grep loop into a single call.
package searchtool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/graph"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools/findtool"
	"github.com/damien1141/a1/internal/tools/graphtool"
	"github.com/damien1141/a1/internal/tools/greptool"
	"github.com/damien1141/a1/internal/tools/ranktool"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/util/filesearch"
)

const (
	searchDefaultLimit  = 30 // files (not matches)
	searchDefaultPath   = "."
	textMatchCapPerFile = 5 // top matches shown per file in the text plane
	// searchDefaultListLimit caps the empty-query directory listing mode.
	// A listing wants more entries than a search wants files — it is the
	// "what's in here" glance, not a ranked result set.
	searchDefaultListLimit = 50
	// textSnippetCap bounds each rendered text/context line so one huge
	// minified line cannot blow up the result.
	textSnippetCap = 200
)

var searchDescription = `Search the workspace over three planes at once: text (ripgrep), import
graph (symbols), and file paths (fd). One ranked per-file list back —
files hit by more planes first, name matches boosted, up to 5 matching
lines each (context=N adds surrounding lines, marked with ·). Replaces
grep-then-read loops when finding where things live.

auto (default) always runs text, adds symbol for identifier-like
queries and file for path-like ones. text|symbol|file force one plane.
grep|find|graph|rank delegate to those tools with their own params
(grep/find take pattern, not query).

Empty query + path lists that directory (entries with size + mtime).
glob filters every plane, e.g. "*.go". literal:true treats the query
as plain text instead of a regex. pattern is accepted as an alias for
query.`

// SearchTool returns the combined search tool definition + handler.
func SearchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "search",
			Description: searchDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Regex, literal text, identifier, or path fragment. Omit (with path set) to list a directory.",
					},
					"pattern": llm.Object{
						"type":        "string",
						"description": "Alias for query (grep/find habit).",
					},
					"mode": llm.Object{
						"type":        "string",
						"description": "auto (default) | text | symbol | file | grep | find | graph | rank.",
						"enum":        []string{"auto", "text", "symbol", "file", "grep", "find", "graph", "rank"},
					},
					"path": llm.Object{
						"type":        "string",
						"description": "Directory to search or list.",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Max files to return. Default %d.", searchDefaultLimit),
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File filter for every plane, e.g. \"*.go\". The text plane honors full ripgrep glob syntax (including ** and !negation).",
					},
					"literal": llm.Object{
						"type":        "boolean",
						"description": "Treat the query as plain text, not a regex (text plane).",
					},
					"context": llm.Object{
						"type":        "integer",
						"description": "Lines of context around each text match, marked with ·. Example: 2",
					},
					"ignoreCase": llm.Object{
						"type":        "boolean",
						"description": "Case-insensitive; text plane only.",
					},
					"rescan": llm.Object{
						"type":        "boolean",
						"description": "Force a full symbol-plane graph rebuild (skips the mtime cache).",
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in searchInput
			_ = json.Unmarshal(input, &in)
			q := strings.TrimSpace(in.Query)
			if q == "" {
				q = strings.TrimSpace(in.Pattern)
			}
			if q != "" {
				if len(q) > 40 {
					q = q[:40] + "..."
				}
				return fmt.Sprintf("search %q", q)
			}
			if p := strings.TrimSpace(in.Path); p != "" {
				return "list " + p
			}
			return ""
		},
		Run: runSearch,
	}
}

type searchInput struct {
	Query      string `json:"query"`
	Pattern    string `json:"pattern,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Path       string `json:"path,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Glob       string `json:"glob,omitempty"`
	Literal    bool   `json:"literal,omitempty"`
	Context    int    `json:"context,omitempty"`
	IgnoreCase bool   `json:"ignoreCase,omitempty"`
	Rescan     bool   `json:"rescan,omitempty"`
}

// planeOpts carries the text-plane knobs (and the glob that also gates the
// symbol and file planes) so runSearch's goroutine closures stay narrow.
type planeOpts struct {
	ignoreCase bool
	literal    bool
	context    int
	glob       string
}

// fileHit aggregates everything the three planes returned for one file.
type fileHit struct {
	rel       string // cwd-relative path, used as the map key
	abs       string // absolute path, for stat + line-reading if needed
	textLines []textMatch
	// display is the ordered match+context view the renderer prints:
	// match lines carry isMatch=true, context lines false. With
	// context=0 it holds the matches alone.
	display []displayLine
	planes  map[string]struct{}
	// graphHit records symbol-plane info: was this file found because it
	// imports the query, is imported by it, or both? Stored as a short
	// label for the renderer.
	graphHit string
	// size/mtime are filled by rankAndTrim's stat pass (top hits only)
	// so file-plane-only results can show more than a bare path.
	size   int64
	mtime  time.Time
	statOK bool
}

type textMatch struct {
	lineNumber int
	text       string
}

type displayLine struct {
	lineNumber int
	text       string
	isMatch    bool
}

func runSearch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in searchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse search arguments: %w", err)
	}

	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "auto", "text", "symbol", "file":
		// Continue below — these modes use the combined renderer and
		// require a `query` field (or a path for the listing mode).
	case "grep", "find", "graph", "rank":
		// Single-plane escape hatches — delegate to the underlying tool
		// immediately. These tools have their own required-field
		// validation (find needs `pattern`, not `query`; rank doesn't
		// need a query at all) so we MUST skip the combined-mode
		// `query is required` check below and let the underlying tool
		// validate its own input.
		h, ok := singlePlaneHandlers[mode]
		if !ok {
			return tooldef.Result{}, fmt.Errorf("search mode %q is not wired up", mode)
		}
		return h(ctx, input)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown mode %q: use auto, text, symbol, file, grep, find, graph, or rank", mode)
	}

	// Combined-mode query validation: only auto/text/symbol/file reach
	// here. `pattern` is accepted as an alias for `query` — the grep and
	// find escape hatches take `pattern`, and models habitually carry
	// that habit back into combined mode.
	query := strings.TrimSpace(in.Query)
	if query == "" {
		query = strings.TrimSpace(in.Pattern)
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = searchDefaultPath
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}
	if _, err := os.Stat(searchPath); err != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}

	limit := in.Limit
	if limit <= 0 {
		limit = searchDefaultLimit
	}

	// Listing mode: an empty query with an EXPLICIT directory lists that
	// directory — search becomes the one-stop discovery tool ("what's in
	// here" without a second ls-shaped call). Size + mtime per entry,
	// dirs first. An empty query with no path stays an error: the model
	// meant to search for something and dropped it.
	if query == "" {
		if strings.TrimSpace(in.Path) == "" {
			return tooldef.Result{}, errors.New("query is required (or pass path to list a directory)")
		}
		info, err := os.Stat(searchPath)
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
		}
		if !info.IsDir() {
			return tooldef.Result{}, fmt.Errorf("query is required (path %s is a file, not a directory to list)", searchPath)
		}
		if in.Limit <= 0 {
			limit = searchDefaultListLimit
		}
		return runListing(searchPath, limit)
	}

	po := planeOpts{
		ignoreCase: in.IgnoreCase,
		literal:    in.Literal,
		context:    max(in.Context, 0),
		glob:       strings.TrimSpace(in.Glob),
	}

	// Decide which planes to run. In auto mode the query shape picks the
	// extra planes; text is always on because it's the cheapest and most
	// generally useful.
	runText := mode == "auto" || mode == "text"
	runSymbol := mode == "symbol" || (mode == "auto" && looksLikeIdentifier(query))
	runFile := mode == "file" || (mode == "auto" && looksLikePathFragment(query))

	// Run the planes concurrently. Each plane writes into a shared map
	// under a mutex; the renderer reads from the map after all planes
	// finish.
	hits := make(map[string]*fileHit)
	var mu sync.Mutex

	var wg sync.WaitGroup
	var textErr, symbolErr, fileErr error

	if runText {
		wg.Add(1)
		go func() {
			defer wg.Done()
			textErr = runTextPlane(ctx, searchPath, query, po, hits, &mu)
		}()
	}
	if runSymbol {
		wg.Add(1)
		go func() {
			defer wg.Done()
			symbolErr = runSymbolPlane(ctx, searchPath, query, in.Rescan, po.glob, hits, &mu)
		}()
	}
	if runFile {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fileErr = runFilePlane(ctx, searchPath, query, po.glob, hits, &mu)
		}()
	}
	wg.Wait()

	// Surface the first non-nil plane error. A plane that finds nothing
	// isn't an error; a plane that fails to start is.
	for _, e := range []error{textErr, symbolErr, fileErr} {
		if e != nil {
			return tooldef.Result{}, fmt.Errorf("search: %w", e)
		}
	}

	// Rank and trim to limit; stat the survivors for metadata.
	total := len(hits)
	ranked := rankAndTrim(hits, limit, query)
	if len(ranked) == 0 {
		content := fmt.Sprintf("No results for %q across the requested planes.\n"+
			"Hints: ignoreCase:true for case variants, literal:true when the query has regex metacharacters,\n"+
			"mode=find for paths, or mode=grep with a broader pattern.", query)
		detail := fmt.Sprintf("0 files across %s", strings.Join(planesFromMode(mode), "+"))
		return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
	}

	content := renderSearchResults(ranked, query, mode, total)
	planesUsed := []string{}
	if runText {
		planesUsed = append(planesUsed, "text")
	}
	if runSymbol {
		planesUsed = append(planesUsed, "symbol")
	}
	if runFile {
		planesUsed = append(planesUsed, "file")
	}
	detail := fmt.Sprintf("%d files across %s", len(ranked), strings.Join(planesUsed, "+"))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

// looksLikeIdentifier reports whether q is a plausible Go/JS/Rust/Python
// identifier — used in auto mode to decide whether to also query the graph.
// Anything containing path separators or starting with a digit is not an
// identifier. Anything with whitespace is not. The check is intentionally
// loose: a regex meta-char triggers identifier mode too, since rg will
// still run for the text plane.
func looksLikeIdentifier(q string) bool {
	if q == "" {
		return false
	}
	if strings.ContainsAny(q, "/\\\n\r\t ") {
		return false
	}
	// A leading digit makes it look like a number, not a name.
	if q[0] >= '0' && q[0] <= '9' {
		return false
	}
	return true
}

// looksLikePathFragment reports whether q has a path separator or a known
// extension dot, suggesting it's a file/path fragment rather than an
// identifier. Used in auto mode to decide whether to also query fd.
func looksLikePathFragment(q string) bool {
	if q == "" {
		return false
	}
	if strings.ContainsAny(q, "/\\") {
		return true
	}
	// `foo.go`, `bar.ts`, `config.yml` — a dot followed by an extension.
	if i := strings.IndexByte(q, '.'); i > 0 && i < len(q)-1 {
		return true
	}
	return false
}

// runTextPlane shells out to ripgrep with --json and collects matches per
// file. With context > 0 it also collects rg's context events into the
// per-file display slice (context lines render with a · marker).
//
// rg failures are surfaced: exit 2 means a real error (an unparseable
// regex, an unreadable root) and its stderr is included in the error —
// previously an invalid regex silently produced "no results".
func runTextPlane(ctx context.Context, root, query string, po planeOpts, hits map[string]*fileHit, mu *sync.Mutex) error {
	rg, err := exec.LookPath("rg")
	if err != nil {
		// rg is required for the text plane; greptool is also rg-backed and
		// has the same dependency. Surface the error to the caller.
		return errors.New("ripgrep (rg) not found in PATH — text plane unavailable")
	}
	args := []string{"--json", "--line-number", "--color=never", "--hidden", "--no-heading"}
	if po.ignoreCase {
		args = append(args, "--ignore-case")
	}
	if po.literal {
		args = append(args, "--fixed-strings")
	}
	if po.context > 0 {
		args = append(args, "-C", strconv.Itoa(po.context))
	}
	if po.glob != "" {
		args = append(args, "--glob", po.glob)
	}
	args = append(args, "--", query, root)

	cmd := exec.CommandContext(ctx, rg, args...) //nolint:gosec // G204: fixed binary, query is the tool's argument
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("ripgrep stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("ripgrep stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ripgrep start: %w", err)
	}
	// Drain stderr concurrently: an early regex error can fill the pipe
	// and deadlock rg while the stdout loop is still reading.
	var stderrBuf bytes.Buffer
	var drain sync.WaitGroup
	drain.Add(1)
	go func() {
		defer drain.Done()
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	type ev struct {
		Type string `json:"type"`
		Data struct {
			Path struct {
				Text string `json:"text"`
			} `json:"path"`
			Lines struct {
				Text string `json:"text"`
			} `json:"lines"`
			LineNumber int `json:"line_number"`
		} `json:"data"`
	}

	// lastDisplay tracks the last line appended per file so adjacent
	// context regions that share a seam line don't duplicate it.
	lastDisplay := make(map[string]int)
	r := bufio.NewReaderSize(stdout, 64*1024)
	for {
		if ctx.Err() != nil {
			break
		}
		line, err := r.ReadBytes('\n')
		if err != nil {
			break
		}
		var e ev
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		if e.Type != "match" && e.Type != "context" {
			continue
		}
		fp := e.Data.Path.Text
		ln := e.Data.LineNumber
		text := strings.TrimRight(e.Data.Lines.Text, "\n")
		if fp == "" || ln < 1 {
			continue
		}
		// Cap the snippet length so the renderer can keep a per-file block
		// narrow without truncating later.
		if len(text) > textSnippetCap {
			text = text[:textSnippetCap] + "..."
		}
		isMatch := e.Type == "match"
		mu.Lock()
		h := getOrCreate(hits, fp, root)
		if isMatch {
			h.textLines = append(h.textLines, textMatch{lineNumber: ln, text: text})
			h.planes["text"] = struct{}{}
		}
		if lastDisplay[fp] != ln {
			h.display = append(h.display, displayLine{lineNumber: ln, text: text, isMatch: isMatch})
			lastDisplay[fp] = ln
		}
		mu.Unlock()
	}
	waitErr := cmd.Wait()
	drain.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			switch exitErr.ExitCode() {
			case 1:
				// No matches — not an error.
				return nil
			case 2:
				msg := strings.TrimSpace(stderrBuf.String())
				if msg == "" {
					msg = waitErr.Error()
				}
				if po.literal {
					return fmt.Errorf("ripgrep: %s", msg)
				}
				return fmt.Errorf("ripgrep: %s (pass literal:true to treat the query as plain text)", msg)
			}
		}
		return fmt.Errorf("ripgrep: %w", waitErr)
	}
	return nil
}

// runSymbolPlane queries the import graph for files that import the query
// (or are imported by it). Each graph hit records a one-line label so the
// renderer can show "imports <query>" vs "imported by <query>".
func runSymbolPlane(ctx context.Context, root, query string, rescan bool, glob string, hits map[string]*fileHit, mu *sync.Mutex) error {
	// The graph needs a project root. Use the project's Root() if available;
	// fall back to the search path itself.
	rootForGraph := root
	if proj := project.GetDefaultProject(); proj != nil {
		if pr := proj.Root(); pr != "" {
			rootForGraph = pr
		}
	}
	// This is the hot path of the search tool: the cached open walks the
	// workspace stat-only and parses only what changed since the last
	// call. A nil vault db degrades to the old full-parse walk.
	g, err := graph.OpenGraphCached(ctx, rootForGraph, session.DefaultVault().DB(), rescan)
	if err != nil {
		return fmt.Errorf("open graph: %w", err)
	}

	// A graph "match" is any file whose import paths reference the query
	// (treated as an identifier) OR whose own path contains the query.
	// The graph stores import paths as module-style strings (e.g.
	// "github.com/damien1141/a1/internal/graph"); the resolver maps them
	// to workspace-relative file paths.
	q := strings.ToLower(query)
	for _, f := range g.AllFiles() {
		if !globMatchesPath(glob, f) {
			continue
		}
		var label string
		// Does this file's path contain the query? (e.g. auth.go for "auth")
		if strings.Contains(strings.ToLower(f), q) {
			label = "name match"
		}
		// Does this file import something matching the query?
		for _, imp := range g.Imports(f) {
			if strings.Contains(strings.ToLower(imp), q) {
				if label != "" {
					label += ", "
				}
				label += "imports " + shortPath(imp)
				break
			}
		}
		// Is this file imported by something whose path contains the query?
		for _, src := range g.ImportedBy(f) {
			if strings.Contains(strings.ToLower(src), q) {
				if label != "" {
					label += ", "
				}
				label += "imported by " + shortPath(src)
				break
			}
		}
		if label == "" {
			continue
		}
		// Resolve to absolute path for the renderer / line reader.
		abs := f
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(rootForGraph, filepath.FromSlash(f))
		}
		mu.Lock()
		h := getOrCreate(hits, abs, rootForGraph)
		h.graphHit = label
		h.planes["symbol"] = struct{}{}
		mu.Unlock()
	}
	return nil
}

// runFilePlane queries fd for files whose path matches the query.
func runFilePlane(ctx context.Context, root, query, glob string, hits map[string]*fileHit, mu *sync.Mutex) error {
	// filesearch.Search limits results; we pull more than `limit` because
	// the ranking step may merge graph hits over file hits and we want a
	// healthy spread. 100 is enough for most workspaces without choking fd.
	paths, _, err := filesearch.Search(ctx, root, query, 100)
	if err != nil && !errors.Is(err, filesearch.ErrTimeout) {
		return fmt.Errorf("fd: %w", err)
	}
	for _, p := range paths {
		if !globMatchesPath(glob, p) {
			continue
		}
		abs := p
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(root, filepath.FromSlash(p))
		}
		mu.Lock()
		h := getOrCreate(hits, abs, root)
		h.planes["file"] = struct{}{}
		mu.Unlock()
	}
	return nil
}

// getOrCreate fetches or inserts a fileHit for abs, computing its
// cwd-relative path against root. Caller must hold mu.
func getOrCreate(hits map[string]*fileHit, abs, root string) *fileHit {
	abs = filepath.Clean(abs)
	if h, ok := hits[abs]; ok {
		return h
	}
	rel := abs
	if r, err := filepath.Rel(root, abs); err == nil && !strings.HasPrefix(r, "..") {
		rel = filepath.ToSlash(r)
	}
	h := &fileHit{
		rel:    rel,
		abs:    abs,
		planes: make(map[string]struct{}),
	}
	hits[abs] = h
	return h
}

// rankAndTrim converts the hits map to a slice sorted by rank and capped at
// limit, then stats the survivors so textless hits can show size + mtime.
// Rank is: (number of planes hit desc, basename-match boost, number of
// text matches desc, path depth asc, relative path asc). The result is a
// stable, predictable ordering that surfaces files active across multiple
// planes first, a file literally named after the query above deeper
// matches, and shallower (more central) files on ties.
func rankAndTrim(hits map[string]*fileHit, limit int, query string) []*fileHit {
	out := make([]*fileHit, 0, len(hits))
	for _, h := range hits {
		out = append(out, h)
	}
	q := strings.ToLower(query)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		// More planes first.
		if len(a.planes) != len(b.planes) {
			return len(a.planes) > len(b.planes)
		}
		// A file named after the query outranks a file that merely
		// mentions it.
		an := strings.Contains(strings.ToLower(path.Base(a.rel)), q)
		bn := strings.Contains(strings.ToLower(path.Base(b.rel)), q)
		if an != bn {
			return an
		}
		// More text matches next.
		if len(a.textLines) != len(b.textLines) {
			return len(a.textLines) > len(b.textLines)
		}
		// Shallower paths (nearer the root) before deeper ones.
		da, db := strings.Count(a.rel, "/"), strings.Count(b.rel, "/")
		if da != db {
			return da < db
		}
		// Alphabetical for stable tiebreak.
		return a.rel < b.rel
	})
	if len(out) > limit {
		out = out[:limit]
	}
	for _, h := range out {
		if info, err := os.Stat(h.abs); err == nil {
			h.size = info.Size()
			h.mtime = info.ModTime()
			h.statOK = true
		}
	}
	return out
}

// shortPath returns the last two segments of a module-style import path,
// e.g. "github.com/damien1141/a1/internal/graph" → "internal/graph".
func shortPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// renderSearchResults produces the human-readable per-file block. Each
// file gets an @file header with the planes that hit; textless hits show
// size + mtime; text hits show the ordered match/context lines (matches
// with |, context with ·) up to the per-file match cap.
func renderSearchResults(ranked []*fileHit, query, mode string, total int) string {
	var sb strings.Builder
	planesStr := strings.Join(planesFromMode(mode), "+")
	sb.WriteString(fmt.Sprintf("search %q — %d files (%s)", query, len(ranked), planesStr))
	if total > len(ranked) {
		sb.WriteString(fmt.Sprintf(" of %d — raise limit or narrow with glob/path", total))
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for _, h := range ranked {
		// Header: @file path [plane badges]
		badges := make([]string, 0, 3)
		if _, ok := h.planes["text"]; ok {
			badges = append(badges, fmt.Sprintf("text:%d", len(h.textLines)))
		}
		if _, ok := h.planes["symbol"]; ok {
			badges = append(badges, "symbol")
		}
		if _, ok := h.planes["file"]; ok {
			badges = append(badges, "file")
		}
		sb.WriteString(fmt.Sprintf("@file %s [%s]\n", h.rel, strings.Join(badges, " ")))
		if h.graphHit != "" {
			sb.WriteString(fmt.Sprintf("  symbol: %s\n", h.graphHit))
		}
		// A hit with no text lines is a name/path-only discovery — its
		// size and recency are the useful facts.
		if len(h.textLines) == 0 && h.statOK {
			sb.WriteString(fmt.Sprintf("  %s · %s\n", humanSize(h.size), relTime(h.mtime)))
		}
		if len(h.display) > 0 {
			shown := 0
			for _, d := range h.display {
				if d.isMatch {
					shown++
					if shown > textMatchCapPerFile {
						break
					}
					sb.WriteString(fmt.Sprintf("  text:%4d| %s\n", d.lineNumber, d.text))
				} else {
					sb.WriteString(fmt.Sprintf("  text:%4d· %s\n", d.lineNumber, d.text))
				}
			}
			if len(h.textLines) > textMatchCapPerFile {
				sb.WriteString(fmt.Sprintf("  text: ... +%d more matches\n", len(h.textLines)-textMatchCapPerFile))
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// planesFromMode returns the human-readable plane names for the renderer
// header, in the order text → symbol → file (matching the run order).
func planesFromMode(mode string) []string {
	switch mode {
	case "text":
		return []string{"text"}
	case "symbol":
		return []string{"symbol"}
	case "file":
		return []string{"file"}
	default:
		return []string{"text", "symbol", "file"}
	}
}

// globMatchesPath reports whether a slash-form relative path passes the
// glob filter: matched against the base name or the whole path. The text
// plane does not use this — rg's --glob handles it natively with full
// pattern power (**, !negation); this simple matcher serves the symbol and
// file planes, where "*.go"-style filters are the norm.
func globMatchesPath(glob, relSlash string) bool {
	if glob == "" {
		return true
	}
	if ok, err := path.Match(glob, path.Base(relSlash)); err == nil && ok {
		return true
	}
	ok, _ := path.Match(glob, relSlash)
	return ok
}

// runListing renders the empty-query listing mode: a flat, metadata-rich
// walk of root — dirs first, then files, each with size + mtime. Capped at
// limit with an explicit truncation notice.
func runListing(root string, limit int) (tooldef.Result, error) {
	type listEntry struct {
		rel   string
		isDir bool
		size  int64
		mtime time.Time
	}
	var entries []listEntry
	truncated := false
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Unreadable entries are skipped, not fatal — a listing is a
			// glance, not an audit.
			return nil
		}
		if p == root {
			return nil
		}
		if len(entries) >= limit {
			truncated = true
			return fs.SkipAll
		}
		e := listEntry{rel: filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(p, root), string(os.PathSeparator))), isDir: d.IsDir()}
		if info, ierr := d.Info(); ierr == nil {
			e.size = info.Size()
			e.mtime = info.ModTime()
		}
		entries = append(entries, e)
		return nil
	})
	if walkErr != nil {
		return tooldef.Result{}, fmt.Errorf("list: %w", walkErr)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].isDir != entries[j].isDir {
			return entries[i].isDir
		}
		return entries[i].rel < entries[j].rel
	})

	dirs, files := 0, 0
	for _, e := range entries {
		if e.isDir {
			dirs++
		} else {
			files++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("list %s — %d entries (%d dirs, %d files)",
		filepath.ToSlash(root), len(entries), dirs, files))
	if truncated {
		sb.WriteString(fmt.Sprintf(" — truncated after %d, pass limit for more", limit))
	}
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for _, e := range entries {
		if e.isDir {
			sb.WriteString(fmt.Sprintf("  dir            %s/\n", e.rel))
		} else {
			sb.WriteString(fmt.Sprintf("  %-10s %-9s %s\n", humanSize(e.size), relTime(e.mtime), e.rel))
		}
	}

	content := sb.String()
	detail := fmt.Sprintf("%d entries", len(entries))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

// humanSize renders a byte count compactly: 980 B, 12.3 KB, 4.5 MB.
func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// relTime renders a timestamp as a short relative age: 3m ago, 2d ago.
func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dw ago", int(d.Hours()/(24*7)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

// identifierRe is reserved for a tighter future identifier check (e.g.
// Unicode identifier support). The current looksLikeIdentifier is a plain
// ASCII scan; this regex is kept here so future callers don't have to
// rebuild the pattern from scratch.
var identifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// singlePlaneHandlers maps the four single-plane escape-hatch modes (grep,
// find, graph, rank) to their underlying tool's Run handler. Built once at
// package init from the public Tool() constructors so the search dispatcher
// can delegate without re-implementing the underlying logic. The raw
// json.RawMessage input from the search call is forwarded unchanged, so the
// single-plane tools see exactly the arguments they would have seen if the
// caller had invoked them directly.
//
// The package-level init() avoids an init cycle: searchtool importing the
// four packages is fine, but calling XxxTool() at package-var evaluation
// time would race against their own package-var init. Doing it in init()
// runs after all package vars have settled.
var singlePlaneHandlers map[string]func(context.Context, json.RawMessage) (tooldef.Result, error)

func init() {
	singlePlaneHandlers = map[string]func(context.Context, json.RawMessage) (tooldef.Result, error){
		"grep":  greptool.GrepTool().Run,
		"find":  findtool.FindTool().Run,
		"graph": graphtool.GraphTool().Run,
		"rank":  ranktool.RankTool().Run,
	}
}
