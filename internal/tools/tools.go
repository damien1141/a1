package tools

import (
	"github.com/damien1141/a1/internal/tools/agenttool"
	"github.com/damien1141/a1/internal/tools/bashtool"
	"github.com/damien1141/a1/internal/tools/browsertool"
	"github.com/damien1141/a1/internal/tools/codetool"
	"github.com/damien1141/a1/internal/tools/contexttool"
	"github.com/damien1141/a1/internal/tools/doctool"
	"github.com/damien1141/a1/internal/tools/fetchtool"
	"github.com/damien1141/a1/internal/tools/gittool"
	"github.com/damien1141/a1/internal/tools/kamitool"
	"github.com/damien1141/a1/internal/tools/mcptool"
	"github.com/damien1141/a1/internal/tools/permissiontool"
	"github.com/damien1141/a1/internal/tools/questiontool"
	"github.com/damien1141/a1/internal/tools/readtool"
	"github.com/damien1141/a1/internal/tools/scripttool"
	"github.com/damien1141/a1/internal/tools/searchtool"
	"github.com/damien1141/a1/internal/tools/secrettool"
	"github.com/damien1141/a1/internal/tools/testtool"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/tools/vectortool"
	"github.com/damien1141/a1/internal/tools/writetool"
)

type (
	// Result re-exports tooldef.Result.
	Result = tooldef.Result
	// Handler re-exports tooldef.Handler.
	Handler = tooldef.Handler
	// Tool re-exports tooldef.Tool.
	Tool = tooldef.Tool
	// Registry re-exports tooldef.Registry.
	Registry = tooldef.Registry
)

// Definitions and the registry helpers are re-exported from tooldef.
var (
	Definitions    = tooldef.Definitions
	NewRegistry    = tooldef.NewRegistry
	WithToolCallID = tooldef.WithToolCallID
	ToolCallID     = tooldef.ToolCallID
	WithCwd        = tooldef.WithCwd
)

type (
	// ShellExecResult re-exports bashtool.ShellExecResult.
	ShellExecResult = bashtool.ShellExecResult
	// ShellExecOptions re-exports bashtool.ShellExecOptions.
	ShellExecOptions = bashtool.ShellExecOptions
	// BashOutputTail re-exports bashtool.BashOutputTail.
	BashOutputTail = bashtool.BashOutputTail
)

// Bash output limits are re-exported from bashtool.
const (
	BashMaxOutputLines = bashtool.BashMaxOutputLines
	BashMaxOutputBytes = bashtool.BashMaxOutputBytes
)

// ExecShell and NewBashOutputTail are re-exported from bashtool.
var (
	ExecShell         = bashtool.ExecShell
	NewBashOutputTail = bashtool.NewBashOutputTail
)

type (
	// AgentDeps re-exports agenttool.AgentDeps.
	AgentDeps = agenttool.AgentDeps
	// AgentResult re-exports agenttool.AgentResult.
	AgentResult = agenttool.AgentResult
)

// AgentTools, ParseAgentResult, and MCPTools are re-exported tool helpers.
var (
	AgentTools       = agenttool.AgentTools
	ParseAgentResult = agenttool.ParseAgentResult
	MCPTools         = mcptool.Tools
)

