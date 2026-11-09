// Package codetool is the unified code-analysis tool. It collapses 19
// separate code-quality / analysis tools into one tool with a `mode`
// field. Each mode forwards the raw json.RawMessage input to the
// underlying tool's Run handler unchanged, so the underlying tool sees
// exactly the arguments it would have seen if the caller had invoked it
// directly.
//
// Why one tool instead of 19? The LLM's tool-selection head has a finite
// budget: with 19 separate schemas that overlap on `path` and `limit`,
// the model spends attention picking among near-identical tools. One
// tool with a `mode` enum collapses that decision into a single
// string-typed field the model can reliably fill, and the dispatch is
// mechanical. The underlying tool packages stay in place; only the
// registry changes.
//
// Modes:
//
//	api         — exported API surface + unused-public-API flagging (apitool)
//	deadcode    — unused exported symbols across the workspace (deadcode)
//	coverage    — go test -cover profile parser (coveragetool)
//	impact      — change-impact analysis (impacttool)
//	deps        — dependency-graph inspector (depstool)
//	nplusone    — N+1 query / loop detection (nplusonetool)
//	testimpact  — test selection by impact (testimpacttool)
//	vuln        — vulnerability scan via govulncheck (vulntool)
//	scaffold    — code scaffolding from templates (scaffoldtool)
//	migration   — API migration helper (migrationtool)
//	token       — token-counter for files (tokentool)
//	property    — property-test skeleton generator (propertytool)
//	runtime     — go test runtime failure inspector (runtimetool)
//	build       — build the project (buildtool)
//	judge       — decision-model (JEV) classification of state or files (judgetool)
//	errtrans    — translate error text to a diagnosis (errortrans)
//	error       — match error text against known patterns for a fix (errortool)
//	stack       — parse a stack trace into file:line frames with source context (stacktool)
//	errpattern  — Go error-handling anti-pattern scan (errpattern)
package codetool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/apitool"
	"github.com/damien1141/a1/internal/tools/buildtool"
	"github.com/damien1141/a1/internal/tools/coveragetool"
	"github.com/damien1141/a1/internal/tools/deadcode"
	"github.com/damien1141/a1/internal/tools/depstool"
	"github.com/damien1141/a1/internal/tools/errortool"
	"github.com/damien1141/a1/internal/tools/errortrans"
	"github.com/damien1141/a1/internal/tools/errpattern"
	"github.com/damien1141/a1/internal/tools/impacttool"
	"github.com/damien1141/a1/internal/tools/judgetool"
	"github.com/damien1141/a1/internal/tools/migrationtool"
	"github.com/damien1141/a1/internal/tools/nplusonetool"
	"github.com/damien1141/a1/internal/tools/propertytool"
	"github.com/damien1141/a1/internal/tools/runtimetool"
	"github.com/damien1141/a1/internal/tools/scaffoldtool"
	"github.com/damien1141/a1/internal/tools/stacktool"
	"github.com/damien1141/a1/internal/tools/testimpacttool"
	"github.com/damien1141/a1/internal/tools/tokentool"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/tools/vulntool"
)

var codeDescription = `Code analysis facade — ` + "`mode`" + ` picks the analysis; other args pass through verbatim.

Failure diagnosis — paste the text via ` + "`text`" + `:
  error match error text against known patterns for a fix (Go/Python/Rust/Node)
  errtrans translate compiler/shell errors to a diagnosis (more languages)
  stack parse a pasted trace into file:line frames + source context
  runtime inspect go test runtime failures (panics, deadlocks, leaks)
  Try error first, then errtrans.
Code facts (path/glob/limit): api exported surface | deadcode unused symbols |
  deps import graph | impact blast radius | errpattern Go error anti-patterns |
  nplusone N+1 queries | vuln govulncheck | token token-count
Tests/build: coverage | testimpact tests to run | property skeletons
Writes: scaffold templates | migration API migration
judge: classification by the decision model, without loading content into
  context. questions is a JSON object mapping names to definitions —
  {"todo": {"type": "noul", "criteria": {"true": "mentions a TODO/FIXME"}}}
  (types: noul yes/no, choice one-of list, score ordered levels best last) —
  then either state (text or JSON, judged once) or files (comma-separated
  globs, each judged independently). Answers carry confidence; report them
  as given.`

