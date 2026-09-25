package journaltool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	journalDefaultLimit = 50
)

var journalDescription = `Record and review action history for debugging and audit.

Logs operational actions with timestamps, file references, and outcomes
so the agent can replay sessions and debug where things went wrong.`

// JournalTool returns the action journal / audit trail tool definition + handler.
func JournalTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "journal",
			Description: journalDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory to analyze. Example: .",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 20 (default: %d)", journalDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in journalInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("journal %s", p)
		},
		Run: runJournal,
	}
}

type journalInput struct {
	Path  string `json:"path,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type journalEntry struct {
	timestamp time.Time
	action    string
	file      string
	line      int
	outcome   string
}

func runJournal(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in journalInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse journal arguments: %w", err)
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
		limit = journalDefaultLimit
	}

	entries, err := readJournal(searchPath, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(entries) == 0 {
		return tooldef.Result{Content: "No journal entries found", Detail: "0 entries", Output: "No journal entries found"}, nil
	}

	content := renderJournalResults(ctx, entries)
	detail := fmt.Sprintf("%d journal entries", len(entries))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func readJournal(root string, limit int) ([]journalEntry, error) {
	journalPath := filepath.Join(root, ".journal")
	if _, err := os.Stat(journalPath); err != nil {
		return nil, nil
	}

	b, err := os.ReadFile(journalPath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")

	var entries []journalEntry
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entry, err := parseJournalLine(line)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= limit {
			break
		}
	}

	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

func parseJournalLine(line string) (journalEntry, error) {
	parts := strings.SplitN(line, "|", 5)
	if len(parts) < 5 {
		return journalEntry{}, fmt.Errorf("invalid journal line: %s", line)
	}

	timestamp, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[0]))
	if err != nil {
		return journalEntry{}, err
	}

	return journalEntry{
		timestamp: timestamp,
		action:    strings.TrimSpace(parts[1]),
		file:      strings.TrimSpace(parts[2]),
		line:      parseInt(strings.TrimSpace(parts[3])),
		outcome:   strings.TrimSpace(parts[4]),
	}, nil
}

func parseInt(s string) int {
	var n int
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func renderJournalResults(ctx context.Context, entries []journalEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		rel := tooldef.RelToCwd(ctx, e.file)
		sb.WriteString(fmt.Sprintf("%s\t%s\t%s:%d\t%s\n", e.timestamp.Format("2006-01-02 15:04:05"), e.action, rel, e.line, e.outcome))
	}
	return sb.String()
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}
