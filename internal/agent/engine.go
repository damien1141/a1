package agent

import (
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "iter"
        "path/filepath"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        "github.com/damien1141/a1/internal/agent/prompt"
        "github.com/damien1141/a1/internal/contextbreakdown"
        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/engineaccess"
        "github.com/damien1141/a1/internal/evidence"
        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        llmclient "github.com/damien1141/a1/internal/llm/client"
        "github.com/damien1141/a1/internal/llm/skills"
        "github.com/damien1141/a1/internal/mcp"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/project/model"
        "github.com/damien1141/a1/internal/schedule"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/session/compaction"
        "github.com/damien1141/a1/internal/snapshot"
        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/memtool"
        "github.com/damien1141/a1/internal/tools/scheduletool"
        "github.com/damien1141/a1/internal/tools/skilltool"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// ErrMaxRounds is returned (wrapped) by Loop when the model exceeds the
// configured tool-round budget and continuation is declined or unavailable.
// Callers can distinguish it from other runtime errors with errors.Is,
// e.g. for a dedicated exit code.
var ErrMaxRounds = errors.New("exceeded maximum tool rounds")

// ActiveEngine is the currently running agent engine, if any. It is set by
// the TUI controller when a loop starts and cleared when it stops. Tools use
// ActiveEngine() to inspect or mutate session-local permission state.
var activeEngine atomic.Value

// SetActiveEngine registers the active engine for tool/harness access.
// Pass nil to clear.
func SetActiveEngine(engine *Engine) {
        activeEngine.Store(engine)
        engineaccess.SetActiveEngine(engine)
}

// ActiveEngine returns the currently registered engine, or nil.
func ActiveEngine() *Engine {
        v := activeEngine.Load()
        if v == nil {
                return nil
        }
        return v.(*Engine)
}

// Gate returns the current permission gate.
func (engine *Engine) Gate() permission.Gate {
        if engine == nil {
                return nil
        }
        return engine.gate
}

const defaultMaxToolRounds = 64

// ContinueFunc asks whether to grant another maxRounds budget after the
// current budget is exhausted. Nil means hard-fail with ErrMaxRounds
// (headless / sub-agent default). True continues the loop with a fresh budget.
type ContinueFunc func(ctx context.Context, maxRounds int) (bool, error)

// Engine drives the agent loop: stream → tools → stream…
// and yields session.Event for the TUI reducer. Context compaction is owned
// here so Session stays a thin message store.
type Engine struct {
        client    *llmclient.Client
        executor  *Executor
        maxRounds int
        // evidence is the engine's protected evidence ledger (VeriLoop-Governed
        // Recurrence). It records deterministic check results (build/vet/tests)
        // observed by the harness from bash runs; the model can never write it.
        // Nil never happens in production — NewEngine always installs one — but
        // the executor treats a nil ledger as "evidence disabled".
        evidence *evidence.Ledger
        // evidenceSnaps backs the per-turn verdict gate (Phase 2): pre-candidate
        // baselines and enforced rollbacks. Lazily built from the session cwd;
        // tests inject a fake via setEvidenceSnapshots.
        evidenceSnaps evidenceSnapshots
        // evidenceBaseline names the engine-owned baseline snapshot. It survives
        // turns so the next turn's gate deletes the ref once superseded — engine
        // baselines never accumulate in refs/a1/snapshots.
        evidenceBaseline string
        // stallLimit caps consecutive non-improving verification rounds before
        // the verdict gate stops the turn (0 = disabled). Atomic to mirror
        // maxToolCalls — the surface can retune it while a loop runs.
        stallLimit atomic.Int64
        // zeroRankBudget bounds the extra verification rounds bought when the
        // model tries to end a turn with declared contract checks not green
        // (0 = disabled: the model's done stands). Atomic to mirror stallLimit.
        zeroRankBudget atomic.Int64
        // turnContract is the turn's declared done-criteria (Phase 3): check
        // ids the model proposes via the `contract` tool and the harness
        // judges against the evidence ledger at turn end (zero-rank stop).
        // Reset at every Loop entry; ratcheted within a turn.
        turnContract turnContractState
        // sessionValence is the session's mood (Bet 06), computed at each
        // turn's end from outcomes (regressions pull down, certificates
        // pull up) and fed to the next retrieval's mood-congruency cue.
        // Atomic.Value: the outlet goroutine reads it while the loop
        // writes it.
        sessionValence atomic.Value // float64
        // memfsckOnce guards the session-start MEMORY.md reconciliation
        // (Bet 06).
        memfsckOnce sync.Once
        // taskContract is the session-scoped contract (Bet 04 task contracts):
        // everything declared this session plus anything inherited from a
        // parent fleet's finish line. Never reset — the add-only ratchet holds
        // across turns and Loops, and spawns pass it to children so a fleet
        // shares one finish line.
        taskContract turnContractState
        // contractSeed / contractSeedEnforce hold a fleet-inherited check set
        // (Bet 04). The seed survives per-Loop resets — every turn of this
        // engine starts with the inherited checks either enforced (worker
        // children: the zero-rank stop holds them to the parent's finish line)
        // or watch-only (explore/review: evidence records, no stop).
        contractSeed        []string
        contractSeedEnforce bool
        // agentDepth is this engine's depth in the agent fleet (0 = top-level
        // parent). Flows into spawns so per-depth budgets and the nesting
        // ceiling hold (Bet 04).
        agentDepth int
        // turnBudget is the turn's declared tool-round budget (Phase 5): an
        // absolute ceiling the model proposes via the `budget` tool and the
        // harness holds — reaching it with tools still wanted runs one
        // re-open round (extend or finish), and a further overrun stops the
        // turn with a spend report. Zero unless declared, so plain chat
        // turns are untouched. Reset at every Loop entry.
        turnBudget turnBudgetState
        // toolRoundsLive publishes the loop's tool-round counter to the
        // budget tool handler, which runs inside executor.Run and cannot
        // see Loop locals. Stored after every round increment; reset at
        // Loop entry.
        toolRoundsLive atomic.Int64
        // budgetContracts gates enforcement of declared budgets (Phase 5):
        // with it off, declarations are recorded and echoed but never stop
        // the turn. Default on — a declared budget only ever binds the
        // model that declared it, and extending is always one visible tool
        // call away.
        budgetContracts atomic.Bool
        // governed is the session's coding-mode latch: the evidence surfaces
        // only go live once the session has done something code-shaped. It
        // flips on the first real contract declaration (the check-id grammar
        // is only expressible for build/test work — a chat task has no
        // finish line in that vocabulary) and never clears: later turns in
        // the same session keep recording and rollback protection. A session
        // that never declares leaves no evidence trace at all, so an
        // incidental build command in a chat turn neither grows the ledger
        // nor arms regression notes for later casual turns. IsGoverned
        // additionally reads the ledger live, so a resumed session (receipts
        // replayed by Load) or a directly seeded one is governed without an
        // explicit declaration. One thin edge is accepted by design: a
        // session that declared a contract but never ran a single check
        // resumes ungoverned — it fails in the safe direction (under-
        // govern, never over-govern).
        governed atomic.Bool
        // maxToolCalls caps executed tool calls per turn (0 = unlimited). Atomic
        // because the TUI can raise or lower it while a loop is running; the
        // loop reads it fresh at every tool round. Yolo permission mode ignores
        // the cap entirely.
        maxToolCalls atomic.Int64
        modelCfg     llm.ModelConfig
        // baseModelCfg is the model the engine was constructed with. A
        // persona switch to a persona without a Model override (or to the
        // default persona) restores it, mirroring basePermissionMode.
        baseModelCfg llm.ModelConfig
        // basePermissionMode is the mode the surface ran before any persona
        // override; restored when the next persona declares none.
        basePermissionMode permission.Mode
        hooks              llmclient.Hooks
        // baseHooks is the hooks set the engine was constructed with (the
        // surface wires model.HooksFor(mainModel)). A persona model swap
        // replaces hooks with the override model's preset; reverting to the
        // base model restores these — vendor-specific wire shapes must
        // follow the model actually in use, not the one the surface booted
        // with.
        baseHooks           llmclient.Hooks
        gate               permission.Gate
        ask                permission.AskFunc
        question           tooldef.QuestionFunc
        continueAsk        ContinueFunc
        jobs               *job.Manager
        scheduler          *schedule.Scheduler // nil = no schedule tool (Bet 01 Clock)
        extensions         *extension.Runner // nil = disabled; all methods are nil-safe no-ops
        baseTools          []tools.Tool      // nil = DefaultTools; preserved across rebind
        omitExtTools       bool              // sub-agents: emit events but skip RegisterTool merge
        mcp                *mcp.Pool
        memStore           *memstore.Store   // nil = no shared bank; remember/recall/revise/forget registered only when non-nil AND memory effective-on
        embedder           memstore.Embedder // for opening per-persona SQLite stores lazily; nil = fall back to engine.memStore
        // memoryEnabled is the surface's raw memory toggle (see
        // WithMemoryEnabled). Atomic so the palette/web toggle can flip it
        // while a loop runs; MemoryEnabled() derives the effective value
        // from it + the active persona's override.
        memoryEnabled atomic.Bool
        // memoryInletMsg holds the per-turn semantic memory injection
        // built by injectSemanticMemories(). It's a transient system
        // message that gets prepended before the latest user message
        // when BuildContext() is called for the next LLM turn. The
        // message is NOT persisted to the session.
        memoryInletMsg *llm.Message
        // Persona apply-on-switch hooks. Optional; when nil, the
        // corresponding persona setting is silently ignored on switch.
        personaModelResolver PersonaModelResolver
        permissionModeSetter PermissionModeSetter

        session *Session
        // readCache backs the executor's read elision (see readcache.go):
        // last full file-mode read per resolved path, keyed with the tool
        // call whose message carries the content. Guarded by readCacheMu
        // because read-only tool batches run concurrently. In-memory only —
        // a restarted session starts cold, which is merely a missed elision.
        readCacheMu sync.Mutex
        readCache   map[string]readCacheEntry
        // lastCompactEstimate is the expected context size right after the last
        // compaction, used by the growth gate (billion-context §3.4). Compaction
        // shrinks the context sharply, so gating on post-compaction size measures
        // the consumed increment since then rather than the absolute size.
        lastCompactEstimate int
        // compactMu serializes compaction: the loop's post-turn auto path and
        // a surface's ForceCompact (web /compact, TUI :compact) run on
        // different goroutines, and two preparations against the same leaf
        // would append two compaction entries — the second summarizing a
        // zone the first already folded.
        compactMu sync.Mutex
        // jevAdv is the optional JEV decision-model advisor for evidence
        // admission (nil = deterministic classifiers only). See
        // jev_evidence.go.
        jevAdv *jevEvidenceAdvisor

        // Runtime context-window resolution (see engine_window.go and
        // doc/context-folding-math.md §4). ctxWindow is the effective window the
        // compaction triggers use — atomic because the TUI footer reads it live
        // while a loop may still be resolving. windowResolved gates the one-time
        // probe/assume. windowProber is injectable for tests.
        ctxWindow      atomic.Int64
        windowResolved atomic.Bool
        windowProber   WindowProber
}

// NewEngine wires an LLM client, tool executor, and an externally created session.
func NewEngine(model llm.ModelConfig, sess *Session, opts ...EngineOption) (*Engine, error) {
        if sess == nil {
                return nil, errors.New("agent: session is required")
        }
        var cfg engineConfig
        for _, opt := range opts {
                opt(&cfg)
        }
        engine := &Engine{
                maxRounds:            defaultMaxToolRounds,
                modelCfg:             model,
                baseModelCfg:         model,
                hooks:                cfg.hooks,
                baseHooks:            cfg.hooks,
                session:              sess,
                gate:                 cfg.gate,
                ask:                  cfg.ask,
                question:             cfg.question,
                continueAsk:          cfg.continueAsk,
                jobs:                 cfg.jobs,
                scheduler:            cfg.scheduler,
                agentDepth:           cfg.agentDepth,
                extensions:           cfg.extensions,
                omitExtTools:         cfg.omitExtTools,
                mcp:                  cfg.mcp,
                memStore:             cfg.memStore,
                embedder:             cfg.embedder,
                personaModelResolver: cfg.personaModelResolver,
                permissionModeSetter: cfg.permissionModeSetter,
                basePermissionMode:   cfg.basePermissionMode,
                evidence:             defaultEvidenceLedger(sess, cfg.evidence, cfg.evidenceChecks),
        }
        // Raw surface toggle: default ON when the surface never set it —
        // engines that predate the toggle keep their behavior. The two
        // real surfaces always pass WithMemoryEnabled explicitly.
        engine.memoryEnabled.Store(!cfg.memoryEnabledSet || cfg.memoryEnabled)
        if cfg.maxRounds > 0 {
                engine.maxRounds = cfg.maxRounds
        }
        if cfg.maxToolCalls > 0 {
                engine.maxToolCalls.Store(int64(cfg.maxToolCalls))
        }
        engine.stallLimit.Store(defaultEvidenceStallLimit)
        if cfg.stallLimitSet {
                engine.stallLimit.Store(int64(max(cfg.stallLimit, 0)))
        }
        engine.zeroRankBudget.Store(defaultZeroRankBudget)
        if cfg.zeroRankBudgetSet {
                engine.zeroRankBudget.Store(int64(max(cfg.zeroRankBudget, 0)))
        }
        engine.budgetContracts.Store(true)
        if cfg.budgetContractsSet {
                engine.budgetContracts.Store(cfg.budgetContracts)
        }
        if cfg.windowProber != nil {
                engine.windowProber = cfg.windowProber
        }
        // JEV evidence advisor: explicit wiring wins (tests); otherwise the
        // config-gated constructor — nil with no decision model configured
        // (jev: section or TYPESAFE_API_KEY), so the default engine is
        // byte-for-byte the pre-JEV behavior.
        engine.jevAdv = cfg.jevEvidence
        if engine.jevAdv == nil {
                engine.jevAdv = newJevEvidenceAdvisor()
        }
        engine.resetWindowForModel(model)
        engine.baseTools = cfg.tools
        engine.extensions.SetBaseTools(engine.buildCoreTools(engine.baseTools))
        engine.extensions.SetMeta(engine.SessionID(), engine.SessionCwd())
        toolList := engine.buildToolList(engine.baseTools)
        engine.client = llmclient.NewClient(model, engine.hooks, tools.Definitions(toolList), engine.systemPrompt())
        engine.bindExecutor(tools.NewRegistry(toolList))
        return engine, nil
}

func (engine *Engine) buildToolList(base []tools.Tool) []tools.Tool {
        out := engine.buildCoreTools(base)
        if !engine.omitExtTools {
                if extTools := engine.extensions.ExtensionTools(); len(extTools) > 0 {
                        merged := make([]tools.Tool, 0, len(out)+len(extTools))
                        merged = append(merged, out...)
                        merged = append(merged, extTools...)
                        return merged
                }
        }
        return out
}

// buildCoreTools returns builtin (+ MCP + agent_* + memory) tools without extension RegisterTool.
func (engine *Engine) buildCoreTools(base []tools.Tool) []tools.Tool {
        if base == nil {
                base = tools.DefaultTools()
        }
        out := base
        // contract tool: the model's declaration surface for machine-checked
        // done-criteria (Phase 3 zero-rank stop). Registered unconditionally —
        // a contract can be declared even in a fresh workspace with no
        // evidence yet; the residual judgment handles no-evidence checks.
        out = append(out, engine.contractTool())
        // budget tool: the model's declaration surface for tool-round budgets
        // (Phase 5 budget contracting). Registered unconditionally — an
        // undeclared budget simply never binds, so plain chat turns are
        // untouched.
        out = append(out, engine.budgetTool())
        // list_skills tool: lets the model discover available skills
        // on demand instead of the skills catalog being injected into
        // the system prompt (~3.4K token saving per request). The tool
        // reads the skill directory from the model config's SkillPath.
        if skillPath := engine.modelCfg.SkillPath; skillPath != "" {
                out = append(out, skilltool.ListSkillsTool(skillPath))
        }
        // Memory tools (remember/recall/revise/forget) are registered only
        // when a shared bank is attached AND memory is effectively on —
        // MemoryEnabled() folds the surface's raw toggle (Ctrl+K / web
        // settings) together with the ACTIVE persona's memory_enabled
        // override, so a persona that opts out (or in) moves tool
        // registration on the very next switch/rebind.
        if engine.memStore != nil && engine.MemoryEnabled() {
                memTools := memtool.Tools(engine.memStore)
                if len(memTools) > 0 {
                        merged := make([]tools.Tool, 0, len(out)+len(memTools))
                        merged = append(merged, out...)
                        merged = append(merged, memTools...)
                        out = merged
                }
        }
        if engine.mcp != nil {
                mcpTools := tools.MCPTools(engine.mcp)
                if len(mcpTools) > 0 {
                        merged := make([]tools.Tool, 0, len(out)+len(mcpTools))
                        merged = append(merged, out...)
                        merged = append(merged, mcpTools...)
                        out = merged
                }
        }
        if engine.jobs == nil {
                return out
        }
        agentTools := tools.AgentTools(tools.AgentDeps{
                Manager:       engine.jobs,
                ParentID:      engine.SessionID,
                WorkDir:       engine.SessionCwd,
                Depth:         engine.agentDepth,
                FleetContract: engine.TaskContract,
        })
        merged := make([]tools.Tool, 0, len(out)+len(agentTools))
        merged = append(merged, out...)
        merged = append(merged, agentTools...)
        out = merged
        // Schedule tool (Bet 01 Clock): registered only when the surface
        // wired a scheduler. One facade with an action enum — the
        // always-on tool count stays put (see the anti-goals).
        if engine.scheduler != nil {
                out = append(out, scheduletool.ScheduleTool(scheduletool.Deps{
                        Scheduler:     engine.scheduler,
                        WorkDir:       engine.SessionCwd,
                        FleetContract: engine.TaskContract,
                }))
        }
        return out
}

// SetModel replaces the LLM client and model-related settings without
// discarding the session tree. Agent tools remain registered when Jobs is set.
// Hooks are left unchanged; use SetModelWithHooks when switching presets.
func (engine *Engine) SetModel(cfg llm.ModelConfig) {
        engine.SetModelWithHooks(cfg, engine.hooks)
}

// SetModelWithHooks replaces the model config and request hooks together.
// The runtime context window is re-seeded from the new config: an explicit
// window resolves immediately; an unknown window re-arms the probe/assume
// on the next turn.
//
// A surface-driven model change also RE-BASES the engine: personas without
// a model override restore baseModelCfg on their next settings pass, so the
// base must follow the surface instead of staying frozen at construction.
// Before this, flipping the web memory toggle (which routes through
// applyPersonaToEngines → SetPersona → applyPersonaSettings) silently
// reverted live engines to whatever model they booted with — the footer
// then showed a different model than the settings panel had picked.
func (engine *Engine) SetModelWithHooks(cfg llm.ModelConfig, hooks llmclient.Hooks) {
        engine.modelCfg = cfg
        engine.baseModelCfg = cfg
        engine.hooks = hooks
        engine.baseHooks = hooks
        engine.resetWindowForModel(cfg)
        engine.rebindTools()
}

// SetMemoryStore attaches or detaches the semantic memory bank and rebuilds
// the tool list: a non-nil store registers remember/recall/revise/forget,
// nil unregisters them. Used by the web surface's memory toggle to rebind
// live engines without restarting their sessions.
func (engine *Engine) SetMemoryStore(store *memstore.Store) {
        if engine == nil {
                return
        }
        engine.memStore = store
        engine.rebindTools()
}

// SetMemoryEnabled sets the surface's raw memory toggle (the global knob the
// palette / web settings panel flips) and rebinds, so the toggle is live on
// the next round without restarting anything. The effective state — what
// this toggle plus the active persona's memory_enabled resolve to — is
// MemoryEnabled(); surfaces that want the persona's setting changed instead
// of the global knob should write the persona (their SetMemoryEnabled
// handlers do exactly that when the active persona carries an explicit
// setting).
func (engine *Engine) SetMemoryEnabled(enabled bool) {
        if engine == nil {
                return
        }
        engine.memoryEnabled.Store(enabled)
        engine.rebindTools()
}

// MemoryEnabled reports the EFFECTIVE memory state: the active persona's
// explicit memory_enabled wins when set; otherwise the surface's raw toggle
// (WithMemoryEnabled / SetMemoryEnabled; ON for engines that never set it).
// This is the single gate for everything memory-shaped — remember/recall
// tool registration (together with a non-nil bank), the per-turn inlet
// injection, and the background outlet consolidation — so "memory off"
// means off everywhere, and a persona that opts in or out moves the whole
// surface with it on the very next switch.
//
// The persona is read live, so a SetPersona (or a persona edit) takes effect
// on the next MemoryEnabled() call without any extra invalidation.
func (engine *Engine) MemoryEnabled() bool {
        if engine == nil {
                return false
        }
        pstore := engine.personaStore()
        if pstore == nil {
                return engine.memoryEnabled.Load()
        }
        p, err := pstore.ActivePersona()
        if err != nil {
                return engine.memoryEnabled.Load()
        }
        if p.MemoryEnabled != nil {
                return *p.MemoryEnabled
        }
        return engine.memoryEnabled.Load()
}

// SetJobs attaches or detaches the job manager and rebuilds the tool list.
// Pass nil to unregister agent_* tools (sub-agents disabled).
func (engine *Engine) SetJobs(jobs *job.Manager) {
        if engine == nil {
                return
        }
        engine.jobs = jobs
        engine.rebindTools()
}

// SetScheduler attaches or detaches the Clock's scheduler (Bet 01) and
// rebinds, so the schedule tool appears/disappears live — mirroring SetJobs.
func (engine *Engine) SetScheduler(s *schedule.Scheduler) {
        if engine == nil {
                return
        }
        engine.scheduler = s
        engine.rebindTools()
}

func (engine *Engine) rebindTools() {
        engine.extensions.SetBaseTools(engine.buildCoreTools(engine.baseTools))
        toolList := engine.buildToolList(engine.baseTools)
        engine.client = llmclient.NewClient(
                engine.modelCfg,
                engine.hooks,
                tools.Definitions(toolList),
                engine.systemPrompt(),
        )
        engine.bindExecutor(tools.NewRegistry(toolList))
}

func (engine *Engine) systemPrompt() string {
        // Persona takeover: when the active persona opts in ("replace the
        // default system prompt"), its prompt IS the system prompt — the
        // built-in a1 behavioral template, project context, skills and MCP
        // catalogs are all dropped so the persona's voice is the only voice.
        // Runtime context (clock, weather, persona memories) still rides in
        // as separate system messages from Session.BuildContext.
        if engine.session != nil {
                if p, ok := engine.session.activePersona(); ok && p.ReplacesSystemPrompt() {
                        return strings.TrimSpace(p.SystemPrompt)
                }
        }
        var mcpServers []string
        if engine.mcp != nil {
                mcpServers = engine.mcp.ServerNames()
        }
        maxConcurrent := 0
        if engine.jobs != nil {
                maxConcurrent = engine.jobs.MaxConcurrent()
        }
        return prompt.Build(engine.modelCfg.SkillPath, engine.jobs != nil, maxConcurrent, mcpServers)
}

// SetPersona switches the active persona on this engine and rebinds the LLM
// client so the change is effective on the very next turn: the system prompt
// is rebuilt (a persona with OverrideSystemPrompt now owns it), the
// executor's memory bank is re-pointed at the new persona's bank, and — when
// the persona carries per-persona settings (Model, PermissionMode) and the
// engine was wired with the corresponding resolvers/setters — those
// settings take effect immediately. Sub-agent engines that share the persona
// store pick the switch up the same way when they are constructed.
//
// Settings applied (in order):
//  1. Clear stale memoryInletMsg — the new persona has its own semantic
//     memory namespace, so any injection from the previous persona is
//     stale and must not carry into the next turn.
//  2. If persona.Model is set and personaModelResolver is wired: resolve
//     the model name to an llm.ModelConfig and update engine.modelCfg.
//     rebindTools() then rebuilds the LLM client with the new config.
//  3. If persona.PermissionMode is set and permissionModeSetter is wired:
//     invoke the setter, which (when wired to the controller) rebuilds
//     the gate with the new mode and preserves session state.
//  4. The closing rebindTools() re-derives memory tool registration from
//     MemoryEnabled() — the new persona's memory_enabled (when explicit)
//     or the surface's raw toggle — so a persona that opts in or out of
//     memory moves remember/recall registration on the same switch.
//
// MemoryEnabled is otherwise checked at retrieval time
// (memStoreForActivePersona), not only at switch time — turning memory
// off mid-session is honored immediately on the next turn without needing
// an explicit clear here.
func (engine *Engine) SetPersona(id string) error {
        if engine == nil {
                return errors.New("agent: nil engine")
        }
        if err := engine.session.SetPersona(id); err != nil {
                return err
        }
        engine.applyPersonaSettings()
        engine.rebindTools()
        return nil
}

// applyPersonaSettings reads the active persona and applies its per-persona
// settings (Model, PermissionMode) that the engine has resolvers/setters
// for. Safe to call when no persona store is attached.
//
// Semantics:
//   - A persona override applies on EVERY switch to that persona — the
//     DEFAULT persona included. The persona editor offers the settings
//     fields for every persona; "the default persona carries no overrides
//     by definition" was true of the shipped defaultPersona() but not of
//     what users configure, and the skip made default-persona settings
//     silently dead (the "falls back to the startup model" report).
//   - A persona WITHOUT an override — and the default persona with none
//     configured — restores the engine's base model + base mode captured
//     at construction, so the previous persona's settings never leak into
//     the next persona.
//   - A model change (override applied OR reverted to base) re-seeds the
//     runtime context window via resetWindowForModel and swaps the request
//     hooks to the model actually in use (different providers need
//     different wire shapes), so the footer's denominator and the client's
//     request building both follow the persona's provider.
//
// Silently no-ops on resolver miss (resolver returns false) — a
// misconfigured persona setting should never break the persona switch
// itself. The user keeps a working setup; the bad setting is just ignored.
func (engine *Engine) applyPersonaSettings() {
        if engine == nil || engine.session == nil {
                return
        }
        // 1. Clear stale memory injection. The new persona has its own
        //    semantic memory namespace; an injection from the previous
        //    persona would be wrong context. The inlet is rebuilt on
        //    the next turn via injectSemanticMemories().
        engine.memoryInletMsg = nil

        // Look up the active persona. No store (or read error) → treat as
        // the default persona: restore base settings, clear nothing else.
        pstore := engine.personaStore()
        if pstore == nil {
                return
        }
        p, err := pstore.ActivePersona()
        if err != nil {
                return
        }

        // 2. Apply the model override, or revert to base when this persona
        //    doesn't name one. Both directions reset the runtime context
        //    window: the new model's provider may have a different window,
        //    and a stale denominator would mis-trigger (or skip) compaction.
        //    Hooks follow the model in use — a persona model on another
        //    provider needs that provider's wire shape, not the main one's.
        if p.Model != "" {
                if engine.personaModelResolver != nil {
                        if m, ok := engine.personaModelResolver(p.Model); ok {
                                engine.modelCfg = m
                                engine.hooks = model.HooksFor(m.Name)
                                engine.resetWindowForModel(m)
                        }
                        // Resolver miss → keep the previous modelCfg. The user
                        // will see the wrong model in use; better to keep a
                        // working model than to break the engine.
                }
        } else if engine.modelCfg.Name != engine.baseModelCfg.Name {
                engine.modelCfg = engine.baseModelCfg
                engine.hooks = engine.baseHooks
                engine.resetWindowForModel(engine.baseModelCfg)
        }

        // 3. Apply the permission-mode override, or restore the base mode.
        //    The setter rebuilds the surface's gate, so the restriction (or
        //    its removal) is live from the very next tool call.
        if engine.permissionModeSetter == nil {
                return
        }
        if p.PermissionMode != "" {
                engine.permissionModeSetter(permission.Mode(p.PermissionMode))
                return
        }
        if engine.basePermissionMode != "" && engine.basePermissionMode != permission.ModeOf(engine.gate) {
                engine.permissionModeSetter(engine.basePermissionMode)
        }
}

func (engine *Engine) bindExecutor(registry tools.Registry) {
        engine.executor = NewExecutor(registry, engine.gate, engine.ask, engine.extensions, engine.question)
        engine.executor.approveAdmit = approveAdmitFunc(engine.gate)
        engine.executor.SetMeta(engine.SessionID(), engine.SessionCwd())
        engine.executor.SetEvidenceLedger(engine.evidence)
        // The governed latch is read live at record time, so a mid-session
        // ledger swap (SetEvidenceLedger) or the first declaration takes
        // effect on the very next bash run without rebinding.
        engine.executor.SetGovernedFn(engine.IsGoverned)
        // JEV decision-model evidence advisor (see jev_evidence.go): admits
        // evidence for check-like commands no deterministic parser knows.
        engine.executor.SetJevAdvisor(engine.jevAdv)
        // Read elision rides on every engine: the check is cheap, the payoff
        // (not re-sending a file the model provably still has) compounds over
        // a long session, and the compaction-aware liveness check keeps it
        // safe by construction.
        engine.executor.SetReadElider(engine.elideUnchangedRead)
        // Image blocks ride tool results only for vision-capable models;
        // rebind on every model switch so the flag never goes stale.
        engine.executor.SetImageEnabled(engine.modelCfg.ImageEnabled)
        if engine.session != nil {
                engine.executor.SetMemoryBank(engine.session.MemoryBank())
        }
}

// defaultEvidenceLedger installs the engine's evidence ledger. Explicit
// wiring wins; otherwise persisted sessions keep receipts next to the
// session file (evidence.jsonl) so the protected ratchet survives resume,
// and in-memory sessions get a memory-only ledger. A corrupt receipt file
// never breaks the loop — it logs and starts from a clean slate. Config-
// defined checks (config.yaml `checks:`) are installed on the default
// ledger; a bad success pattern logs and drops the set rather than
// blocking engine construction.
func defaultEvidenceLedger(sess *Session, wired *evidence.Ledger, checks []evidence.CheckDef) *evidence.Ledger {
        if wired != nil {
                return wired
        }
        // Bet 08: when the vault is live, receipts are rows keyed by the same
        // project scope the file era used (the session file's directory).
        if v := session.DefaultVault(); v != nil {
                if f := sess.File(); f != "" {
                        ledger := evidence.NewVaultLedger(filepath.Dir(f), v)
                        if len(checks) > 0 {
                                if err := ledger.SetChecks(checks); err != nil {
                                        debuglog.Logf("agent: evidence checks install: %v", err)
                                }
                        }
                        if err := ledger.Load(); err != nil {
                                debuglog.Logf("agent: evidence ledger load (vault): %v", err)
                        }
                        return ledger
                }
        }
        var path string
        if f := sess.File(); f != "" {
                path = filepath.Join(filepath.Dir(f), "evidence.jsonl")
        }
        ledger := evidence.NewLedger(path)
        if len(checks) > 0 {
                if err := ledger.SetChecks(checks); err != nil {
                        debuglog.Logf("agent: evidence checks install: %v", err)
                }
        }
        if err := ledger.Load(); err != nil {
                debuglog.Logf("agent: evidence ledger load %s: %v", path, err)
        }
        return ledger
}

// IsGoverned reports whether the session's evidence surfaces are live:
// the executor records bash runs, the verdict gate may baseline and
// roll back, and compaction summaries carry the evidence record. The
// latch is set by the first real contract declaration and never cleared;
// a non-empty ledger (resumed receipts, direct seeding) also reads as
// governed, so derivation needs no second flag.
func (engine *Engine) IsGoverned() bool {
        if engine == nil {
                return false
        }
        if engine.governed.Load() {
                return true
        }
        if l := engine.evidence; l != nil && len(l.State()) > 0 {
                return true
        }
        return false
}

// SetEvidenceLedger replaces the engine's evidence ledger (primarily for
// tests). The executor is rebound so the new ledger takes effect
// immediately.
func (engine *Engine) SetEvidenceLedger(l *evidence.Ledger) {
        if engine == nil {
                return
        }
        engine.evidence = l
        if engine.executor != nil {
                engine.executor.SetEvidenceLedger(l)
        }
}

// EvidenceLedger returns the engine's evidence ledger (nil-safe).
func (engine *Engine) EvidenceLedger() *evidence.Ledger {
        if engine == nil {
                return nil
        }
        return engine.evidence
}

// SetStallLimit retunes the verdict gate's anti-thrash limit: the number of
// consecutive non-improving verification rounds a turn tolerates before it
// stops with the residual failure set. n <= 0 disables the stall stop.
func (engine *Engine) SetStallLimit(n int) {
        if engine == nil {
                return
        }
        engine.stallLimit.Store(int64(max(n, 0)))
}

// StallLimit reports the verdict gate's anti-thrash limit (0 = disabled).
func (engine *Engine) StallLimit() int {
        if engine == nil {
                return 0
        }
        return int(engine.stallLimit.Load())
}

// SetZeroRankBudget retunes the Phase 3 zero-rank stop's verification
// budget (extra rounds bought when the model tries to end a turn with
// declared checks not green). n <= 0 disables the zero-rank stop.
func (engine *Engine) SetZeroRankBudget(n int) {
        if engine == nil {
                return
        }
        engine.zeroRankBudget.Store(int64(max(n, 0)))
}

// ZeroRankBudget reports the zero-rank verification budget (0 = disabled).
func (engine *Engine) ZeroRankBudget() int {
        if engine == nil {
                return 0
        }
        return int(engine.zeroRankBudget.Load())
}

// TurnContract returns the checks currently declared in the turn's
// contract (sorted copy; empty when nothing is declared).
func (engine *Engine) TurnContract() []string {
        if engine == nil {
                return nil
        }
        return engine.turnContract.snapshot()
}

// TaskContract returns the session's task-scoped contract: every check
// declared this session plus any fleet-inherited checks (sorted copy).
// Spawns forward this set so children share the parent's finish line.
func (engine *Engine) TaskContract() []string {
        if engine == nil {
                return nil
        }
        return engine.taskContract.snapshot()
}

// SeedTaskContract seeds the engine with a parent fleet's declared check
// ids (Bet 04 task contracts). The seed survives per-Loop resets: every
// turn of this engine starts with the inherited checks. When enforce is
// true (worker children) the checks enter the turn contract and the
// zero-rank stop holds the child to the parent's finish line; otherwise
// (explore/review) they only arm the ledger watches so evidence flows
// into the structured handoff without holding recon hostage to a finish
// line it cannot green. The seed also enters the task contract, so this
// engine's own spawns inherit it unchanged.
func (engine *Engine) SeedTaskContract(checks []string, enforce bool) {
        if engine == nil {
                return
        }
        valid := make([]string, 0, len(checks))
        for _, raw := range checks {
                id := strings.TrimSpace(raw)
                if evidence.ValidCheckID(id) {
                        valid = append(valid, id)
                }
        }
        if len(valid) == 0 {
                return
        }
        engine.contractSeed = valid
        engine.contractSeedEnforce = enforce
        engine.taskContract.declare(valid)
}

// applyContractSeed re-applies the fleet-inherited contract seed after a
// per-Loop reset. Called at every Loop entry; a no-op for unseeded engines
// (every pre-Bet-04 session is byte-identical).
func (engine *Engine) applyContractSeed() {
        if engine == nil || len(engine.contractSeed) == 0 {
                return
        }
        if engine.contractSeedEnforce {
                engine.turnContract.declare(engine.contractSeed)
        }
        for _, id := range engine.contractSeed {
                engine.evidence.Watch(id)
        }
        engine.governed.Store(true)
}

// SetBudgetContracts toggles enforcement of model-declared tool-round
// budgets at runtime. Declarations are always recorded and echoed either
// way; enforcement decides whether a firing can stop the turn.
func (engine *Engine) SetBudgetContracts(enabled bool) {
        if engine == nil {
                return
        }
        engine.budgetContracts.Store(enabled)
}

// BudgetContracts reports whether declared tool-round budgets are enforced.
func (engine *Engine) BudgetContracts() bool {
        if engine == nil {
                return false
        }
        return engine.budgetContracts.Load()
}

// TurnBudgetCeiling reports the turn's declared tool-round ceiling
// (0 = nothing declared this turn).
func (engine *Engine) TurnBudgetCeiling() int {
        if engine == nil {
                return 0
        }
        ceiling, _, _ := engine.turnBudget.snapshot()
        return ceiling
}

// evidenceSnapshots lazily builds the snapshot manager backing the verdict
// gate. Session cwd is fixed for the engine's lifetime, so the manager is
// built once and reused across turns.
func (engine *Engine) evidenceSnapshots() evidenceSnapshots {
        if engine.evidenceSnaps == nil {
                engine.evidenceSnaps = snapshot.NewManager(engine.SessionCwd())
        }
        return engine.evidenceSnaps
}

// setEvidenceSnapshots overrides the snapshot backend used by the verdict
// gate (test seam; production uses the git snapshot manager).
func (engine *Engine) setEvidenceSnapshots(s evidenceSnapshots) {
        engine.evidenceSnaps = s
}

// newEvidenceGate builds the per-turn verdict gate: pre-candidate
// baselines, enforced rollback in autopilot/headless-strict, stall
// tracking. The gate reads the live permission mode at decision time (the
// APPA mode is the strictness dial) and hands each baseline name back to
// the engine so the next turn's gate can clean it up once superseded.
func (engine *Engine) newEvidenceGate() *turnEvidenceGate {
        return newTurnEvidenceGate(
                engine.evidenceSnapshots(),
                engine.evidence,
                func() permission.Mode { return permission.ModeOf(engine.gate) },
                int(engine.stallLimit.Load()),
                engine.evidenceBaseline,
                func(name string) { engine.evidenceBaseline = name },
        )
}

// ContextTokens estimates the total context usage in tokens (system prompt +
// project context + messages + tool outputs + tool schemas). Uses the
// 4-bytes-per-token heuristic the /context breakdown also uses. The footer
// counter calls this instead of toolBytes/4 because toolBytes only counts
// tool schemas — the old counter showed ~5.8k when the real usage was ~9.8k.
//
// Nil-safe: returns 0 when the engine or session isn't initialized.
func (engine *Engine) ContextTokens() int {
        if engine == nil || engine.session == nil {
                return 0
        }
        totalBytes := 0
        // System prompt: the template + project context + MCP block, as
        // built by prompt.Build (same call the client constructor makes).
        sysPrompt := engine.systemPrompt()
        totalBytes += len(sysPrompt)
        // Messages: the full conversation including tool messages.
        msgs := engine.session.BuildContext()
        for _, msg := range msgs {
                totalBytes += len(msg.Content)
                totalBytes += len(msg.ReasoningContent)
                for _, tc := range msg.ToolCalls {
                        totalBytes += len(tc.Function.Name) + len(tc.Function.Arguments)
                }
        }
        // Tool schemas.
        toolBytes, _ := engine.ToolSchemaBytes()
        totalBytes += toolBytes
        return totalBytes / contextbreakdown.BytesPerToken
}

// ToolSchemaBytes returns the combined serialized size of every registered tool
// definition, plus the count. The /context breakdown uses it to size the "Tools"
// category: schemas are context the model pays for before the conversation
// starts, and they grow as extensions and MCP servers register.
func (engine *Engine) ToolSchemaBytes() (bytes, count int) {
        if engine == nil || engine.executor == nil {
                return 0, 0
        }
        for _, t := range engine.executor.registry {
                bytes += len(t.Definition.Name) + len(t.Definition.Description)
                if t.Definition.Params == nil {
                        count++
                        continue
                }
                bytes += len(t.Definition.Params.Type)
                for k, v := range t.Definition.Params.Properties {
                        bytes += len(k) + toolsSchemaValueBytes(v)
                }
                for _, r := range t.Definition.Params.Required {
                        bytes += len(r)
                }
                count++
        }
        return bytes, count
}

// HasTool reports whether a tool is currently registered on the executor.
func (engine *Engine) HasTool(name string) bool {
        if engine == nil || engine.executor == nil {
                return false
        }
        _, ok := engine.executor.registry[name]
        return ok
}

// toolsSchemaValueBytes sizes a JSON-schema-ish value without pulling
// encoding/json into a per-call path.
func toolsSchemaValueBytes(v any) int {
        switch t := v.(type) {
        case string:
                return len(t)
        case map[string]any:
                n := 0
                for k, vv := range t {
                        n += len(k) + toolsSchemaValueBytes(vv)
                }
                return n
        case []any:
                n := 0
                for _, vv := range t {
                        n += toolsSchemaValueBytes(vv)
                }
                return n
        default:
                return 0
        }
}

// SetPermission updates the gate and ask handler used by the tool executor.
func (engine *Engine) SetPermission(gate permission.Gate, ask permission.AskFunc) {
        if engine == nil {
                return
        }
        engine.gate = gate
        engine.ask = ask
        if engine.executor != nil {
                engine.executor.gate = gate
                engine.executor.ask = ask
                engine.executor.approveAdmit = approveAdmitFunc(gate)
        }
}

// SetBasePermissionMode updates the mode restored when a persona without a
// mode override is active. Surface-driven mode changes (the web footer's
// mode pill, the settings mode row) must move the base with them —
// otherwise the next persona-settings pass (a persona switch, the memory
// toggle, a persona edit) reverts the live gate to whatever mode the engine
// booted with. The persona path itself never calls this: overrides apply
// without touching the base, which is exactly what keeps them restorable.
func (engine *Engine) SetBasePermissionMode(mode permission.Mode) {
        if engine == nil {
                return
        }
        engine.basePermissionMode = mode
}

// approveAdmitFunc returns the callback that folds an approved Gate 2
// admission into the trajectory. Nil when the gate cannot approve admissions.
func approveAdmitFunc(gate permission.Gate) func(permission.Request) {
        static := permission.StaticGateOf(gate)
        if static == nil {
                return nil
        }
        return static.ApproveAdmit
}

// SetContinueAsk sets the handler invoked when the tool-round budget is exhausted.
// Pass nil to hard-fail with ErrMaxRounds (default for headless runs).
func (engine *Engine) SetContinueAsk(fn ContinueFunc) {
        if engine == nil {
                return
        }
        engine.continueAsk = fn
}

// SetMaxToolCalls sets the per-turn tool-call budget (0 = unlimited). Safe to
// call while a loop is running — the change takes effect at the next tool
// round. Yolo permission mode overrides the cap with unlimited.
func (engine *Engine) SetMaxToolCalls(n int) {
        if engine == nil {
                return
        }
        if n < 0 {
                n = 0
        }
        engine.maxToolCalls.Store(int64(n))
}

// MaxToolCalls returns the configured per-turn tool-call budget (0 = unlimited).
func (engine *Engine) MaxToolCalls() int {
        if engine == nil {
                return 0
        }
        return int(engine.maxToolCalls.Load())
}

// toolBudgetUnlimited reports whether the tool-call budget is suppressed for
// the current gate: yolo mode promises unlimited tool calls, so the budget
// must not fight the mode the user explicitly chose.
func (engine *Engine) toolBudgetUnlimited() bool {
        return permission.ModeOf(engine.gate) == permission.ModeYolo
}

// SetExtensions replaces the extension runner. Pass nil to disable extensions.
// Rebinds tools so RegisterTool from the new runner takes effect.
func (engine *Engine) SetExtensions(r *extension.Runner) {
        if engine == nil {
                return
        }
        engine.extensions = r
        engine.rebindTools()
}

// SessionID returns the durable session id.
func (engine *Engine) SessionID() string {
        if engine == nil || engine.session == nil {
                return ""
        }
        return engine.session.ID()
}

// SessionFile returns the JSONL path (empty in memory mode).
func (engine *Engine) SessionFile() string {
        if engine == nil || engine.session == nil {
                return ""
        }
        return engine.session.File()
}

// SessionCwd returns the cwd recorded on the session header.
func (engine *Engine) SessionCwd() string {
        if engine == nil || engine.session == nil {
                return ""
        }
        return engine.session.Cwd()
}

// Session returns the underlying session wrapper (for UI transcript replay).
func (engine *Engine) Session() *Session {
        if engine == nil {
                return nil
        }
        return engine.session
}

// recentUserMessages returns up to maxCount of the most recent user
// messages in the session, oldest-first. Used by the memory outlet to
// give the consolidation LLM conversation context for pronoun resolution
// (e.g., "I just upgraded to Go 1.22" → "I" refers to the user, the
// recent user messages make that explicit).
//
// Trivial impl: walks the session's BuildContext output and filters for
// user-role messages. For large sessions this is O(n) on each outlet
// call, but the outlet is a background goroutine so the cost is hidden
// from the user-facing turn.
func (engine *Engine) recentUserMessages(maxCount int) []string {
        if engine == nil || engine.session == nil || maxCount <= 0 {
                return nil
        }
        msgs := engine.session.BuildContext()
        var collected []string
        for i := len(msgs) - 1; i >= 0 && len(collected) < maxCount; i-- {
                if msgs[i].Role == llm.RoleUser {
                        collected = append(collected, msgs[i].Content)
                }
        }
        // Reverse to oldest-first order.
        for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
                collected[i], collected[j] = collected[j], collected[i]
        }
        return collected
}

