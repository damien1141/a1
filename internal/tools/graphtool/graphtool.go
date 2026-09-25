package graphtool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/graph"
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
	Query    string `json:"query"`
	Path     string `json:"path,omitempty"`
	Direction string `json:"direction,omitempty"`
	Limit    int    `json:"limit"`
	Rescan   bool   `json:"rescan"`
}

func runGraph(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in graphInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse graph arguments: %w", err)
	}

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return tooldef.Result{}, fmt.Errorf("query is required: describe the dependency relationship you want to inspect")
	}

	proj := project.GetDefaultProject()
	if proj == nil {
		return tooldef.Result{}, fmt.Errorf("graph: project config not loaded")
	}
	root := proj.Root()
	g, err := graph.OpenGraph(ctx, root)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("build graph: %w", err)
	}

	target := strings.TrimSpace(in.Path)
	if target == "" {
		target = guessTargetFromQuery(query, g.AllFiles())
	}
	if target == "" {
		return tooldef.Result{Content: "No target file inferred from query. Pass path explicitly.", Detail: "0 results", Output: "No target file inferred from query. Pass path explicitly."}, nil
	}

	direction := strings.ToLower(strings.TrimSpace(in.Direction))
	if direction == "" {
		direction = "both"
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	resolver := graph.NewPathResolver(root)
	var results []string
	switch direction {
	case "imports":
		for _, imp := range g.Imports(target) {
			if r := resolver.Resolve(imp); r != "" {
				results = append(results, r)
			}
		}
	case "imported_by":
		for _, src := range g.ImportedBy(target) {
			results = append(results, src)
		}
	default:
		for _, imp := range g.Imports(target) {
			if r := resolver.Resolve(imp); r != "" {
				results = append(results, r)
			}
		}
		for _, src := range g.ImportedBy(target) {
			results = append(results, src)
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
		return tooldef.Result{Content: "No dependency edges found for " + target, Detail: "0 results", Output: "No dependency edges found for " + target}, nil
	}

	content := renderGraphResults(target, direction, deduped)
	detail := fmt.Sprintf("%d graph results for %s (%s)", len(deduped), target, direction)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
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
