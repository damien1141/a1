package tools

import (
	"github.com/damien1141/a1/internal/tools/agenttool"
	"github.com/damien1141/a1/internal/tools/apidoc"
	"github.com/damien1141/a1/internal/tools/apitool"
	"github.com/damien1141/a1/internal/tools/bashtool"
	"github.com/damien1141/a1/internal/tools/batchtool"
	"github.com/damien1141/a1/internal/tools/buildtool"
	"github.com/damien1141/a1/internal/tools/contexttool"
	"github.com/damien1141/a1/internal/tools/coveragetool"
	"github.com/damien1141/a1/internal/tools/deadcode"
	"github.com/damien1141/a1/internal/tools/depstool"
	"github.com/damien1141/a1/internal/tools/doctool"
	"github.com/damien1141/a1/internal/tools/errortool"
	"github.com/damien1141/a1/internal/tools/errortrans"
	"github.com/damien1141/a1/internal/tools/errpattern"
	"github.com/damien1141/a1/internal/tools/findtool"
	"github.com/damien1141/a1/internal/tools/gittool"
	"github.com/damien1141/a1/internal/tools/graphtool"
	"github.com/damien1141/a1/internal/tools/greptool"
	"github.com/damien1141/a1/internal/tools/impacttool"
	"github.com/damien1141/a1/internal/tools/journaltool"
	"github.com/damien1141/a1/internal/tools/lstool"
	"github.com/damien1141/a1/internal/tools/mcptool"
	"github.com/damien1141/a1/internal/tools/migrationtool"
	"github.com/damien1141/a1/internal/tools/nplusonetool"
	"github.com/damien1141/a1/internal/tools/propertytool"
	"github.com/damien1141/a1/internal/tools/ranktool"
	"github.com/damien1141/a1/internal/tools/readtool"
	"github.com/damien1141/a1/internal/tools/scaffoldtool"
	"github.com/damien1141/a1/internal/tools/secrettool"
	"github.com/damien1141/a1/internal/tools/stacktool"
	"github.com/damien1141/a1/internal/tools/testtool"
	"github.com/damien1141/a1/internal/tools/todotool"
	"github.com/damien1141/a1/internal/tools/tokentool"
	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/tools/vectortool"
	"github.com/damien1141/a1/internal/tools/vulntool"
	"github.com/damien1141/a1/internal/tools/watchertool"
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
func DefaultTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		readtool.ReadTool(),
		writetool.WriteTool(),
		greptool.GrepTool(),
		lstool.LsTool(),
		writetool.EditTool(),
		findtool.FindTool(),
		gittool.GitTool(),
		todotool.TodoTool(),
		watchertool.WatcherTool(),
		stacktool.StackTool(),
		testtool.TestTool(),
		secrettool.SecretTool(),
		errortool.ErrorTool(),
		ranktool.RankTool(),
		apitool.ApiTool(),
		deadcode.DeadcodeTool(),
		coveragetool.CoverageTool(),
		impacttool.ImpactTool(),
		depstool.DepsTool(),
		migrationtool.MigrationTool(),
		propertytool.PropertyTool(),
		doctool.DocTool(),
		nplusonetool.NplusoneTool(),
	apidoc.ApidocTool(),
		vulntool.VulnTool(),
		errpattern.ErrPatternTool(),
		scaffoldtool.ScaffoldTool(),
		buildtool.BuildTool(),
		tokentool.TokenTool(),
		errortrans.ErrtransTool(),
		contexttool.ContextTool(),
		journaltool.JournalTool(),
		vectortool.VectorTool(),
		graphtool.GraphTool(),
		batchtool.BatchTool(),
	}
}

// ReadonlyTools returns exploration tools without write/edit.
// Bash remains registered; pair with ModeReadonly (and typically
// ChildPolicy) so write/edit stay denied while non-deny bash is allowed.
func ReadonlyTools() []Tool {
	return []Tool{
		bashtool.BashTool(),
		readtool.ReadTool(),
		greptool.GrepTool(),
		lstool.LsTool(),
		findtool.FindTool(),
		gittool.GitTool(),
		todotool.TodoTool(),
		watchertool.WatcherTool(),
		stacktool.StackTool(),
		testtool.TestTool(),
		secrettool.SecretTool(),
		errortool.ErrorTool(),
		ranktool.RankTool(),
		apitool.ApiTool(),
		deadcode.DeadcodeTool(),
		coveragetool.CoverageTool(),
		impacttool.ImpactTool(),
		depstool.DepsTool(),
		migrationtool.MigrationTool(),
		propertytool.PropertyTool(),
		doctool.DocTool(),
		nplusonetool.NplusoneTool(),
	apidoc.ApidocTool(),
		vulntool.VulnTool(),
		errpattern.ErrPatternTool(),
		scaffoldtool.ScaffoldTool(),
		buildtool.BuildTool(),
		tokentool.TokenTool(),
		errortrans.ErrtransTool(),
		contexttool.ContextTool(),
		journaltool.JournalTool(),
		vectortool.VectorTool(),
		graphtool.GraphTool(),
		batchtool.BatchTool(),
	}
}
