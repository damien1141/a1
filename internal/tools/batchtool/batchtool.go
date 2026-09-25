package batchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/graph"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

var batchDescription = `Batch editor with dependency ordering.

Sorts a list of files by their dependency order using the project's import graph.
If File A imports File B, B will appear before A in the output. The agent should
then apply edits in the returned order.`

// BatchTool returns the batch editor tool definition + handler.
func BatchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "batch",
			Description: batchDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"files": llm.Object{
						"type":        "array",
						"description": "List of file paths to edit, in any order. Example: [\"internal/auth/auth.go\", \"internal/handler.go\"]",
						"items": llm.Object{
							"type": "string",
						},
					},
					"rescan": llm.Object{
						"type":        "boolean",
						"description": "Rebuild the dependency graph before sorting.",
					},
				},
				Required: []string{"files"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in batchInput
			_ = json.Unmarshal(input, &in)
			n := len(in.Files)
			if n == 0 {
				return "batch"
			}
			return fmt.Sprintf("batch %d files", n)
		},
		Run: runBatch,
	}
}

type batchInput struct {
	Files  []string `json:"files"`
	Rescan bool     `json:"rescan"`
}

func runBatch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in batchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse batch arguments: %w", err)
	}

	if len(in.Files) == 0 {
		return tooldef.Result{}, fmt.Errorf("batch tool requires a non-empty files list")
	}

	proj := project.GetDefaultProject()
	if proj == nil {
		return tooldef.Result{}, fmt.Errorf("batch: project config not loaded")
	}
	root := proj.Root()

	g, err := graph.OpenGraph(ctx, root)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("build graph: %w", err)
	}

	// Normalize file paths to be relative to root.
	clean := make([]string, 0, len(in.Files))
	for _, f := range in.Files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		clean = append(clean, f)
	}
	if len(clean) == 0 {
		return tooldef.Result{}, fmt.Errorf("batch tool requires at least one non-empty file path")
	}

	resolver := graph.NewPathResolver(root)
	order := g.TopologicalSortWithResolver(clean, resolver)

	content := renderBatchResults(order, g)
	detail := fmt.Sprintf("batch sorted %d files by dependency order", len(order))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func renderBatchResults(order []string, g *graph.Graph) string {
	var sb strings.Builder
	sb.WriteString("Batch edit order (dependencies first)\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for i, p := range order {
		sb.WriteString(fmt.Sprintf("%d. @file %s\n", i+1, p))
		imports := g.Imports(p)
		if len(imports) > 0 {
			sb.WriteString(fmt.Sprintf("   imports: %s\n", strings.Join(imports, ", ")))
		}
		importedBy := g.ImportedBy(p)
		if len(importedBy) > 0 {
			sb.WriteString(fmt.Sprintf("   imported by: %s\n", strings.Join(importedBy, ", ")))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
