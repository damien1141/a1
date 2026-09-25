package errortool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	errorDefaultLimit = 10
)

var errorDescription = `Match an error message against a library of known error patterns and return suggested fixes.

Supports Go, Python, Rust, and generic build/test errors. Returns the pattern
name, description, and actionable fix so the agent can apply known corrections.`

// ErrorTool returns the error pattern library tool definition + handler.
func ErrorTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "error",
			Description: errorDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
			Properties: llm.Object{
				"text": llm.Object{
					"type":        "string",
					"description": "Error message or stack trace line to match.",
				},
				"limit": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum matches to return. Example: 5 (default: %d)", errorDefaultLimit),
				},
			},
				Required: []string{"text"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in errorInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Text)
			if len(t) > 40 {
				t = t[:40] + "..."
			}
			return fmt.Sprintf("error match %s", t)
		},
		Run: runError,
	}
}

type errorInput struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"`
}

type errorPattern struct {
	name        string
	description string
	fix         string
	re          *regexp.Regexp
}

var errorPatterns = []errorPattern{
	{
		name:        "Go: undefined variable",
		description: "The Go compiler cannot find the referenced identifier.",
		fix:         "Check for typos, ensure the variable is declared in scope, and verify imports are correct.",
		re:          regexp.MustCompile(`(?i)undefined:\s+(\S+)`),
	},
	{
		name:        "Go: type mismatch",
		description: "A value of the wrong type was assigned or passed.",
		fix:         "Convert the value explicitly, or update the function signature to accept the intended type.",
		re:          regexp.MustCompile(`(?i)cannot use .+ as type`),
	},
	{
		name:        "Go: missing return",
		description: "A function with a return type is missing a return statement.",
		fix:         "Add a return statement with the expected values at the end of the function.",
		re:          regexp.MustCompile(`(?i)missing return at end of function`),
	},
	{
		name:        "Go: nil pointer dereference",
		description: "A nil pointer was dereferenced at runtime.",
		fix:         "Add nil checks before dereferencing, or ensure the pointer is initialized before use.",
		re:          regexp.MustCompile(`(?i)runtime error: invalid memory address or nil pointer dereference`),
	},
	{
		name:        "Go: index out of range",
		description: "A slice or array index was accessed outside its bounds.",
		fix:         "Check the index bounds before accessing, or use len() to validate the range.",
		re:          regexp.MustCompile(`(?i)runtime error: index out of range`),
	},
	{
		name:        "Python: module not found",
		description: "Python cannot find the imported module.",
		fix:         "Install the missing package with pip, or add the module path to PYTHONPATH.",
		re:          regexp.MustCompile(`(?i)ModuleNotFoundError:\s+No module named\s+'([^']+)'`),
	},
	{
		name:        "Python: key error",
		description: "A dictionary key was not found.",
		fix:         "Use dict.get(key, default) or check key existence before access.",
		re:          regexp.MustCompile(`(?i)KeyError:\s+'([^']+)'`),
	},
	{
		name:        "Python: type error",
		description: "An operation was performed on an incompatible type.",
		fix:         "Add type conversion or validate input types before the operation.",
		re:          regexp.MustCompile(`(?i)TypeError:\s+(.*)`),
	},
	{
		name:        "Rust: borrow checker error",
		description: "The Rust borrow checker rejected an ownership pattern.",
		fix:         "Clone the value, use references with appropriate lifetimes, or restructure ownership.",
		re:          regexp.MustCompile(`(?i)borrow of moved value|cannot borrow.*as mutable|borrowed value does not live long enough`),
	},
	{
		name:        "Rust: index out of bounds",
		description: "A vector index was out of bounds.",
		fix:         "Check the index with .get() or validate the length before indexing.",
		re:          regexp.MustCompile(`(?i)index out of bounds:\s+the len is \d+ but the index is`),
	},
	{
		name:        "Rust: unwrap on None",
		description: "unwrap() was called on a None value.",
		fix:         "Use if let, match, or .expect(\"message\") to handle the None case explicitly.",
		re:          regexp.MustCompile(`(?i)called\s+` + "`" + `unwrap` + "`" + `\s+on\s+a\s+` + "`" + `None` + "`" + `\s+value`),
	},
	{
		name:        "Node: require not found",
		description: "Node.js cannot find the required module.",
		fix:         "Run npm install, check the module name, or verify NODE_PATH.",
		re:          regexp.MustCompile(`(?i)Error:\s+Cannot find module\s+'([^']+)'`),
	},
	{
		name:        "Generic: permission denied",
		description: "A file system or network operation was denied.",
		fix:         "Check file permissions, run with appropriate privileges, or verify the path exists.",
		re:          regexp.MustCompile(`(?i)permission denied`),
	},
	{
		name:        "Generic: out of memory",
		description: "The process ran out of memory.",
		fix:         "Reduce memory usage, increase available memory, or add swap space.",
		re:          regexp.MustCompile(`(?i)out of memory|fatal error: runtime: cannot allocate memory`),
	},
	{
		name:        "Generic: connection refused",
		description: "A network connection was refused.",
		fix:         "Verify the service is running, check the host/port, and ensure firewall rules allow the connection.",
		re:          regexp.MustCompile(`(?i)connection refused`),
	},
}

func runError(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in errorInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse error arguments: %w", err)
	}

	text := strings.TrimSpace(in.Text)
	if text == "" {
		return tooldef.Result{}, errors.New("text is required: paste the error message")
	}

	limit := in.Limit
	if limit <= 0 {
		limit = errorDefaultLimit
	}

	var matches []string
	for _, pattern := range errorPatterns {
		subs := pattern.re.FindStringSubmatch(text)
		if len(subs) > 0 {
			matches = append(matches, fmt.Sprintf("[%s]\n  Matched: %s\n  %s\n  Fix: %s", pattern.name, subs[0], pattern.description, pattern.fix))
			if len(matches) >= limit {
				break
			}
		}
	}

	if len(matches) == 0 {
		return tooldef.Result{Content: "No known error patterns matched", Detail: "0 matches", Output: "No known error patterns matched"}, nil
	}

	content := strings.Join(matches, "\n\n")
	detail := fmt.Sprintf("%d pattern matches", len(matches))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}