// LoopOpts configures a single agent loop turn.
type LoopOpts struct {
        // PendingSkills are skill names the user selected in the composer.
        // When set, the model is instructed to read those SKILL.md files first.
        PendingSkills []string
        // Images are base64 attachments from the composer pending queue.
        Images []llm.Image
}

// Loop appends the user prompt and runs inference + tool rounds until the
// model stops calling tools or the context is cancelled.
//
// Compaction runs in two places:
//  1. After a finished turn (final assistant with no tool_calls), when usage
//     crosses the context-window threshold.
//  2. Mid-loop on a context-overflow API error: force-compact, rebuild
//     context, and retry the stream — up to maxConsecutiveOverflowRecoveries
//     consecutive times per turn (the streak resets after any successful
//     round). A provider-stated limit in the error is learned as the runtime
//     window; a numberless overflow halves the retry window (turn-local) so
//     the post-compaction context fits windows smaller than configured.
func (engine *Engine) Loop(ctx context.Context, prompt string, opts LoopOpts) iter.Seq2[session.Event, error] {
        return func(yield func(session.Event, error) bool) {
                // The extension runner is nil-safe (nil = disabled), so calls are
                // unconditional no-ops when no extensions are loaded.
                content, handled := engine.extensions.EmitUserInput(prompt)
                if handled {
                        return
                }
                if instr := pendingSkillsInstruction(engine.modelCfg.SkillPath, opts.PendingSkills); instr != "" {
                        if content == "" {
                                content = instr
                        } else {
                                content = instr + "\n\n" + content
                        }
                }
                rewritten, extra := engine.extensions.EmitBeforeAgentStart(content)
                content = rewritten
                if extra != "" {
                        content = content + "\n\n" + extra
                }
                engine.extensions.EmitAgentStart()
                defer engine.extensions.EmitAgentEnd()
                if err := engine.session.Append(llm.Message{
                        Role:    llm.RoleUser,
                        Content: content,
                        Images:  append([]llm.Image(nil), opts.Images...),
                }); err != nil {
                        yield(nil, err)
                        return
                }

                // Memory inlet (every-turn, pre-LLM): retrieve the top-K
                // relevant memories from the active persona's semantic store
                // and inject as a transient system message before the user
                // message we just appended. The message is NOT persisted to
                // the session — it's rebuilt every turn so memories written
                // mid-conversation (via the outlet) surface immediately on
                // the next user message.
                //
                // Returned nil when: no persona store attached, memory
                // disabled, skip detector flagged the message, or no
                // relevant memories found. All silent — a broken memory
                // subsystem never breaks the agent loop.
                engine.memoryInletMsg = engine.injectSemanticMemories(ctx, content)

                // memfsck (Bet 06): reconcile <cwd>/memory with MEMORY.md
                // once per session start, as the package doc always said.
                engine.runMemfsckAtSessionStart()

                toolRounds := 0
                toolCallsUsed := 0
                // Zero-rank stop (Phase 3): a fresh user prompt starts a
                // fresh contract, and the verification-round counter the
                // contract gate buys against starts at zero.
                engine.turnContract.reset()
                // Task contracts (Bet 04): a fleet-inherited seed re-enters the
                // fresh turn contract (workers) or arms its watches (explore/
                // review) so the child is held to — or at least records evidence
                // for — the parent's declared finish line, every turn.
                engine.applyContractSeed()
                zeroRankRounds := 0
                // Declared-budget gate (Phase 5): a fresh user prompt starts
                // a fresh budget, and the live round counter the budget tool
                // declares against starts at zero.
                engine.turnBudget.reset()
                engine.toolRoundsLive.Store(0)
                // Verdict gate (Phase 2): per-turn evidence ratchet. Any verdicts
                // left undrained by an aborted prior turn are stale for this
                // one — discard them so the candidate marker stays honest.
                engine.executor.DrainEvidenceVerdicts()
                evGate := engine.newEvidenceGate()
                // Overflow recovery state (doc/context-folding-math.md §5):
                // overflowStreak counts consecutive recoveries and resets on
                // any successful stream round; recoveryWindow is the
                // turn-local window the recovery compaction derives its
                // settings from (0 = use the engine window). A numberless
                // overflow halves it per retry so the post-compaction
                // context fits even when the real window is smaller than
                // anything configured or probed. It is deliberately
                // turn-local: a numberless overflow is usually one oversized
                // request, not proof the persistent window is wrong.
                overflowStreak := 0
                recoveryWindow := 0
                // Resolve the runtime window (config → probe → assumed)
                // before the first stream so folding works from the very
                // first turn on unknown-window models.
                engine.ensureWindow(ctx)
                for {
                        if ctx.Err() != nil {
                                return
                        }

                        engine.extensions.EmitTurnStart(toolRounds)

                        msgs := engine.session.BuildContext()
                        // Apply the per-turn memory inlet injection. The
                        // inlet is rebuilt every turn so memories written
                        // mid-conversation surface on the next user
                        // message. We don't cache this in the session's
                        // context cache (it's a transient pre-LLM
                        // injection, not persistent state).
                        msgs = injectMemoryBeforeLastUser(msgs, engine.memoryInletMsg)

                        msg, completeEvent, err := engine.streamTurn(ctx, yield, msgs)
                        if err != nil {
                                if llm.IsContextOverflow(err) && overflowStreak < maxConsecutiveOverflowRecoveries {
                                        // Learn the real window when the provider states
                                        // it; that is ground truth and persists. Otherwise
                                        // halve the retry window (turn-local) so the
                                        // recovery compaction keeps more headroom on each
                                        // attempt.
                                        if limit, ok := llm.ParseContextLimit(err); ok {
                                                engine.setContextWindow(limit)
                                                recoveryWindow = limit
                                        } else if recoveryWindow <= 0 {
                                                engine.ensureWindow(ctx)
                                                recoveryWindow = engine.ContextWindow()
                                        } else {
                                                recoveryWindow = max(recoveryWindow/2, llm.ContextWindowFloor())
                                        }
                                        did, cerr := engine.runCompact(ctx, yield, 0, true, recoveryWindow)
                                        if cerr == nil && did {
                                                overflowStreak++
                                                continue
                                        }
                                        // Compaction failed or had nothing left to
                                        // shrink: fall through and surface the original
                                        // overflow instead of burning more retries.
                                }
                                yield(nil, err)
                                return
                        }
                        // A successful round proves the context fits: reset the
                        // recovery streak and window.
                        overflowStreak = 0
                        recoveryWindow = 0
                        if completeEvent == nil {
                                // Cancelled or consumer stopped — no error to surface.
                                return
                        }

                        // Defer publishing and persisting the terminal assistant update until
                        // the tool budget is checked. An over-budget tool request must not
                        // leave an unexecuted tool call in the session or UI.
                        if len(msg.ToolCalls) > 0 && toolRounds >= engine.maxRounds {
                                if engine.continueAsk == nil {
                                        yield(nil, fmt.Errorf("agent: %w (%d)", ErrMaxRounds, engine.maxRounds))
                                        return
                                }
                                ok, askErr := engine.continueAsk(ctx, engine.maxRounds)
                                if askErr != nil {
                                        yield(nil, askErr)
                                        return
                                }
                                if !ok {
                                        yield(nil, fmt.Errorf("agent: %w (%d)", ErrMaxRounds, engine.maxRounds))
                                        return
                                }
                                // Granted: reset the budget; the current and following tool
                                // rounds run under the fresh budget.
                                toolRounds = 0
                        }
                        // Declared-budget gate (Phase 5): the model's self-declared
                        // tool-round budget. Under the held ceiling nothing changes;
                        // at the ceiling with tools still wanted, the round runs on
                        // the turn's one re-open (extend or finish — a note lands
                        // after the batch); a second overrun without a new
                        // declaration stops the turn with a spend report. No
                        // declaration = no gate, so plain chat turns are untouched.
                        budget := engine.budgetGate(toolRounds, msg.ToolCalls)
                        if budget.Terminal {
                                // Memory outlet: a budget-terminal stop is still a
                                // completed turn — the conversation content is real
                                // even though the pending calls were rejected.
                                engine.consolidateTurn(content, msg)
                                engine.budgetTerminalStop(msg, completeEvent, toolRounds, yield)
                                return
                        }
                        if !yield(completeEvent, nil) {
                                return
                        }

                        if len(msg.ToolCalls) == 0 {
                                // No tool calls: persist the assistant message
                                // before the turn ends so the next BuildContext
                                // (for the next user prompt) sees it. The Append
                                // can't be deferred past here — there's no tool
                                // work to overlap it with.
                                if err := engine.session.Append(msg); err != nil {
                                        yield(nil, err)
                                        return
                                }
                                // Memory outlet (background, post-LLM): consolidate this
                                // turn's content into the persona's semantic memory store.
                                // Every completed-turn terminal path fires this (see
                                // consolidateTurn); the goroutine has its own 30s timeout
                                // and self-removes when done.
                                engine.consolidateTurn(content, msg)
                                engine.extensions.EmitTurnEnd(toolRounds)
                                // Zero-rank stop (Phase 3): "the model said
                                // done" is not done. If the turn declared a
                                // contract, judge it against the evidence
                                // ledger now: residual checks re-open the
                                // loop (bounded), a clean contract certifies
                                // the end, an exhausted budget stops the
                                // turn with the residual set surfaced.
                                if zr := engine.zeroRankCheck(&zeroRankRounds); zr.Note != "" || zr.Continue || zr.Stop {
                                        if err := engine.session.Append(llm.Message{Role: llm.RoleUser, Content: zr.Note}); err != nil {
                                                yield(nil, err)
                                                return
                                        }
                                        if !yield(session.UserAppend{Text: zr.Note}, nil) {
                                                return
                                        }
                                        if zr.Continue {
                                                continue
                                        }
                                        if zr.Stop {
                                                return
                                        }
                                        // Certificate: fall through to the
                                        // normal end (steering, compaction).
                                }
                                if cont, steer := engine.extensions.EmitTurnStopping(toolRounds); cont {
                                        steerMsg := steer
                                        if steerMsg == "" {
                                                steerMsg = "continue"
                                        }
                                        if err := engine.session.Append(
                                                llm.Message{Role: llm.RoleUser, Content: steerMsg},
                                        ); err != nil {
                                                yield(nil, err)
                                                return
                                        }
                                        continue
                                }
                                // Turn finished — compact using this assistant's usage. The
                                // threshold reads context size the same way the composer does.
                                if _, err := engine.runCompact(ctx, yield, msg.Usage.ContextTokens(), false, 0); err != nil {
                                        yield(nil, err)
                                }
                                return
                        }

                        // Tool calls: run the tools FIRST, then persist the
                        // assistant message and tool results together. This
                        // overlaps the disk write (Append) with the network/
                        // tool work that just finished, instead of serializing
                        // the tools behind the Append. The assistant message
                        // is already cached in `msg` from streamTurn, so the
                        // executor has what it needs without the session tree.
                        toolRounds++
                        // Publish the live round count for the budget tool
                        // handler (Phase 5): a declaration made during this
                        // round prices itself against rounds already used,
                        // including this one once it completes.
                        engine.toolRoundsLive.Store(int64(toolRounds))
                        // Per-turn tool-call budget: each executed call consumes a
                        // slot. Over-budget calls are rejected (so the session and
                        // UI never hold unexecuted tool calls) and the turn ends
                        // cleanly with a system note. Yolo mode runs unlimited.
                        allowance := -1
                        if limit := int(engine.maxToolCalls.Load()); limit > 0 && !engine.toolBudgetUnlimited() {
                                allowance = limit - toolCallsUsed
                                if allowance < 0 {
                                        allowance = 0
                                }
                        }
                        engine.executor.SetCallAllowance(allowance)
                        // Verdict gate (Phase 2): take the pre-candidate baseline
                        // before a mutating batch can touch the workspace, and
                        // capture the evidence state to diff against when the
                        // batch completes.
                        mutating := engine.executor.BatchMutates(msg.ToolCalls)
                        evGate.beforeBatch(ctx, mutating)
                        preEvidence := engine.evidence.State()
                        // Sources stash: the executor emits each run's ranked sources
                        // with the ToolData event (the SSE stream mirrors them live),
                        // but the persisted tool result needs them too — otherwise a
                        // reloaded session loses its source pills. Keyed by tool_use
                        // id, drained into the AppendToolResult calls below.
                        toolSources := make(map[string][]session.ToolSource)
                        toolMsgs, stop, _ := engine.executor.Run(ctx, msg.ToolCalls, func(td session.ToolData) bool {
                                if len(td.Run.Sources) > 0 {
                                        toolSources[td.Run.ToolUseID] = td.Run.Sources
                                }
                                return yield(td, nil)
                        })
                        toolCallsUsed += engine.executor.AllowanceUsed()
                        // Persist the assistant message, then the tool results.
                        // Order matters: Append(msg) sets the leaf so each
                        // tool message's ParentID points at the assistant msg.
                        if err := engine.session.Append(msg); err != nil {
                                yield(nil, err)
                                return
                        }
                        for _, tm := range toolMsgs {
                                var sources []session.ToolSource
                                if tm.Role == llm.RoleTool {
                                        sources = toolSources[tm.ToolCallID]
                                }
                                if err := engine.session.AppendToolResult(tm, sources); err != nil {
                                        yield(nil, err)
                                        return
                                }
                        }
                        engine.extensions.EmitTurnEnd(toolRounds - 1)
                        // Verdict gate (Phase 2): judge the completed batch
                        // against its pre-candidate evidence — enforced rollback
                        // on protected regression, baseline advance on clean
                        // candidates, stall tracking. The note lands as a system
                        // note the model reads next round; Stop ends the turn
                        // with the residual failure set instead of burning
                        // budget. The fold runs before honoring executor stop so
                        // a regression is still rejected on the way out.
                        action := evGate.afterBatch(ctx, preEvidence, len(engine.executor.DrainEvidenceVerdicts()) > 0, mutating)
                        if action.Note != "" {
                                if err := engine.session.Append(llm.Message{Role: llm.RoleUser, Content: action.Note}); err != nil {
                                        yield(nil, err)
                                        return
                                }
                                if !yield(session.UserAppend{Text: action.Note}, nil) {
                                        return
                                }
                        }
                        if action.Stop {
                                // Memory outlet: a verdict-gate stop still ends a
                                // completed turn — the exchange is worth keeping.
                                engine.consolidateTurn(content, msg)
                                return
                        }
                        if stop {
                                // Memory outlet: an executor stop still ends a
                                // completed turn.
                                engine.consolidateTurn(content, msg)
                                return
                        }

                        // Budget exhausted mid-turn: stop cleanly instead of erroring.
                        // The over-budget calls already came back as rejected results,
                        // so the session stays consistent; the note lands in the
                        // transcript and the session history so the model knows on the
                        // next prompt why the turn ended early.
                        if engine.executor.AllowanceTripped() {
                                note := fmt.Sprintf(
                                        "System note: tool call budget (%d) reached — the turn was stopped. Raise the cap from the command palette (Max tool calls) or ask to continue.",
                                        engine.maxToolCalls.Load(),
                                )
                                if err := engine.session.Append(llm.Message{Role: llm.RoleUser, Content: note}); err != nil {
                                        yield(nil, err)
                                        return
                                }
                                yield(session.UserAppend{Text: note}, nil)
                                // Memory outlet: the turn was cut short by the
                                // tool-call budget, but the conversation still
                                // happened — consolidate it.
                                engine.consolidateTurn(content, msg)
                                return
                        }

                        // Declared-budget grace note (Phase 5): this round ran
                        // on the turn's re-open. If the model did not extend
                        // during it (ceiling still reached), say so before the
                        // next round decides the turn — an in-round declaration
                        // already answered itself via the tool reply, so no
                        // note then.
                        if budget.Grace && engine.turnBudget.stillSpent(toolRounds) {
                                note := engine.budgetGraceNote(toolRounds)
                                if err := engine.session.Append(llm.Message{Role: llm.RoleUser, Content: note}); err != nil {
                                        yield(nil, err)
                                        return
                                }
                                if !yield(session.UserAppend{Text: note}, nil) {
                                        return
                                }
                        }

                        if ctx.Err() != nil {
                                return
                        }
                }
        }
}

