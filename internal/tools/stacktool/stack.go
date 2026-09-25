package stacktool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/util"
)

const (
	stackDefaultContextLines = 5
)

var stackDescription = `Parse a runtime stack trace and resolve file paths to line-accurate source references.

Accepts Go, Python, Node, and Rust-style stack traces. Returns file:line#hash
anchors plus surrounding context so the agent can jump directly to the failure.`

// StackTool returns the stack trace navigator tool definition + handler.
func StackTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "stack",
			Description: stackDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
			Properties: llm.Object{
				"text": llm.Object{
					"type":        "string",
					"description": "Stack trace text to parse. Paste the full traceback.",
				},
				"context": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Lines of source context around each frame. Example: 5 (default: %d)", stackDefaultContextLines),
				},
			},
				Required: []string{"text"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in stackInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Text)
			if len(t) > 40 {
				t = t[:40] + "..."
			}
			return fmt.Sprintf("stack %s", t)
		},
		Run: runStack,
	}
}

type stackInput struct {
	Text    string `json:"text"`
	Context int    `json:"context,omitempty"`
}

func runStack(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in stackInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse stack arguments: %w", err)
	}

	text := strings.TrimSpace(in.Text)
	if text == "" {
		return tooldef.Result{}, errors.New("text is required: paste the stack trace")
	}

	contextLines := in.Context
	if contextLines <= 0 {
		contextLines = stackDefaultContextLines
	}

	frames, err := parseStackFrames(text)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(frames) == 0 {
		return tooldef.Result{Content: "No recognizable stack frames found in input", Detail: "0 frames", Output: "No recognizable stack frames found in input"}, nil
	}

	var out []string
	for _, f := range frames {
		out = append(out, formatFrame(ctx, f, contextLines)...)
	}

	content := strings.Join(out, "\n")
	detail := fmt.Sprintf("%d frames", len(frames))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

type stackFrame struct {
	file string
	line int
	text string
}

func parseStackFrames(text string) ([]stackFrame, error) {
	var frames []stackFrame

	// Go-style: "\t/path/to/file.go:123 +0x45"
	goRe := regexp.MustCompile(`(?m)^\s+([^\s:]+):(\d+)(?:\s+\+\S+)?$`)
	for _, m := range goRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		frames = append(frames, stackFrame{file: m[1], line: ln, text: m[0]})
	}

	// Python-style: '  File "file.py", line 123, in module'
	pythonRe := regexp.MustCompile(`(?m)^\s+File\s+"([^"]+)",\s+line\s+(\d+),`)
	for _, m := range pythonRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		frames = append(frames, stackFrame{file: m[1], line: ln, text: m[0]})
	}

	// Node-style: "    at main (/path/to/file.js:123:45)"
	nodeRe := regexp.MustCompile(`(?m)^\s+at\s+(?:\S+\s+)?\(?([^\s:]+):(\d+):\d+\)?`)
	for _, m := range nodeRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		frames = append(frames, stackFrame{file: m[1], line: ln, text: m[0]})
	}

	// Rust panic format: "thread 'main' panicked at 'x', /path/to/file.rs:2:5"
	rustPanicRe := regexp.MustCompile(`(?m)panicked at [^,]*,\s*([^\s:]+):(\d+):\d+`)
	for _, m := range rustPanicRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		frames = append(frames, stackFrame{file: m[1], line: ln, text: m[0]})
	}

	// Rust backtrace format: "    at /path/to/file.rs:123:45"
	rustRe := regexp.MustCompile(`(?m)^\s+at\s+([^\s:]+):(\d+):\d+$`)
	for _, m := range rustRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		frames = append(frames, stackFrame{file: m[1], line: ln, text: m[0]})
	}

	// Deduplicate by file:line
	seen := make(map[string]bool)
	var unique []stackFrame
	for _, f := range frames {
		key := fmt.Sprintf("%s:%d", f.file, f.line)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, f)
		}
	}

	return unique, nil
}

func formatFrame(ctx context.Context, f stackFrame, contextLines int) []string {
	var out []string

	abs := f.file
	if !filepath.IsAbs(abs) {
		abs, _ = resolvePath(ctx, abs)
	}

	rel := tooldef.RelToCwd(ctx, abs)

	lines, err := readFileLines(abs)
	if err != nil {
		out = append(out, fmt.Sprintf("%s:%d#?? | %s | (unable to read file: %v)", rel, f.line, f.text, err))
		return out
	}

	start := max(1, f.line-contextLines)
	end := min(len(lines), f.line+contextLines)

	fileTag := computeFileTag(lines)
	if fileTag != "" {
		out = append(out, util.FormatFileHeader(rel, fileTag))
	}

	for ln := start; ln <= end; ln++ {
		lineText := lines[ln-1]
		lineText = strings.TrimRight(lineText, "\r")
		h := util.ComputeLineHash(lineText)
		ref := fmt.Sprintf("%d#%s", ln, h)
		prefix := "  "
		if ln == f.line {
			prefix = ">>"
		}
		out = append(out, fmt.Sprintf("%s:%s%s|%s", rel, prefix, ref, lineText))
	}

	return out
}

func resolvePath(ctx context.Context, p string) (string, error) {
	if filepath.IsAbs(p) {
		return p, nil
	}
	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		return p, err
	}
	return filepath.Join(cwd, p), nil
}

func readFileLines(abs string) ([]string, error) {
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	text := util.NormalizeLF(string(b))
	return strings.Split(text, "\n"), nil
}

func computeFileTag(lines []string) string {
	text := strings.Join(lines, "\n")
	return util.ComputeFileHash(text)
}
