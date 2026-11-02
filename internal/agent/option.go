package agent

import (
        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        llmclient "github.com/damien1141/a1/internal/llm/client"
        "github.com/damien1141/a1/internal/mcp"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/schedule"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// EngineOption configures optional NewEngine dependencies.
type EngineOption func(*engineConfig)

type engineConfig struct {
        gate         permission.Gate
        ask          permission.AskFunc
        question     tooldef.QuestionFunc
        continueAsk  ContinueFunc
        tools        []tools.Tool
        maxRounds    int
        maxToolCalls int
        jobs         *job.Manager
        scheduler    *schedule.Scheduler // Bet 01 Clock: registers the schedule tool when set
        agentDepth   int                // this engine's fleet depth (0 = top-level; Bet 04)
        extensions   *extension.Runner
        omitExtTools bool
        mcp          *mcp.Pool
        hooks        llmclient.Hooks
        memStore     *memstore.Store
        // memoryEnabled is the surface's raw memory toggle bit — NOT
        // persona-adjusted; the engine derives the effective value in
        // MemoryEnabled(). memoryEnabledSet distinguishes "unset" (default
        // true: engines never told about a toggle keep the pre-toggle
        // behavior) from an explicit false. The two surfaces set it
        // explicitly: web via WithMemoryEnabled in engineOptions, TUI via
        // WithMemoryEnabled at construction + SetMemoryEnabled on the
        // Ctrl+K toggle.
        memoryEnabled    bool
        memoryEnabledSet bool
        embedder         memstore.Embedder // for opening per-persona SQLite stores lazily
        windowProber     WindowProber
        // evidence overrides the engine's default evidence ledger. Nil = the
        // engine installs a session-persisted ledger automatically.
        evidence *evidence.Ledger
        // evidenceChecks are config-defined checks (config.yaml `checks:`)
        // installed on the default ledger; ignored when evidence is wired.
        evidenceChecks []evidence.CheckDef
        // stallLimit / stallLimitSet configure the evidence verdict gate's
        // anti-thrash limit (consecutive non-improving verification rounds
        // before a turn stops). Unset = defaultEvidenceStallLimit.
        stallLimit    int
        stallLimitSet bool
        // jevEvidence overrides the engine's JEV decision-model evidence
        // advisor. Nil = the config-gated constructor (nil with no decision
        // model configured — deterministic classifiers only).
        jevEvidence *jevEvidenceAdvisor
        // zeroRankBudget / zeroRankBudgetSet configure the Phase 3 zero-rank
        // stop: extra verification rounds bought when the model tries to end a
        // turn with declared contract checks not green. Unset =
        // defaultZeroRankBudget.
        zeroRankBudget    int
        zeroRankBudgetSet bool
        // budgetContracts / budgetContractsSet configure the Phase 5
        // declared-budget gate: whether a model-declared tool-round budget
        // (the `budget` tool) can stop the turn when it fires. Declarations
        // are recorded and echoed either way. Unset = enabled.
        budgetContracts    bool
        budgetContractsSet bool

        // Persona apply-on-switch hooks. Optional; when nil, the
        // corresponding persona setting is silently ignored on switch.
        personaModelResolver PersonaModelResolver
        permissionModeSetter PermissionModeSetter
        // basePermissionMode is the mode the surface ran before any persona
        // override. A switch to a persona WITHOUT a mode override (or to the
        // default persona) restores it, so a readonly persona's restriction
        // cannot leak into the next persona.
        basePermissionMode permission.Mode
}

// PersonaModelResolver looks up an llm.ModelConfig by name. Used by
// SetPersona when the active persona has a non-empty Model field: the
// resolved config replaces engine.modelCfg before rebindTools() rebuilds
// the LLM client, so the next turn routes to the persona's chosen model.
// Returns (zero, false) when the name is unknown — the engine keeps its
// previous modelCfg in that case (rather than silently breaking).
type PersonaModelResolver func(name string) (llm.ModelConfig, bool)

// PermissionModeSetter applies a permission mode change. Wired by the
// CLI to call the controller's SetPermissionMode, which rebuilds the
// gate with the new mode and preserves session state (ask timeout,
// APPA trajectory). Called by SetPersona when the active persona has a
// non-empty PermissionMode field.
type PermissionModeSetter func(mode permission.Mode)

// WithGate sets the permission gate (nil = allow all).
func WithGate(gate permission.Gate) EngineOption {
        return func(c *engineConfig) { c.gate = gate }
}

// WithQuestion sets the AskUserQuestion handler. The tool dispatches through
// this, so it needs the same TUI-backed asker the permission gate uses.
func WithQuestion(fn tooldef.QuestionFunc) EngineOption {
        return func(o *engineConfig) { o.question = fn }
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

// WithMaxToolCalls sets the per-turn tool-call budget (0 = unlimited).
// Unlike the round budget this counts individual executed calls, so a round
// that batches several calls consumes several budget slots. Yolo permission
// mode ignores the cap entirely.
func WithMaxToolCalls(n int) EngineOption {
        return func(c *engineConfig) { c.maxToolCalls = n }
}

// WithJobs registers agent_* tools against the given job manager.
func WithJobs(jobs *job.Manager) EngineOption {
        return func(c *engineConfig) { c.jobs = jobs }
}

// WithScheduler registers the schedule tool (Bet 01 Clock) against the
// given scheduler. Fires submit prompts through the scheduler's host
// callback — in practice a job-manager spawn, so wake-ups reuse the
// fleet's backpressure and run headless-strict by default.
func WithScheduler(s *schedule.Scheduler) EngineOption {
        return func(c *engineConfig) { c.scheduler = s }
}

// WithAgentDepth sets this engine's depth in the agent fleet (0 = top-level
// parent). The depth flows into its spawns so per-depth step budgets and
// the nesting ceiling hold (Bet 04).
func WithAgentDepth(depth int) EngineOption {
        return func(c *engineConfig) { c.agentDepth = depth }
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

// WithMemoryStore attaches the agent memory system. When non-nil, the
// engine registers remember/recall/revise/forget tools and injects
// recalled memories into the per-turn context. When nil (default),
// no memory tools are registered and no retrieval happens.
func WithMemoryStore(store *memstore.Store) EngineOption {
        return func(c *engineConfig) { c.memStore = store }
}

// WithMemoryEnabled sets the surface's raw memory toggle — the global knob
// the settings palette / web settings panel flips, BEFORE any persona
// override is applied. The effective state the engine runs with is
// MemoryEnabled(): the ACTIVE persona's explicit memory_enabled wins when
// set, otherwise this toggle decides. Default (option absent) is ON, so
// engines that predate the toggle (sub-agents, embedders, tests) keep
// their behavior; both real surfaces pass it explicitly.
func WithMemoryEnabled(enabled bool) EngineOption {
        return func(c *engineConfig) {
                c.memoryEnabled = enabled
                c.memoryEnabledSet = true
        }
}

// WithEmbedder attaches the embedding backend used to open per-persona
// semantic memory stores (one SQLite file per persona at
// <persona memoryDir>/semantic/<id>.sqlite). When nil, per-persona
// semantic memory auto-injection + auto-consolidation are disabled; the
// explicit remember/recall tools still work against engine.memStore if
// it's set.
//
// Typical wiring: the CLI constructs the embedder via
// memstore.TryOllamaFromConfig(baseURL, model) using the project's
// semantic_search config, and passes it here. When Ollama is unreachable
// or unconfigured, pass memstore.FakeEmbedder{DimCount: 64} for offline
// tests — the activation math still works, just with weaker semantic
// matching.
func WithEmbedder(emb memstore.Embedder) EngineOption {
        return func(c *engineConfig) { c.embedder = emb }
}

// WithPersonaModelResolver wires the model-name → ModelConfig lookup used
// when SetPersona switches to a persona that has a non-empty Model field.
// The CLI typically passes:
//
//      agent.WithPersonaModelResolver(func(name string) (llm.ModelConfig, bool) {
//          if proj == nil || proj.Config() == nil { return llm.ModelConfig{}, false }
//          return proj.Config().FindModel(name)
//      })
//
// When nil (default), a persona with a Model field set has no effect —
// the engine keeps its current model. This is the safe default: a
// missing resolver shouldn't silently break a persona switch.
func WithPersonaModelResolver(fn PersonaModelResolver) EngineOption {
        return func(c *engineConfig) { c.personaModelResolver = fn }
}

// WithPermissionModeSetter wires the permission-mode setter used when
// SetPersona switches to a persona that has a non-empty PermissionMode
// field. The CLI typically passes:
//
//      agent.WithPermissionModeSetter(func(mode permission.Mode) {
//          ctrl.SetPermissionMode(mode) // existing controller method
//      })
//
// When nil (default), a persona with a PermissionMode field set has no
// effect — the gate keeps its current mode. This is the safe default
// for tests that don't wire a real gate.
func WithPermissionModeSetter(fn PermissionModeSetter) EngineOption {
        return func(c *engineConfig) { c.permissionModeSetter = fn }
}

// WithBasePermissionMode records the mode the surface ran before any
// persona override (the TUI passes its live gate's mode; the web server
// passes the mode store's current mode). SetPersona restores it when the
// target persona has no PermissionMode of its own — including when the
// target is the default persona — so persona mode overrides are scoped to
// exactly the persona that declared them.
func WithBasePermissionMode(mode permission.Mode) EngineOption {
        return func(c *engineConfig) { c.basePermissionMode = mode }
}

// WithEvidenceLedger overrides the engine's evidence ledger. By default the
// engine installs a ledger whose receipts persist next to the session file
// (memory-only for in-memory sessions). Callers who want receipts elsewhere
// (or a shared ledger across engines) pass their own.
func WithEvidenceLedger(l *evidence.Ledger) EngineOption {
        return func(c *engineConfig) { c.evidence = l }
}

// WithEvidenceChecks installs config-defined checks (config.yaml `checks:`)
// on the engine's default evidence ledger. Each completed run of a defined
// command records `check:<name>` evidence — the permanent, config-owned
// variant of the contract's watched exit checks. Ignored when the ledger
// is wired explicitly via WithEvidenceLedger.
func WithEvidenceChecks(defs []evidence.CheckDef) EngineOption {
        return func(c *engineConfig) { c.evidenceChecks = defs }
}

// WithStallLimit sets the evidence verdict gate's anti-thrash limit: how
// many consecutive non-improving verification rounds (candidates that
// regressed something protected, or produced no new evidence) a turn
// tolerates before it stops and surfaces the residual failure set.
// n <= 0 disables the stall stop. Default: 3.
func WithStallLimit(n int) EngineOption {
        return func(c *engineConfig) { c.stallLimit = n; c.stallLimitSet = true }
}

// WithJevEvidenceAdvisor wires the JEV decision-model evidence advisor
// (see jev_evidence.go). By default the engine builds the config-gated
// advisor: present only when a decision model is configured (the jev:
// section of config.yaml — api or ollama — or the TYPESAFE_API_KEY
// fallback), and consulted only for check-like commands the deterministic
// classifiers do not recognize. A nil override restores the
// deterministic-only behavior.
func WithJevEvidenceAdvisor(a *jevEvidenceAdvisor) EngineOption {
        return func(c *engineConfig) { c.jevEvidence = a }
}

// WithZeroRankBudget sets the Phase 3 zero-rank stop's verification budget:
// how many extra rounds the harness buys when the model tries to end a turn
// whose declared contract checks are not all green, injecting the residual
// failure set each time. n <= 0 disables the zero-rank stop (the model's
// done stands). Default: 2.
func WithZeroRankBudget(n int) EngineOption {
        return func(c *engineConfig) { c.zeroRankBudget = n; c.zeroRankBudgetSet = true }
}

// WithBudgetContracts toggles enforcement of model-declared tool-round
// budgets (the `budget` tool, Phase 5). With it off, declarations are still
// recorded and echoed in the transcript, but a firing never stops the turn —
// useful for surfaces that want the visibility without the binding. Default:
// enabled.
func WithBudgetContracts(enabled bool) EngineOption {
        return func(c *engineConfig) { c.budgetContracts = enabled; c.budgetContractsSet = true }
}

// WithWindowProber overrides how the engine discovers an unknown context
// window (config → probe → assumed fallback). Production uses the llama.cpp
// /props and ollama /api/show probe; tests inject a deterministic stub so
// no engine test touches the network.
func WithWindowProber(fn WindowProber) EngineOption {
        return func(c *engineConfig) { c.windowProber = fn }
}