// runCompact prepares and persists a compaction entry.
// When force is false, it no-ops unless usage crosses the window threshold.
// When force is true (overflow recovery), it skips the threshold check but
// still no-ops when there is nothing useful to summarize (avoids burning an
// overflow retry on a no-op compact).
// window is the context window the settings derive from: overflow recovery
// passes its turn-local retry window; 0 means "resolve and use the engine
// window". did is true only when a compaction entry was appended.
//
// Two rulers, by design:
//   - safety valve (ShouldCompact): compares `usage` (provider-reported
//     real tokens) against the model's context window (also real tokens).
//     This is the hard-overflow guard and MUST use real tokens.
//   - growth gate (ShouldCompactGated): compares the estimated context
//     size (bytes/4, from EstimateContextTokens) against lastCompactEstimate
//     (also bytes/4). Both operands must use the same ruler; mixing real
//     tokens and bytes/4 broke the growth calculation (doc/history-issues.md #12).
func (engine *Engine) runCompact(
        ctx context.Context,
        yield func(session.Event, error) bool,
        usage int,
        force bool,
        window int,
) (did bool, err error) {
        // One compaction at a time: the post-turn auto path and a surface's
        // ForceCompact race otherwise (see compactMu).
        engine.compactMu.Lock()
        defer engine.compactMu.Unlock()
        if window <= 0 {
                engine.ensureWindow(ctx)
                window = engine.ContextWindow()
        }
        settings := compaction.SettingsFor(window)
        if engine.client == nil {
                return false, nil
        }
        // Two triggers, OR'd together:
        //  1. Safety valve (ShouldCompact): context is within reverseTokens of the
        //     window. This must fire regardless of growth so the context never
        //     exceeds the window. Uses provider-reported real tokens.
        //  2. Growth gate (ShouldCompactGated): context exceeds the floor fraction
        //     AND has grown by at least GrowthThreshold since the last compaction.
        //     Uses bytes/4 estimated tokens — the same ruler as the cut logic and
        //     lastCompactEstimate. Mixing real tokens here (the old code) made the
        //     growth check subtract bytes/4 from real tokens, producing nonsense.
        // Window: the resolved runtime window (config → probe → assumed →
        // learned), not the raw model config — a model without context_window
        // resolves to the assumed default, and both triggers refuse to fire
        // on window 0 (the "folding never runs on local models" bug).
        estimatedUsage := 0
        if !force {
                estimatedUsage = compaction.EstimateContextTokens(engine.session.PathEntries())
        }
        // Provider-reported usage is the right ruler for the safety valve, but
        // a provider that reports none (usage 0) must not leave the valve
        // blind — the bytes/4 estimate stands in. It can underestimate (code,
        // CJK text), so the valve may fire late, but late beats never: a
        // blind valve is how a 64K session walks straight into a hard
        // overflow with compaction never having fired.
        safetyUsage := usage
        if safetyUsage <= 0 {
                safetyUsage = estimatedUsage
        }
        safety := !force && compaction.ShouldCompact(safetyUsage, window, settings)
        gated := !force &&
                compaction.ShouldCompactGated(estimatedUsage, window, engine.lastCompactEstimate, settings)
        // Both are checked before PrepareCompact so a no-op does not burn work.
        // force skips both triggers, so the guard must also require !force —
        // otherwise the overflow-recovery path (force=true) would no-op here and
        // burn its single retry on a second overflow.
        if !force && !safety && !gated {
                return false, nil
        }
        prep, err := compaction.PrepareCompact(engine.session.PathEntries(), settings)
        if err != nil {
                return false, err
        }
        if prep.FirstKeptEntryId == "" {
                return false, nil
        }
        if force && len(prep.MessagesToSummarize) == 0 && len(prep.TurnPrefixMessages) == 0 {
                return false, nil
        }
        // The window the settings derived from, so the summarizer's input
        // cap (SerializeConversationCapped) can keep the summary call
        // itself inside the window that triggered this compaction.
        prep.ContextWindow = window

        // Evidence record (Phase 4: evidence-driven recurrence). The ledger and
        // the turn contract rendered as harness-written state, appended to the
        // summary so the next recurrence round's context carries what is
        // verified rather than a transcript-shaped memory of it. Rendered here,
        // at fold time, so every compaction path (post-turn, overflow recovery,
        // /compact) emits the same record. Empty for sessions with no evidence
        // and no contract — plain chat compaction is unchanged.
        prep.EvidenceState = engine.renderEvidenceRecord()

        id := fmt.Sprintf("compaction-%d", time.Now().UnixNano())
        if !yield(session.CompactionStarted{}, nil) {
                return false, context.Canceled
        }

        comp, err := compaction.Compact(ctx, *prep, engine.client)
        if err != nil {
                _ = yield(session.CompactionComplete{ID: id, Failed: true}, nil)
                return false, err
        }
        if err := engine.session.AppendCompaction(comp); err != nil {
                _ = yield(session.CompactionComplete{ID: id, Failed: true}, nil)
                return false, err
        }
        // Episodic store (Bet 06): the compaction summary — which already
        // embeds the harness evidence record — becomes a retrievable
        // episode row in the vault, keyed by this project's scope.
        recordEpisode(engine, comp.Summary)
        // Track the post-compaction estimate so the next growth gate measures the
        // consumed increment since this compression, not the absolute size.
        engine.lastCompactEstimate = compaction.PostCompactionEstimate(settings)
        if !yield(session.CompactionComplete{ID: id, TokensBefore: comp.TokensBefore}, nil) {
                return false, context.Canceled
        }
        engine.extensions.EmitSessionCompact("auto")
        return true, nil
}

