package agent

import (
	"github.com/damien1141/a1/internal/extension"
	"github.com/damien1141/a1/internal/job"
	llmclient "github.com/damien1141/a1/internal/llm/client"
	"github.com/damien1141/a1/internal/mcp"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools"
)

// EngineOption configures optional NewEngine dependencies.
type EngineOption func(*engineConfig)

type engineConfig struct {
	gate         permission.Gate
	ask          permission.AskFunc
	continueAsk  ContinueFunc
	tools        []tools.Tool
	maxRounds    int
	jobs         *job.Manager
	extensions   *extension.Runner
	omitExtTools bool
	mcp          *mcp.Pool
	hooks        llmclient.Hooks
}

// WithGate sets the permission gate (nil = allow all).
func WithGate(gate permission.Gate) EngineOption {
	return func(c *engineConfig) { c.gate = gate }
}

// WithAsk sets the ask callback (nil = deny on Ask).
func WithAsk(ask permission.AskFunc) EngineOption {
	return func(c *engineConfig) { c.ask = ask }
}

// WithContinueAsk sets the max-rounds continuation prompt (nil = ErrMaxRounds).
func WithContinueAsk(fn ContinueFunc) EngineOption {
	return func(c *engineConfig) { c.continueAsk = fn }
}

// WithTools sets the base tool list (nil = tools.DefaultTools(); sub-agents use ChildTools()).
func WithTools(list []tools.Tool) EngineOption {
	return func(c *engineConfig) { c.tools = list }
}

// WithMaxRounds sets the tool-round budget (0 = package default).
func WithMaxRounds(n int) EngineOption {
	return func(c *engineConfig) { c.maxRounds = n }
}

// WithJobs registers agent_* tools against the given job manager.
func WithJobs(jobs *job.Manager) EngineOption {
	return func(c *engineConfig) { c.jobs = jobs }
}

// WithExtensions attaches an extension runner (nil = no extensions).
func WithExtensions(runner *extension.Runner) EngineOption {
	return func(c *engineConfig) { c.extensions = runner }
}

// WithOmitExtensionTools keeps Runner events but skips RegisterTool merge (sub-agents).
func WithOmitExtensionTools(omit bool) EngineOption {
	return func(c *engineConfig) { c.omitExtTools = omit }
}

// WithMCP registers mcp_list/inspect/call meta-tools against the pool.
func WithMCP(pool *mcp.Pool) EngineOption {
	return func(c *engineConfig) { c.mcp = pool }
}

// WithHooks sets provider request interceptors (nil fields = no customization).
func WithHooks(hooks llmclient.Hooks) EngineOption {
	return func(c *engineConfig) { c.hooks = hooks }
}
