package contexttool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	contextDefaultLimit = 50
)

var contextDescription = `Fold-based context management analysis.

Applies billion-context fold concepts (protected zones, recent zone, growth-gated
compression, block tiers) to the analyzed codebase or session and reports
compressible increments, protected content, and fold health.`

// ContextTool returns the context budget manager tool definition + handler.
func ContextTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "context",
			Description: contextDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory to analyze. Example: .",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 20 (default: %d)", contextDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in contextInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("context %s", p)
		},
		Run: runContext,
	}
}

type contextInput struct {
	Path  string `json:"path,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type contextEntry struct {
	file     string
	line     int
	message  string
	suggestion string
	category string
	tier string
}

func runContext(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in contextInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse context arguments: %w", err)
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

	limit := in.Limit
	if limit <= 0 {
		limit = contextDefaultLimit
	}

	entries, err := analyzeContext(searchPath, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	report := buildFoldReport(entries)
	content := renderContextResults(ctx, entries)
	detail := renderFoldDetail(report)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func analyzeContext(root string, limit int) ([]contextEntry, error) {
	var entries []contextEntry

	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			if info != nil && info.IsDir() {
				if shouldSkipDir(filepath.Base(path)) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".go" {
			return nil
		}

		fileEntries, err := analyzeFileContext(path)
		if err != nil {
			return nil
		}
		entries = append(entries, fileEntries...)
		if len(entries) >= limit {
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

func analyzeFileContext(path string) ([]contextEntry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var entries []contextEntry
	for i, line := range lines {
		if strings.Contains(line, "context.WithValue") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "context.WithValue usage detected",
				suggestion: "consider using typed context keys for better type safety",
				category: "protected-zone",
				tier: "tier-1",
			})
		}
		if strings.Contains(line, "context.Background()") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "context.Background() usage detected",
				suggestion: "consider using context.WithTimeout for bounded operations",
				category: "recent-zone",
				tier: "tier-1",
			})
		}
		if strings.Contains(line, "http.NewRequest") && !strings.Contains(line, "WithContext") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "HTTP request without context",
				suggestion: "use http.NewRequestWithContext with a timeout",
				category: "growth-gate",
				tier: "tier-2",
			})
		}
		if strings.Contains(line, "context.WithTimeout") || strings.Contains(line, "context.WithDeadline") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "bounded context detected",
				suggestion: "protected zone: timeout discipline confirmed",
				category: "protected-zone",
				tier: "tier-1",
			})
		}
		if strings.Contains(line, "cancel") && strings.Contains(line, "defer") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "cancellation cleanup detected",
				suggestion: "protected zone: cancel discipline confirmed",
				category: "protected-zone",
				tier: "tier-1",
			})
		}
		if strings.Contains(line, "io.ReadAll") || strings.Contains(line, "ioutil.ReadAll") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "unbounded read detected",
				suggestion: "limit body reads or compress the result before adding to context",
				category: "growth-gate",
				tier: "tier-2",
			})
		}
		if strings.Contains(line, "json.Marshal") || strings.Contains(line, "json.MarshalIndent") {
			entries = append(entries, contextEntry{
				file: path,
				line: i + 1,
				message: "JSON serialization detected",
				suggestion: "compress large payloads before adding to context",
				category: "growth-gate",
				tier: "tier-2",
			})
		}
	}

	return entries, nil
}

func renderContextResults(ctx context.Context, entries []contextEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		rel := tooldef.RelToCwd(ctx, e.file)
		sb.WriteString(fmt.Sprintf("%s:%d\t%s\t%s\t%s\t%s\n", rel, e.line, e.category, e.tier, e.message, e.suggestion))
	}
	if sb.Len() == 0 {
		return "No context issues detected"
	}
	return sb.String()
}

func foldHealthSummary(entries []contextEntry) string {
	protected := 0
	growth := 0
	recent := 0
	tier1 := 0
	tier2 := 0
	for _, e := range entries {
		switch e.category {
		case "protected-zone":
			protected++
		case "growth-gate":
			growth++
		case "recent-zone":
			recent++
		}
		switch e.tier {
		case "tier-1":
			tier1++
		case "tier-2":
			tier2++
		}
	}
	return fmt.Sprintf("protected-zone=%d growth-gate=%d recent-zone=%d tier-1=%d tier-2=%d total=%d", protected, growth, recent, tier1, tier2, len(entries))
}

type foldReport struct {
	entries     int
	protected   int
	growth      int
	recent      int
	tier1       int
	tier2       int
}

func buildFoldReport(entries []contextEntry) foldReport {
	report := foldReport{entries: len(entries)}
	for _, e := range entries {
		switch e.category {
		case "protected-zone":
			report.protected++
		case "growth-gate":
			report.growth++
		case "recent-zone":
			report.recent++
		}
		switch e.tier {
		case "tier-1":
			report.tier1++
		case "tier-2":
			report.tier2++
		}
	}
	return report
}

func renderFoldDetail(report foldReport) string {
	return fmt.Sprintf("protected-zone=%d growth-gate=%d recent-zone=%d tier-1=%d tier-2=%d total=%d", report.protected, report.growth, report.recent, report.tier1, report.tier2, report.entries)
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}