// ForceCompact runs a user-initiated compaction synchronously. It calls
// runCompact with force=true, which skips the safety-valve and growth-gate
// threshold checks but still no-ops when there is nothing to summarize (so
// a /compact on a fresh session is a no-op, not an error).
//
// emit receives the same CompactionStarted / CompactionComplete events the
// engine loop's yield does. Callers pass a callback that publishes them on
// the UI bus so the user sees the compaction happen.
//
// Returns (did, err): did is true when a compaction entry was appended,
// false when there was nothing to summarize or the user cancelled. err is
// non-nil on preparation or persistence failure.
//
// This is the entry point for the /compact slash command. The engine loop
// calls runCompact directly for its automatic post-turn and overflow paths;
// ForceCompact just exposes the same machinery to outside callers.
func (engine *Engine) ForceCompact(ctx context.Context, emit func(session.Event, error) bool) (bool, error) {
        return engine.runCompact(ctx, emit, 0, true, 0)
}

// streamTurn runs one assistant stream. On success it returns the final
// message and a StateComplete event (not yet yielded — caller checks tool
// budget first). On API/stream failure it returns err without yielding it
// so Loop can attempt overflow recovery. A nil event with nil err means
// cancel or the consumer stopped receiving.
func (engine *Engine) streamTurn(
        ctx context.Context,
        yield func(session.Event, error) bool,
        messages []llm.Message,
) (llm.Message, session.Event, error) {
        id := fmt.Sprintf("assistant-%d", time.Now().UnixNano())
        var thinking, text string
        var final llm.Message
        gotDone := false
        start := time.Now()

        for event, err := range engine.client.Stream(ctx, messages) {
                if err != nil {
                        if thinking != "" || text != "" {
                                _ = yield(emitMessage(id, session.StateError, session.StopNone, thinking, text, nil, llm.Usage{}), nil)
                        }
                        return llm.Message{}, nil, err
                }

                switch event.Type {
                case llm.StreamEventTypeError:
                        errText := event.Err
                        if errText == "" {
                                errText = "stream error"
                        }
                        return llm.Message{}, nil, fmt.Errorf("%s", errText)

                case llm.StreamEventTypeDelta:
                        if event.Delta.ReasoningContent != "" {
                                thinking += event.Delta.ReasoningContent
                        }
                        if event.Delta.Content != "" {
                                text += event.Delta.Content
                        }
                        if !yield(
                                emitMessage(id, session.StateStreaming, session.StopNone, thinking, text, nil, llm.Usage{}),
                                nil,
                        ) {
                                return llm.Message{}, nil, nil
                        }

                case llm.StreamEventTypeDone:
                        if event.Final == nil {
                                return llm.Message{}, nil, errors.New("agent: stream finished with no assistant message")
                        }
                        final = *event.Final
                        gotDone = true
                        // Prefer fully accumulated message for the complete event.
                        if final.ReasoningContent != "" {
                                thinking = final.ReasoningContent
                        }
                        if final.Content != "" {
                                text = final.Content
                        }
                }
        }

        if !gotDone {
                if ctx.Err() != nil {
                        _ = yield(emitMessage(id, session.StateCancelled, session.StopNone, thinking, text, nil, llm.Usage{}), nil)
                        return llm.Message{}, nil, nil
                }
                return llm.Message{}, nil, errors.New("agent: stream closed without assistant output")
        }

        reason := session.StopEndTurn
        if len(final.ToolCalls) > 0 {
                reason = session.StopToolUse
        }
        usage := session.TokenUsageFrom(final.Usage)
        if elapsed := time.Since(start).Seconds(); elapsed > 0 && usage.CompletionTokens > 0 {
                usage.TPS = float64(usage.CompletionTokens) / elapsed
        }
        complete := session.AssistantMessageUpdate{Message: session.ProjectAssistant(
                id,
                final,
                func(name, args string) string { return engine.ToolDetail(name, args) },
                session.StateComplete,
                reason,
                usage,
        )}
        return final, complete, nil
}

