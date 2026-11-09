package readtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/damien1141/a1/internal/tools/lstool"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/tools/watchertool"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/util"
)

const (
	readDefaultMaxLines = 1000
	readDefaultMaxBytes = 50 * 1024
)

var readDescription = fmt.Sprintf(`Read files and directory state. Prefer this over bash cat/head/tail.

- file (default): read a file. Pass path; offset (1-based) + limit paginate.
  Lines are 'line_number|content', capped at %d lines / %d KiB per call.
  Re-reading an unchanged file returns an "unchanged" note while the earlier
  content is still in context; refresh: true forces the content again.
- ls: list a directory (names, sizes, types).
- watch: files changed on disk since the last check (first call sets the
  baseline).`,
	readDefaultMaxLines, readDefaultMaxBytes/1024)

// ReadTool returns the read tool definition + handler.
// Dispatches by mode: file (default) → runRead, ls → lstool.LsTool().Run,
// watch → watchertool.WatcherTool().Run. The two non-file modes are
// escape hatches that delegate to the underlying tool without changing
// the read surface — one tool to learn, three behaviors available.
func ReadTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "read",
			Description: readDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"mode": llm.Object{
						"type":        "string",
						"description": "file (default) | ls | watch.",
						"enum":        []string{"file", "ls", "watch"},
					},
					"path": llm.Object{
						"type":        "string",
						"description": "File (file mode) or directory (ls/watch).",
					},
					"offset": llm.Object{
						"type":        "integer",
						"description": "First line to return, 1-based (file mode).",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum lines to return (mode=file), capped at %d. Ignored in ls/watch modes.", readDefaultMaxLines),
					},
					"refresh": llm.Object{
						"type":        "boolean",
						"description": "file mode: return full content even when unchanged since your last read (identical re-reads otherwise collapse to a note).",
					},
				},
				Required: []string{"path"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in readInput
			_ = json.Unmarshal(input, &in)
			return strings.TrimSpace(in.Path)
		},
		Run: runReadDispatch,
	}
}

// readInput is the union of file + ls + watch params. Mode picks the
// handler; the unused fields are ignored by the dispatched-to handler.
// Refresh is consumed by the executor-level read elider (see
// internal/agent/readcache.go), not the read itself — the tool always reads.
type readInput struct {
	Mode    string `json:"mode,omitempty"`
	Path    string `json:"path"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
	Refresh bool   `json:"refresh,omitempty"`
}

// runReadDispatch reads the mode field and routes to the matching handler.
// Default mode is "file" for backward compatibility; an empty mode is
// treated as "file" so legacy callers that only sent path + offset + limit
// keep working.
func runReadDispatch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse read arguments: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "file"
	}
	switch mode {
	case "file":
		return runRead(ctx, input)
	case "ls":
		return lstool.LsTool().Run(ctx, input)
	case "watch":
		return watchertool.WatcherTool().Run(ctx, input)
	default:
		return tooldef.Result{}, fmt.Errorf("unknown read mode %q: use file, ls, or watch", in.Mode)
	}
}

func runRead(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in readInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse read arguments: %w", err)
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return tooldef.Result{}, errors.New("path is required")
	}
	path, err := tooldef.ResolveToCwd(ctx, path)
	if err != nil {
		return tooldef.Result{}, err
	}

	select {
	case <-ctx.Done():
		return tooldef.Result{}, ctx.Err()
	default:
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return tooldef.Result{}, err
	}
	text := util.NormalizeLF(string(raw))
	display := tooldef.RelToCwd(ctx, path)
	header := "@file " + display

	startLine := in.Offset
	startLine = max(startLine, 1)
	limit := in.Limit
	if limit <= 0 || limit > readDefaultMaxLines {
		limit = readDefaultMaxLines
	}

	lines := strings.Split(text, "\n")
	// Trailing empty split from final newline is fine for line numbering.
	if text == "" {
		out := header + "\n(empty file)"
		return tooldef.Result{Content: out, Detail: display, Output: out}, nil
	}

	var (
		b         strings.Builder
		collected int
		bytesN    int
	)
	b.WriteString(header)
	b.WriteByte('\n')

	for lineNo := startLine; lineNo <= len(lines); lineNo++ {
		select {
		case <-ctx.Done():
			return tooldef.Result{}, ctx.Err()
		default:
		}
		line := lines[lineNo-1]
		if bytesN+len(line)+1 > readDefaultMaxBytes {
			if bytesN == 0 && collected == 0 {
				// The very first line to collect alone busts the cap. A
				// truncation notice would say "Next offset: <lineNo>" — the
				// same line — so a model following the hint re-reads it
				// identically forever. Hard-split instead: emit the prefix
				// that fits and resume strictly past the over-long line.
				writeSplitLine(&b, lineNo, line)
			} else {
				// Mid-stream over-long line: something is already collected,
				// so pointing back at this line is safe — the re-read lands
				// on it as the FIRST line and hard-splits.
				fmt.Fprintf(&b, "\n... truncated at %d bytes. Next offset: %d\n", readDefaultMaxBytes, lineNo)
			}
			break
		}
		fmt.Fprintf(&b, "%4d|%s\n", lineNo, line)
		bytesN += len(line) + 1
		collected++
		if collected >= limit {
			if lineNo < len(lines) {
				fmt.Fprintf(&b, "... truncated at %d lines. Next offset: %d\n", limit, lineNo+1)
			}
			break
		}
	}

	out := b.String()
	return tooldef.Result{Content: out, Detail: display, Output: out}, nil
}

// splitMarker is the hard-split notice appended after an over-long line's
// emitted prefix. The next offset is lineNo+1 so a following read strictly
// advances past the line that could not fit.
func splitMarker(lineNo, cut int) string {
	return fmt.Sprintf("... line %d split at %d bytes. Next offset: %d\n", lineNo, cut, lineNo+1)
}

// writeSplitLine emits the byte-cap hard split of an over-long line: the
// "%4d|" prefix (same formatting as normal lines so widths stay consistent),
// as many content bytes as fit, the newline, and the split marker. The
// budget is the cap minus the prefix, its newline, and the marker line; the
// marker's own width shifts with the cut's digit count, so the cut is
// settled with a short loop rather than closed-form arithmetic.
func writeSplitLine(b *strings.Builder, lineNo int, line string) {
	prefix := fmt.Sprintf("%4d|", lineNo)
	cut := readDefaultMaxBytes - len(prefix) - 1
	for cut > 0 && len(prefix)+cut+1+len(splitMarker(lineNo, cut)) > readDefaultMaxBytes {
		cut--
	}
	if cut > len(line) {
		cut = len(line)
	}
	b.WriteString(prefix)
	b.WriteString(line[:cut])
	b.WriteByte('\n')
	b.WriteString(splitMarker(lineNo, cut))
}