// codeModes is the list of accepted mode values. Listed here for the
// schema's enum field so the LLM gets a constrained choice instead of a
// free-form string.
var codeModes = []string{
	"api", "deadcode", "coverage", "impact", "deps", "nplusone",
	"testimpact", "vuln", "scaffold", "migration", "token", "property",
	"runtime", "build", "judge", "errtrans", "error", "stack", "errpattern",
}

// CodeTool returns the unified code-analysis tool definition + handler.
//
// The tool's schema advertises `mode` (enum) plus a small set of common
// params (`path`, `limit`, `glob`). Mode-specific params are not
// enumerated in the schema — they're forwarded verbatim via the raw
// json.RawMessage input. This keeps the schema small and stable; the
// underlying tools' own schemas remain available in their packages for
// anything that needs to introspect them.
func CodeTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "code",
			Description: codeDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"mode": llm.Object{
						"type":        "string",
						"description": "Which analysis to run. See tool description for the full list.",
						"enum":        codeModes,
					},
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or file to analyze (mode-dependent). Example: ./internal/tools",
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File pattern filter (mode-dependent). Example: *.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": "Maximum results to return (mode-dependent). Example: 50",
					},
				},
				Required: []string{"mode"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in codeInput
			_ = json.Unmarshal(input, &in)
			mode := strings.ToLower(strings.TrimSpace(in.Mode))
			p := strings.TrimSpace(in.Path)
			if p != "" {
				return mode + " " + p
			}
			// Text-keyed modes (error/stack/errtrans) carry no path; a
			// truncated paste of `text` identifies the failure in the
			// transcript instead of a bare mode name.
			if txt := strings.TrimSpace(in.Text); txt != "" {
				if len(txt) > 40 {
					txt = txt[:40] + "..."
				}
				return mode + " " + txt
			}
			return mode
		},
		Run: runCode,
	}
}

type codeInput struct {
	Mode  string `json:"mode"`
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
	Text  string `json:"text,omitempty"`
}

// codeHandlers maps each mode to the underlying tool's Run handler. Built
// once at init() from the public Tool() constructors so the dispatch is a
// map lookup at call time. init() avoids any package-var initialization
// ordering surprises.
var codeHandlers map[string]func(context.Context, json.RawMessage) (tooldef.Result, error)

func init() {
	codeHandlers = map[string]func(context.Context, json.RawMessage) (tooldef.Result, error){
		"api":        apitool.ApiTool().Run,
		"deadcode":   deadcode.DeadcodeTool().Run,
		"coverage":   coveragetool.CoverageTool().Run,
		"impact":     impacttool.ImpactTool().Run,
		"deps":       depstool.DepsTool().Run,
		"nplusone":   nplusonetool.NplusoneTool().Run,
		"testimpact": testimpacttool.TestImpactTool().Run,
		"vuln":       vulntool.VulnTool().Run,
		"scaffold":   scaffoldtool.ScaffoldTool().Run,
		"migration":  migrationtool.MigrationTool().Run,
		"token":      tokentool.TokenTool().Run,
		"property":   propertytool.PropertyTool().Run,
		"runtime":    runtimetool.RuntimeTool().Run,
		"build":      buildtool.BuildTool().Run,
		"judge":      judgetool.JudgeTool().Run,
		"errtrans":   errortrans.ErrtransTool().Run,
		"error":      errortool.ErrorTool().Run,
		"stack":      stacktool.StackTool().Run,
		"errpattern": errpattern.ErrPatternTool().Run,
	}
}

// runCode reads the mode field, looks up the underlying handler, and
// forwards the raw input unchanged. An unknown mode returns an error so
// the LLM gets feedback to re-pick.
func runCode(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in codeInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse code arguments: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		return tooldef.Result{}, fmt.Errorf("mode is required (one of: %s)", strings.Join(codeModes, ", "))
	}
	h, ok := codeHandlers[mode]
	if !ok {
		return tooldef.Result{}, fmt.Errorf("unknown code mode %q: use one of %s", mode, strings.Join(codeModes, ", "))
	}
	return h(ctx, input)
}
