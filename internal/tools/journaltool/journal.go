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

Without a record payload, returns recent entries from .journal in the path
directory. With an action+file+outcome payload, appends one timestamped entry
to that .journal and returns the recorded line — the model uses this to
leave a durable trail of significant actions (edits, runs, decisions).`

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
                                                "description": "Directory holding the .journal file. Example: .",
                                        },
                                        "action": llm.Object{
                                                "type":        "string",
                                                "description": "Optional: record an entry instead of reading. One of edit|read|run|bash|write|test|decision.",
                                        },
                                        "file": llm.Object{
                                                "type":        "string",
                                                "description": "Optional: file path associated with the recorded action. Empty for non-file actions.",
                                        },
                                        "line": llm.Object{
                                                "type":        "integer",
                                                "description": "Optional: line number associated with the recorded action. 0 if not applicable.",
                                        },
                                        "outcome": llm.Object{
                                                "type":        "string",
                                                "description": "Optional: outcome of the recorded action. Example: success, error, skipped.",
                                        },
                                        "limit": llm.Object{
                                                "type": "integer",
                                                "description": fmt.Sprintf(
                                                        "Maximum results to return on read. Example: 20 (default: %d)",
                                                        journalDefaultLimit,
                                                ),
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
                        if strings.TrimSpace(in.Action) != "" {
                                return fmt.Sprintf("journal record %s", strings.TrimSpace(in.Action))
                        }
                        return fmt.Sprintf("journal %s", p)
                },
                Run: runJournal,
        }
}

type journalInput struct {
        Path    string `json:"path,omitempty"`
        Action  string `json:"action,omitempty"`
        File    string `json:"file,omitempty"`
        Line    int    `json:"line,omitempty"`
        Outcome string `json:"outcome,omitempty"`
        Limit   int    `json:"limit,omitempty"`
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

        // Record mode: append a timestamped entry to .journal in the search
        // directory. The previous tool only read .journal and nothing in the
        // codebase ever wrote to it — so the read always returned "No journal
        // entries found." The model now has a way to leave a durable trail.
        if action := strings.TrimSpace(in.Action); action != "" {
                entry, line, err := writeJournalEntry(searchPath, journalEntry{
                        timestamp: time.Now().UTC(),
                        action:    action,
                        file:      strings.TrimSpace(in.File),
                        line:      in.Line,
                        outcome:   strings.TrimSpace(in.Outcome),
                })
                if err != nil {
                        return tooldef.Result{}, err
                }
                content := renderJournalResults(ctx, []journalEntry{entry})
                detail := fmt.Sprintf("recorded %s @ %s", action, entry.timestamp.Format(time.RFC3339))
                _ = line
                return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
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
                return tooldef.Result{
                        Content: "No journal entries found",
                        Detail:  "0 entries",
                        Output:  "No journal entries found",
                }, nil
        }

        content := renderJournalResults(ctx, entries)
        detail := fmt.Sprintf("%d journal entries", len(entries))
        return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

// writeJournalEntry appends a single entry to root/.journal using the line
// format that parseJournalLine expects: timestamp|action|file|line|outcome.
// The .journal file is created if missing. The format is intentionally
// pipe-delimited so it can be tailed, grepped, and parsed without a yaml/json
// dependency.
func writeJournalEntry(root string, entry journalEntry) (journalEntry, int, error) {
        journalPath := filepath.Join(root, ".journal")
        // 0644 matches the rest of the project's text-file artifacts. The
        // .journal file is meant to be inspected by humans and tools, not a
        // secret.
        f, err := os.OpenFile(journalPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
        if err != nil {
                return journalEntry{}, 0, fmt.Errorf("open journal: %w", err)
        }
        defer f.Close()

        line := fmt.Sprintf(
                "%s|%s|%s|%d|%s\n",
                entry.timestamp.Format(time.RFC3339),
                entry.action,
                entry.file,
                entry.line,
                entry.outcome,
        )
        n, err := f.WriteString(line)
        if err != nil {
                return journalEntry{}, 0, fmt.Errorf("write journal: %w", err)
        }
        return entry, n, nil
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
                sb.WriteString(
                        fmt.Sprintf(
                                "%s\t%s\t%s:%d\t%s\n",
                                e.timestamp.Format("2006-01-02 15:04:05"),
                                e.action,
                                rel,
                                e.line,
                                e.outcome,
                        ),
                )
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
