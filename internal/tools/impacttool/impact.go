package impacttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	impactDefaultLimit = 100
)

var impactDescription = `Analyze the blast radius of renaming or changing a Go symbol.

Finds all references to the target symbol across the codebase and returns
the affected files and lines so the agent can assess refactoring risk.`

// ImpactTool returns the refactoring impact analyzer tool definition + handler.
func ImpactTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "impact",
			Description: impactDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"symbol": llm.Object{
						"type":        "string",
						"description": "Symbol name to analyze. Example: PublicFunc",
					},
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or package path. Example: ./internal/tools",
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File pattern filter. Example: *.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", impactDefaultLimit),
					},
				},
				Required: []string{"symbol"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in impactInput
			_ = json.Unmarshal(input, &in)
			return fmt.Sprintf("impact %s", strings.TrimSpace(in.Symbol))
		},
		Run: runImpact,
	}
}

type impactInput struct {
	Symbol string `json:"symbol"`
	Path   string `json:"path,omitempty"`
	Glob   string `json:"glob,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type impactRef struct {
	file    string
	line    int
	context string
}

func runImpact(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in impactInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse impact arguments: %w", err)
	}

	symbol := strings.TrimSpace(in.Symbol)
	if symbol == "" {
		return tooldef.Result{}, errors.New("symbol is required: provide the symbol name to analyze")
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = "."
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}

	if _, err := os.Stat(searchPath); err != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}

	glob := strings.TrimSpace(in.Glob)
	if glob == "" {
		glob = "*.go"
	}

	limit := in.Limit
	if limit <= 0 {
		limit = impactDefaultLimit
	}

	refs, err := findSymbolRefs(searchPath, symbol, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(refs) == 0 {
		return tooldef.Result{Content: "No references found for " + symbol, Detail: "0 refs", Output: "No references found for " + symbol}, nil
	}

	content := renderImpactResults(ctx, refs)
	detail := fmt.Sprintf("%d references to %s", len(refs), symbol)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func findSymbolRefs(root, symbol, glob string, limit int) ([]impactRef, error) {
	args := []string{
		"-r",
		"-n",
		"--include=" + glob,
		symbol,
		root,
	}
	cmd := exec.Command("grep", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("grep: %s", strings.TrimSpace(string(out)))
	}

	lines := strings.Split(string(out), "\n")
	var refs []impactRef
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 2 {
			continue
		}
		file := parts[0]
		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)
		context := ""
		if len(parts) >= 3 {
			context = strings.TrimSpace(parts[2])
		}
		refs = append(refs, impactRef{
			file:    file,
			line:    lineNum,
			context: context,
		})
		if len(refs) >= limit {
			break
		}
	}

	return refs, nil
}

func renderImpactResults(ctx context.Context, refs []impactRef) string {
	var sb strings.Builder
	for _, r := range refs {
		rel := tooldef.RelToCwd(ctx, r.file)
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\n", rel, r.line, r.context))
	}
	return sb.String()
}
