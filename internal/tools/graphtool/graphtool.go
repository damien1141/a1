package graphtool

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/graph"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	defaultLimit = 20
)

var graphDescription = `Dependency/call graph explorer using lightweight import parsing.

Scans the workspace and builds a directed dependency graph from import statements.
Supports Go, Python, Rust, and JS/TS. Queries return file paths only; the agent
can read matched files directly.`

// GraphTool returns the dependency graph tool definition + handler.
func GraphTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "graph",
			Description: graphDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"query": llm.Object{
						"type":        "string",
						"description": "Natural language query about dependencies. Example: \"Who imports auth.go?\"",
					},
					"path": llm.Object{
						"type":        "string",
						"description": "File or module path to inspect. Example: internal/auth/auth.go",
					},
					"direction": llm.Object{
						"type":        "string",
						"description": "Traversal direction: imports, imported_by, or both. Example: imported_by",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results. Example: 20 (default: %d)", defaultLimit),
					},
					"rescan": llm.Object{
						"type":        "boolean",
						"description": "Rebuild the graph before querying.",
					},
				},
				Required: []string{"query"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in graphInput
			_ = json.Unmarshal(input, &in)
			q := strings.TrimSpace(in.Query)
			if q == "" {
				return "graph"
			}
			return fmt.Sprintf("graph %q", q)
		},
		Run: runGraph,
	}
}

type graphInput struct {
	Query     string `json:"query"`
	Path      string `json:"path,omitempty"`
	Direction string `json:"direction,omitempty"`
	Limit     int    `json:"limit"`
	Rescan    bool   `json:"rescan"`
}

func runGraph(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in graphInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse graph arguments: %w", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		query = "show project dependency overview"
	}

	// TryDefaultProject surfaces discovery failure as an error instead of the
	// process-wide panic GetDefaultProject would raise mid-request.
	proj, err := project.TryDefaultProject()
	if err != nil || proj == nil {
		return tooldef.Result{}, fmt.Errorf("graph: project config not loaded")
	}
	root := proj.Root()
	// The vault handle is process-wide and set by every surface at boot;
	// a nil vault (or its nil db) degrades OpenGraphCached to the plain
	// per-call walk, so no vault means exactly the old behavior.
	g, err := graph.OpenGraphCached(ctx, root, session.DefaultVault().DB(), in.Rescan)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("build graph: %w", err)
	}

	target := strings.TrimSpace(in.Path)
	if target == "" {
		target = guessTargetFromQuery(query, g.AllFiles())
	}

	resolver := graph.NewPathResolver(root)
	if target == "" {
		// Fallback: show top imported files as an overview.
		return tooldef.Result{
			Content: renderOverview(g, resolver),
			Detail:  fmt.Sprintf("%d files indexed", g.Len()),
			Output:  renderOverview(g, resolver),
		}, nil
	}

	direction := strings.ToLower(strings.TrimSpace(in.Direction))
	if direction == "" {
		direction = "both"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	// importsOf accepts both spellings: file paths index g.edges directly,
	// and a module-path target is retried once through the resolver, which
	// maps it to the file whose edges the user meant.
	importsOf := func(t string) []string {
		edges := g.Imports(t)
		if len(edges) == 0 {
			if f := resolver.Resolve(t); f != "" {
				edges = g.Imports(f)
			}
		}
		return edges
	}
	var results []string
	switch direction {
	case "imports":
		for _, imp := range importsOf(target) {
			if r := resolver.Resolve(imp); r != "" {
				results = append(results, r)
			}
		}
	case "imported_by":
		for _, key := range reverseKeys(resolver, target) {
			for _, src := range g.ImportedBy(key) {
				results = append(results, src)
			}
		}
	default:
		for _, imp := range importsOf(target) {
			if r := resolver.Resolve(imp); r != "" {
				results = append(results, r)
			}
		}
		for _, key := range reverseKeys(resolver, target) {
			for _, src := range g.ImportedBy(key) {
				results = append(results, src)
			}
		}
	}

	// Deduplicate while preserving order.
	seen := make(map[string]struct{}, len(results))
	deduped := make([]string, 0, len(results))
	for _, p := range results {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		deduped = append(deduped, p)
	}
	if len(deduped) > limit {
		deduped = deduped[:limit]
	}

	if len(deduped) == 0 {
		return tooldef.Result{
			Content: "No dependency edges found for " + target,
			Detail:  "0 results",
			Output:  "No dependency edges found for " + target,
		}, nil
	}

	content := renderGraphResults(target, direction, deduped)
	detail := fmt.Sprintf("%d graph results for %s (%s)", len(deduped), target, direction)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

// reverseKeys expands a user target into every key the graph's reverse index
// may hold for it. The index is keyed by raw import strings — for Go the
// module-qualified package directory ("<module>/internal/auth") — while this
// tool's docs steer users at file paths ("internal/auth/auth.go"), so a
// file-path target never matches unless its package directory and the
// module-prefixed form of each spelling are probed too. The direction query's
// dedup loop absorbs any overlap between candidates.
func reverseKeys(resolver *graph.PathResolver, target string) []string {
	forms := []string{target}
	if dir := path.Dir(filepath.ToSlash(target)); dir != "." && dir != "/" && dir != target {
		// A "." directory candidate could only ever match Python root-package
		// relative imports ("from . import x"), which have nothing to do with
		// the target the user named — pure false-positive surface.
		forms = append(forms, dir)
	}
	if mod := resolver.ModulePath(); mod != "" {
		base := len(forms)
		for _, f := range forms[:base] {
			if f == mod || strings.HasPrefix(f, mod+"/") {
				continue
			}
			forms = append(forms, mod+"/"+f)
		}
	}
	return forms
}

func guessTargetFromQuery(query string, files []string) string {
	q := strings.ToLower(query)
	for _, f := range files {
		if strings.Contains(q, strings.ToLower(f)) {
			return f
		}
	}
	return ""
}

func renderGraphResults(target, direction string, paths []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Dependency graph for %s (%s)\n", target, direction))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for _, p := range paths {
		sb.WriteString("@file ")
		sb.WriteString(p)
		sb.WriteString("\n")
	}
	return sb.String()
}

func renderOverview(g *graph.Graph, resolver *graph.PathResolver) string {
	var sb strings.Builder
	sb.WriteString("Project dependency overview\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Indexed files: %d\n\n", g.Len()))

	// Show top imported files. The reverse index is keyed by raw import
	// paths, so a file's importers are whatever ImportedBy holds under any
	// of its key spellings (file path, package dir, module-prefixed forms) —
	// the same expansion reverseKeys applies to direction queries. One
	// importer may surface under several keys for the same file; dedupe by
	// importer so the count is "how many files import this one", not a
	// count of key spellings.
	type count struct {
		path  string
		count int
	}
	counts := make([]count, 0)
	for _, f := range g.AllFiles() {
		seen := make(map[string]struct{})
		for _, key := range reverseKeys(resolver, f) {
			for _, src := range g.ImportedBy(key) {
				seen[src] = struct{}{}
			}
		}
		if len(seen) > 0 {
			counts = append(counts, count{path: f, count: len(seen)})
		}
	}
	if len(counts) > 0 {
		sb.WriteString("Top imported files:\n")
		for _, c := range counts {
			sb.WriteString(fmt.Sprintf("  %s (%d importers)\n", c.path, c.count))
		}
	}
	return sb.String()
}