// ToolDetail resolves a friendly one-line detail for a tool call's raw JSON
// arguments via the tool's DetailFromArgs, matching the live executor. It is
// used both when building live tool_use blocks and by UI transcript replay so
// resumed sessions show the same detail (e.g. "read foo.go:10-20") instead of
// raw JSON. Returns "" when the tool is unknown or has no detail formatter.
func (engine *Engine) ToolDetail(name, args string) string {
        if engine == nil || engine.executor == nil {
                return ""
        }
        tool, ok := engine.executor.registry[name]
        if !ok || tool.DetailFromArgs == nil {
                return ""
        }
        return tool.DetailFromArgs(json.RawMessage(args))
}

func buildContent(thinking, text string, tools []session.ContentBlock) []session.ContentBlock {
        var out []session.ContentBlock
        if thinking != "" {
                out = append(out, session.ContentBlock{Type: session.BlockThinking, Text: thinking})
        }
        if text != "" {
                out = append(out, session.ContentBlock{Type: session.BlockText, Text: text})
        }
        out = append(out, tools...)
        return out
}

func emitMessage(
        id string,
        state session.State,
        reason session.StopReason,
        thinking,
        text string,
        tools []session.ContentBlock,
        usage llm.Usage,
) session.Event {
        return session.AssistantMessageUpdate{Message: session.Message{
                ID:         id,
                State:      state,
                StopReason: reason,
                Content:    buildContent(thinking, text, tools),
                Text:       text,
                Usage:      session.TokenUsageFrom(usage),
        }}
}

// pendingSkillsInstruction tells the model to read SKILL.md files for the
// selected skills (reuse the read tool, no dedicated skill tool).
func pendingSkillsInstruction(skillPath string, names []string) string {
        if len(names) == 0 {
                return ""
        }
        list, err := skills.LoadSkills(skillPath)
        targets := make([]string, 0, len(names))
        if err == nil {
                for _, name := range names {
                        if s := skills.Find(list, name); s != nil && s.SkillFilePath != "" {
                                targets = append(targets, s.SkillFilePath)
                                continue
                        }
                        targets = append(targets, name)
                }
        } else {
                targets = append(targets, names...)
        }
        return fmt.Sprintf(
                "You MUST read these skill files first with the read tool and follow them: %s. Do this immediately before responding.",
                strings.Join(targets, ", "),
        )
}
