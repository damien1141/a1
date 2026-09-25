package testtool

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
)

const (
	testDefaultContextLines = 3
)

var testDescription = `Parse test runner output and extract failures with file:line references.

Supports go test, pytest, and cargo test output. Returns a structured summary
with failure locations so the agent can jump directly to broken tests.`

// TestTool returns the test result interpreter tool definition + handler.
func TestTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "test",
			Description: testDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
			Properties: llm.Object{
				"output": llm.Object{
					"type":        "string",
					"description": "Raw test runner output to parse.",
				},
				"context": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Lines of context around each failure. Example: 3 (default: %d)", testDefaultContextLines),
				},
			},
				Required: []string{"output"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in testInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Output)
			if len(t) > 40 {
				t = t[:40] + "..."
			}
			return fmt.Sprintf("test %s", t)
		},
		Run: runTest,
	}
}

type testInput struct {
	Output  string `json:"output"`
	Context int    `json:"context,omitempty"`
}

type testFailure struct {
	file string
	line int
	text string
}

func runTest(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in testInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse test arguments: %w", err)
	}

	text := strings.TrimSpace(in.Output)
	if text == "" {
		return tooldef.Result{}, errors.New("output is required: paste the test runner output")
	}

	contextLines := in.Context
	if contextLines <= 0 {
		contextLines = testDefaultContextLines
	}

	failures := parseTestFailures(text)
	if len(failures) == 0 {
		return tooldef.Result{Content: "No test failures detected in output", Detail: "0 failures", Output: "No test failures detected in output"}, nil
	}

	var out []string
	for _, f := range failures {
		out = append(out, formatTestFailure(ctx, f, contextLines)...)
	}

	content := strings.Join(out, "\n")
	detail := fmt.Sprintf("%d failures", len(failures))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func parseTestFailures(text string) []testFailure {
	var failures []testFailure

	// Go test: "=== RUN   TestName"
	// followed by "=== FAIL: TestName (0.00s)"
	// and "    file.go:42: assertion failed"
	goTestRe := regexp.MustCompile(`(?m)^===\s+FAIL:\s+(\S+)\s+\(.*\)\s*$\n(?:\s+.*\n)*?\s+([^\s:]+):(\d+):\s+(.*)$`)
	for _, m := range goTestRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[3])
		failures = append(failures, testFailure{
			file: m[2],
			line: ln,
			text: fmt.Sprintf("FAIL: %s — %s", m[1], m[4]),
		})
	}

	// Go panic: "panic: message" followed by file:line
	goPanicRe := regexp.MustCompile(`(?m)^panic:\s*(.*?)\s*$\n\s*([^\s:]+):(\d+)\s+\+\S+`)
	for _, m := range goPanicRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[3])
		failures = append(failures, testFailure{
			file: m[2],
			line: ln,
			text: fmt.Sprintf("panic: %s", m[1]),
		})
	}

	// Pytest: "FAILED test_file.py::test_name - AssertionError"
	pytestFailRe := regexp.MustCompile(`(?m)^FAILED\s+([^\s]+)::([^\s]+)\s+-\s+(.*)$`)
	for _, m := range pytestFailRe.FindAllStringSubmatch(text, -1) {
		failures = append(failures, testFailure{
			file: m[1],
			line: 0,
			text: fmt.Sprintf("FAILED: %s::%s — %s", m[1], m[2], m[3]),
		})
	}

	// Pytest traceback: "file.py:42: AssertionError"
	pytestTracebackRe := regexp.MustCompile(`(?m)^\s+([^\s:]+):(\d+):\s+(.*)$`)
	for _, m := range pytestTracebackRe.FindAllStringSubmatch(text, -1) {
		ln, _ := strconv.Atoi(m[2])
		failures = append(failures, testFailure{
			file: m[1],
			line: ln,
			text: m[3],
		})
	}

	// Cargo test: "test result: FAILED. 1 passed; 1 failed; 0 ignored"
	// followed by "---- test_name stdout ----"
	// and "file.rs:42: assertion failed"
	cargoSectionRe := regexp.MustCompile(`(?m)^----\s+(\S+)\s+stdout\s*----\s*$`)
	cargoFileRe := regexp.MustCompile(`([^\s:]+):(\d+):\s*(.*)`)
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		m := cargoSectionRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		testName := m[1]
		// Collect body until next section header or end.
		var bodyLines []string
		for j := i + 1; j < len(lines); j++ {
			if cargoSectionRe.MatchString(lines[j]) {
				break
			}
			bodyLines = append(bodyLines, lines[j])
		}
		body := strings.Join(bodyLines, "\n")
		for _, line := range strings.Split(body, "\n") {
			if strings.Contains(line, "assertion") || strings.Contains(line, "panic") || strings.Contains(line, "error") {
				if fm := cargoFileRe.FindStringSubmatch(line); fm != nil {
					ln, _ := strconv.Atoi(fm[2])
					failures = append(failures, testFailure{
						file: fm[1],
						line: ln,
						text: fmt.Sprintf("FAIL: %s — %s", testName, fm[3]),
					})
				}
			}
		}
	}

	// Deduplicate by file:line
	seen := make(map[string]bool)
	var unique []testFailure
	for _, f := range failures {
		key := fmt.Sprintf("%s:%d", f.file, f.line)
		if !seen[key] {
			seen[key] = true
			unique = append(unique, f)
		}
	}

	return unique
}

func formatTestFailure(ctx context.Context, f testFailure, contextLines int) []string {
	var out []string

	out = append(out, fmt.Sprintf("// %s", f.text))

	abs := f.file
	if !filepath.IsAbs(abs) {
		abs, _ = resolvePath(ctx, abs)
	}

	rel := tooldef.RelToCwd(ctx, abs)

	lines, err := readFileLines(abs)
	if err != nil {
		out = append(out, fmt.Sprintf("%s:%d#?? | (unable to read file: %v)", rel, f.line, err))
		return out
	}

	start := max(1, f.line-contextLines)
	end := min(len(lines), f.line+contextLines)
	if start > len(lines) {
		start = len(lines)
	}
	if start > end {
		start = end
	}

	fileTag := computeFileTag(lines)
	if fileTag != "" {
		out = append(out, formatFileHeader(rel, fileTag))
	}

	for ln := start; ln <= end; ln++ {
		lineText := lines[ln-1]
		lineText = strings.TrimRight(lineText, "\r")
		h := computeLineHash(lineText)
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
	text := utilNormalizeLF(string(b))
	return strings.Split(text, "\n"), nil
}

func utilNormalizeLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func computeFileTag(lines []string) string {
	text := strings.Join(lines, "\n")
	return utilComputeFileHash(text)
}

func computeLineHash(line string) string {
	return utilComputeLineHash(line)
}

func formatFileHeader(path, tag string) string {
	return utilFormatFileHeader(path, tag)
}

var (
	utilComputeFileHash = func(text string) string {
		return ""
	}
	utilComputeLineHash = func(line string) string {
		return ""
	}
	utilFormatFileHeader = func(path, tag string) string {
		return fmt.Sprintf("@file %s#%s", path, tag)
	}
)