// DefaultTools returns the built-in agent tool set.
//
// Tool count target: ≤15. Previously this list had 45 entries — many were
// near-identical code-analysis tools that overwhelmed the LLM's tool-
// selection head. The collapse strategy:
//   - `write` folds edit (mode=write|edit)
//   - `search` folds grep / find / graph / rank (mode=auto|text|symbol|file|grep|find|graph|rank)
//   - `read`   folds ls / watcher (mode=file|ls|watch)
//   - `docsync` folds apidoc (mode=sync|api)
//   - `context` folds journal / batch / snapshot / configvalidator (mode=fold|journal|batch|snapshot|config)
//   - `code` is new — folds 19 code-analysis tools (mode=api|deadcode|coverage|impact|deps|nplusone|testimpact|vuln|scaffold|migration|token|property|runtime|build|judge|errtrans|error|stack|errpattern)
//   - `git` is the governed git/gh facade (Phase 1 close-out, roadmap-
//     sanctioned addition): one tool, 12 modes, APPA-gated per mode in
//     permission/extract.go so reads never prompt while commit/push/pull
//     always ask — the lean replacement for raw-bash git calls.
//
// The underlying tool packages stay in place as dispatch targets. Source
// packages for tools that were only registered (e.g. errortrans, deadcode)
// stay too — they keep their own tests and remain usable through `code`.
func DefaultTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		// script is the programmatic-tool-calling surface: one Python
		// script that calls real a1 tools as functions over a loopback
		// RPC, with only the final stdout entering context. Every
		// in-script call traverses the executor's normal permission
		// pipeline — the collapse saves context, never safety.
		scripttool.ScriptTool(),
		// git before bash-adjacent fallbacks: the model should reach for the
		// governed facade, not a shell string, for git work.
		gittool.GitTool(),
		readtool.ReadTool(),
		writetool.WriteTool(),
		searchtool.SearchTool(),
		questiontool.QuestionTool(),
		testtool.TestTool(),
		secrettool.SecretTool(),
		fetchtool.FetchTool(),
		browsertool.BrowserTool(),
		// Atomic browser tools — typed schemas for the common
		// navigate / interact / read path. LLMs reliably fill
		// typed fields; they struggle with the legacy `action`
		// string discriminator. The legacy `browser` tool stays
		// for profile/proxy/close (operator actions the model
		// rarely needs).
		browsertool.BrowserNavigateTool(),
		browsertool.BrowserInteractTool(),
		browsertool.BrowserReadTool(),
		contexttool.ContextTool(),
		vectortool.VectorTool(),
		// code is the unified code-analysis tool — 19 modes
		// (api/deadcode/coverage/impact/deps/nplusone/testimpact/
		// vuln/scaffold/migration/token/property/runtime/build/
		// judge/errtrans/error/stack/errpattern) dispatched by the
		// `mode` field. Replaces 19 separate tool registrations
		// with one.
		codetool.CodeTool(),
		// doctool is the unified documentation tool — modes sync
		// (default, doc-vs-code drift) and api (apidoc stubs).
		doctool.DocTool(),
		// kamitool: unified Kami-styled document generation.
		// One tool, one enum, markdown content. Replaces the
		// old 3-tool proposal/resume/onepager approach that
		// confused the LLM with overlapping schemas.
		kamitool.DocumentTool(),
		// scratchpadtool removed — memory bank (session.MemoryBank()) is the
		// single note store for the agent, wired into the executor. Keeping
		// both meant the model had two places to write the same kind of note
		// and they diverged.
		permissiontool.PermissionTool(),
	}
}

// ReadonlyTools returns exploration tools without write/edit.
// Bash remains registered; pair with ModeReadonly (and typically
// ChildPolicy) so write/edit stay denied while non-deny bash is allowed.
//
// Mirrors DefaultTools minus the write/edit-capable tools: write (write/
// edit modes both touch files), test (runs binaries), build (mode=build
// inside `code` runs builds — but `code` is read-only in all other modes
// so it stays). `git` stays for the same reason `bash` does: its read
// modes are ActionRead, and the mutating modes (add/restore/commit/push/…)
// are ActionWrite/ActionBash, which ModeReadonly folds to Deny — the gate,
// not the registration list, is the confinement.
func ReadonlyTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		gittool.GitTool(),
		readtool.ReadTool(),
		searchtool.SearchTool(),
		questiontool.QuestionTool(),
		testtool.TestTool(),
		secrettool.SecretTool(),
		fetchtool.FetchTool(),
		browsertool.BrowserTool(),
		browsertool.BrowserNavigateTool(),
		browsertool.BrowserInteractTool(),
		browsertool.BrowserReadTool(),
		contexttool.ContextTool(),
		vectortool.VectorTool(),
		codetool.CodeTool(),
		doctool.DocTool(),
		kamitool.DocumentTool(),
		permissiontool.PermissionTool(),
	}
}
