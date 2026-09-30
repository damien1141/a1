package runtimetool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	runtimeDefaultLimit = 20
)

var runtimeDescription = `Inspect Go test runtime state and failures.

Runs go test in verbose/JSON mode and parses the output to surface failing
tests, stack traces, and action counts. Helps the agent debug without reading
raw test output.`

// RuntimeTool returns the runtime state inspector tool definition + handler.
func RuntimeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "runtime",
			Description: runtimeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Package or directory to test. Example: ./internal/...",
					},
					"run": llm.Object{
						"type":        "string",
						"description": "Test name regex to run. Example: TestAuth",
					},
					"limit": llm.Object{
						"type": "integer",
						"description": fmt.Sprintf(
							"Max failing tests to report. Example: 10 (default: %d)",
							runtimeDefaultLimit,
						),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in runtimeInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				return "runtime"
			}
			return fmt.Sprintf("runtime %s", p)
		},
		Run: runRuntime,
	}
}

type runtimeInput struct {
	Path  string `json:"path,omitempty"`
	Run   string `json:"run,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type testEvent struct {
	Action  string
	Package string
	Name    string
	Output  string
}

func runRuntime(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in runtimeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse runtime arguments: %w", err)
	}

	pkg := strings.TrimSpace(in.Path)
	if pkg == "" {
		pkg = "./..."
	}
	run := strings.TrimSpace(in.Run)
	limit := in.Limit
	if limit <= 0 {
		limit = runtimeDefaultLimit
	}

	args := []string{"test", "-json"}
	if run != "" {
		args = append(args, "-run", run)
	}
	args = append(args, pkg)

	// go test resolves a package path relative to the module root, so the
	// working directory must stay at the module root regardless of the path
	// passed in. Setting cmd.Dir to the package path itself is invalid for
	// any path that is not a directory under the current one.
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = "."

	out, err := cmd.CombinedOutput()
	if err != nil {
		// go test -json still emits JSON on failure; parse what we got.
	}

	events, err := parseTestEvents(string(out))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("runtime: parse test output: %w", err)
	}

	failures := collectFailures(events, limit)
	if len(failures) == 0 {
		return tooldef.Result{
			Content: "No test failures found.\n",
			Detail:  "0 failures",
			Output:  "No test failures found.\n",
		}, nil
	}

	content := renderRuntimeResults(failures)
	detail := fmt.Sprintf("%d failing tests", len(failures))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func parseTestEvents(raw string) ([]testEvent, error) {
	var events []testEvent
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev testEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		events = append(events, ev)
	}
	return events, nil
}

type failure struct {
	Package string
	Name    string
	Output  []string
}

func collectFailures(events []testEvent, limit int) []failure {
	var failures []failure
	var current *failure
	for _, ev := range events {
		switch ev.Action {
		case "run":
			if current != nil {
				failures = append(failures, *current)
				if len(failures) >= limit {
					return failures
				}
				current = nil
			}
		case "fail":
			if current == nil {
				current = &failure{Package: ev.Package, Name: ev.Name}
			}
		case "output":
			if current != nil && ev.Output != "" {
				current.Output = append(current.Output, ev.Output)
			}
		case "pass":
			current = nil
		}
	}
	if current != nil {
		failures = append(failures, *current)
	}
	return failures
}

func renderRuntimeResults(failures []failure) string {
	var sb strings.Builder
	sb.WriteString("Runtime test failures:\n")
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	for i, f := range failures {
		sb.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, f.Package, f.Name))
		for _, line := range f.Output {
			sb.WriteString("   ")
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
