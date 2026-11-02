package controller

import (
        "context"
        "errors"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "sync/atomic"
        "time"

        ext "github.com/damien1141/a1/ext/go"
        "github.com/damien1141/a1/internal/agent"
        "github.com/damien1141/a1/internal/components/toast"
        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/mcp"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/project/model"
        "github.com/damien1141/a1/internal/schedule"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools/tooldef"
        "github.com/damien1141/a1/internal/weather"

        "github.com/damien1141/a1/internal/vault"
)

const defaultAskTimeoutSec = 120

// EngineController owns agent.Engine lifecycle and stream cancellation.
// It talks to the UI only by publishing Msg values onto the Bus.
type EngineController struct {
        engine *agent.Engine
        proj   *project.Project

        // vault is the one durable store (Bet 08) when it booted; nil =
        // degraded file layout.
        vault *vault.Vault

        streamMu     sync.Mutex
        streamCancel context.CancelFunc
        streamGen    int

        bus *Bus

        sessionDir string
        cwd        string
        modelCfg   llm.ModelConfig
        // roleModels maps sub-agent role → model name. Empty → inherit modelCfg.
        roleModels map[job.Role]string
        jobs       *job.Manager
        // scheduler is the Clock (Bet 01): durable wake-ups that fire jobs
        // while this surface lives and persist for the daemon (a1 serve).
        scheduler *schedule.Scheduler
        unsubJobs func()

        gate          permission.Gate
        askTimeoutSec int
        allowAll      atomic.Bool
        agentsEnabled atomic.Bool
        memoryEnabled atomic.Bool
        // maxToolCalls mirrors config.Tools.MaxToolCalls (0 = unlimited). The
        // palette updates it live: it rebinds the running engine and persists
        // to config.yaml in one step.
        maxToolCalls atomic.Int64
        extRunner    atomic.Pointer[extension.Runner]
        mcpPool      *mcp.Pool
        memStore     *memstore.Store // opened lazily when MemoryEnabled() turns on

        // personaStore + weatherSvc back the agent's persona framing and weekly
        // forecast context injections. They are constructed once in
        // NewController from the project's global layout and updated in place
        // when the config file changes (the underlying services cache).
        personaStore *persona.Store
        weatherSvc   *weather.Service

        lastJobProgress sync.Map // job slot key → last published signature
}

// NewController returns a live EngineController. proj must be non-nil.
// Failure returns (nil, err), never a half-initialized value.
func NewController(bus *Bus, proj *project.Project, cwd string) (*EngineController, error) {
        if bus == nil {
                return nil, errors.New("tui: nil bus")
        }
        if proj == nil {
                return nil, errors.New("tui: nil project")
        }
        if strings.TrimSpace(cwd) == "" {
                var err error
                cwd, err = os.Getwd()
                if err != nil {
                        return nil, fmt.Errorf("tui: getwd: %w", err)
                }
        }

        if err := proj.LoadConfig(); err != nil {
                return nil, err
        }
        config := proj.Config()

        c := &EngineController{
                bus:           bus,
                proj:          proj,
                cwd:           cwd,
                sessionDir:    proj.SessionDir(),
                askTimeoutSec: defaultAskTimeoutSec,
                modelCfg:      config.Model(),
                roleModels:    make(map[job.Role]string),
        }
        // Persona + weather: attach to the controller so every engine opened from
        // here on inherits the same persona store + weather service. The weather
        // service is rebuilt from config on every LoadConfig so toggling the
        // enabled flag in the config editor takes effect on the next turn.
        c.personaStore = persona.NewStore(proj.Global().PersonasDir())
        // Bet 08 — the Vault: one durable store under ~/.a1/a1.db. Boot runs
        // the one-shot migration from the legacy file layout; a boot failure
        // degrades to the file layout (sessions JSONL, personas.json, &c).
        if v, err := vault.Boot(proj.Global().Root()); err != nil {
                debuglog.Logf("vault: boot degraded to file layout: %v", err)
        } else {
                c.vault = v
                session.SetDefaultVault(v)
                c.personaStore.AttachVault(v)
        }
        c.weatherSvc = weather.NewService(weatherConfigFromProject(proj), 0, nil)
        // TUI defaults to bypass; palette → settings → permissions toggles it.
        c.allowAll.Store(true)

        c.initGate(config.Permissions)
        c.agentsEnabled.Store(config.Agents.Enabled)
        c.maxToolCalls.Store(int64(config.Tools.MaxToolCalls))
        if s := config.Agents.Models.Explore; s != "" {
                c.roleModels[job.RoleExplore] = s
        }
        if s := config.Agents.Models.Review; s != "" {
                c.roleModels[job.RoleReview] = s
        }
        if s := config.Agents.Models.Worker; s != "" {
                c.roleModels[job.RoleWorker] = s
        }

        extRunner := loadExtensions(proj)
        c.extRunner.Store(extRunner)
        c.bindExtensionHost(extRunner)

        jobs, err := agent.NewJobManager(proj.JobsDir(), c.vault.DB(), c.modelCfg, c.modelForRole, c.Extensions, c.parentGate)
        if err != nil {
                return nil, err
        }
        c.jobs = jobs

        // The Clock (Bet 01): schedules persisted in the vault, config-declared
        // ones synced in, fires spawning jobs through the fleet's manager —
        // wake-ups reuse its backpressure and run headless-strict by default.
        // The ticker runs for the process lifetime; TUI exit stops it with the
        // process, and the rows survive for the a1 serve daemon.
        if sched, serr := schedule.New(c.vault.DB(), func(ctx context.Context, sc schedule.Schedule) error {
                role, rerr := job.ParseRole(sc.Role)
                if rerr != nil {
                        role = job.RoleWorker
                }
                _, serr := jobs.Spawn(ctx, job.SpawnRequest{
                        Prompt:      sc.Prompt,
                        Description: sc.Description,
                        ParentID:    "schedule:" + sc.ID,
                        Role:        role,
                        WorkDir:     sc.WorkDir,
                        WarmFrom:    "",
                        Contract:    sc.Contract,
                        Timeout:     sc.Timeout,
                        Kind:        job.KindSchedule,
                        ScheduleID:  sc.ID,
                })
                return serr
        }, schedule.WithLogger(func(format string, args ...any) {
                debuglog.Logf(format, args...)
        })); serr != nil {
                debuglog.Logf("schedule: scheduler unavailable: %v", serr)
        } else {
                if serr := sched.Sync(context.Background(), configScheduleDefs(c.proj)); serr != nil {
                        debuglog.Logf("schedule: config sync: %v", serr)
                }
                c.scheduler = sched
                go sched.Run(context.Background())
        }

        if pool, err := mcp.LoadPool(proj.MCPConfigFile()); err != nil {
                debuglog.Logf("mcp: load: %v", err)
        } else {
                c.mcpPool = pool
        }

        eng, err := c.openEngine(c.modelCfg, extRunner, "")
        if err != nil {
                return nil, err
        }
        c.engine = eng
        c.startJobProgress()
        c.emitSessionStart("startup", eng.SessionID(), "")
        return c, nil
}

func (c *EngineController) startJobProgress() {
        ch, cancel := c.jobs.Subscribe()
        c.unsubJobs = cancel
        go func() {
                for p := range ch {
                        if c.shouldPublishJobProgress(p) {
                                c.publish(JobProgressMsg{Progress: p})
                        }
                }
        }()
}

func (c *EngineController) shouldPublishJobProgress(p job.Progress) bool {
        key := p.JobID + "\x00" + p.ToolUseID
        if p.ToolUseID == "" {
                key = p.JobID + "\x00" + p.Name + "\x00" + p.Detail
        }
        sig := p.Status + "\x00" + p.Name + "\x00" + p.Detail
        if prev, ok := c.lastJobProgress.Load(key); ok && prev.(string) == sig {
                return false
        }
        c.lastJobProgress.Store(key, sig)
        return true
}

func (c *EngineController) initGate(policy permission.Policy) {
        if policy.AskTimeoutSec > 0 {
                c.askTimeoutSec = policy.AskTimeoutSec
        }
        if policy.Mode == "" {
                policy.Mode = permission.ModeInteractive
        }
        if policy.DangerouslyAllowAll {
                c.allowAll.Store(true)
        }
        // Do not clear allowAll when config omits dangerously_allow_all — TUI defaults
        // to bypass, and the palette toggle must survive SetModel / re-init.
        var inner permission.Gate
        inner, err := permission.NewGateAt(policy, c.cwd)
        if err != nil {
                inner, err = permission.NewGate(permission.DefaultPolicy(), permission.WorkspaceRoot())
        }
        if err != nil {
                inner = permission.AllowAll{}
        }
        c.gate = &permission.SessionAllowGate{Inner: inner, Enabled: &c.allowAll}
}

func (c *EngineController) PermissionMode() permission.Mode {
        if c.gate == nil || c.proj == nil {
                return permission.ModeInteractive
        }
        return permission.ModeOf(c.gate)
}

// ToolSchemaBytes returns the registered tool definitions' combined serialized
// size and count, for the /context breakdown.
func (c *EngineController) ToolSchemaBytes() (int, int) {
        if c == nil || c.engine == nil {
                return 0, 0
        }
        return c.engine.ToolSchemaBytes()
}

// PermissionGate returns the inner StaticGate for APPA inspection.
func (c *EngineController) PermissionGate() *permission.StaticGate {
        if c.gate == nil {
                return nil
        }
        if sg, ok := c.gate.(*permission.SessionAllowGate); ok {
                if inner, ok := sg.Inner.(*permission.StaticGate); ok {
                        return inner
                }
        }
        return nil
}

func (c *EngineController) CyclePermissionMode() {
        modes := permission.AllModes
        current := c.PermissionMode()
        idx := 0
        for i, m := range modes {
                if m == current {
                        idx = (i + 1) % len(modes)
                        break
                }
        }
        c.SetPermissionMode(modes[idx])
}

func (c *EngineController) CycleThinkLevel() {
        modes := llm.AllThinkModes()
        current := c.ThinkLevel()
        idx := 0
        for i, m := range modes {
                if m == current {
                        idx = (i + 1) % len(modes)
                        break
                }
        }
        c.SetThinkLevel(modes[idx])
        c.publish(ModelChangeMsg{Kind: "think_level", Value: string(modes[idx])})
        c.publish(
                ToastMsg{Message: "Thinking mode: " + string(modes[idx]), Kind: toast.ToastSuccess, Duration: 2 * time.Second},
        )
}

func (c *EngineController) SetPermissionMode(mode permission.Mode) {
        if c.proj == nil || c.gate == nil {
                return
        }
        policy := c.proj.Config().Permissions
        policy.Mode = mode
        // Preserve the session-tuned ask timeout: initGate stored it on the
        // controller, but rebuilding the gate from the config would drop it back
        // to the default on every mode switch.
        if c.askTimeoutSec > 0 {
                policy.AskTimeoutSec = c.askTimeoutSec
        }
        // Preserve APPA trajectory state across mode switches.
        var prevTrajectory *permission.TrajectoryState
        if sg, ok := c.gate.(*permission.SessionAllowGate); ok {
                if inner, ok := sg.Inner.(*permission.StaticGate); ok {
                        prevTrajectory = inner.Trajectory()
                }
        }
        var inner permission.Gate
        var err error
        inner, err = permission.NewGateAt(policy, c.cwd)
        if err != nil {
                inner, err = permission.NewGate(permission.DefaultPolicy(), permission.WorkspaceRootAt(c.cwd))
        }
        if err != nil {
                inner = permission.AllowAll{}
        }
        if prevTrajectory != nil {
                if sg, ok := inner.(*permission.StaticGate); ok {
                        sg.SetTrajectory(prevTrajectory)
                }
        }
        c.gate = &permission.SessionAllowGate{Inner: inner, Enabled: &c.allowAll}
        if c.engine != nil {
                c.engine.SetPermission(c.gate, c.askPermission)
        }
        c.publish(
                ToastMsg{Message: "Permission mode: " + string(mode), Kind: toast.ToastSuccess, Duration: 2 * time.Second},
        )
}

func (c *EngineController) SetAllowAll(v bool) {
        c.allowAll.Store(v)
}

func (c *EngineController) SetAgentsEnabled(v bool) {
        c.agentsEnabled.Store(v)
        if c.engine != nil {
                c.engine.SetJobs(c.engineJobs())
                c.engine.SetScheduler(c.engineScheduler())
        }
}

// SetMemoryEnabled toggles the agent memory system. Default is off
// — memory engineering (memstore, memfsck, activation-based retrieval) must be
// explicitly enabled by the user via the Ctrl+K palette.
//
// WHERE the toggle writes depends on the active persona, mirroring the web
// surface: a persona carrying an explicit memory_enabled owns the setting
// (the toggle edits the persona, persisted to the store), while a persona
// without one inherits the global knob. Either way the live engine is
// re-bound against the resulting effective state — before this fix the
// toggle only moved an atomic nobody read back, so flipping it mid-session
// changed nothing until a brand-new engine was built.
func (c *EngineController) SetMemoryEnabled(v bool) {
        if c == nil {
                return
        }
        if c.personaStore != nil {
                if p, err := c.personaStore.ActivePersona(); err == nil && p.MemoryEnabled != nil {
                        if _, err := c.personaStore.SetSettings(p.ID, nil, nil, &v); err != nil {
                                debuglog.Logf("memory: persist persona memory setting: %v", err)
                        }
                        c.applyMemoryToEngine()
                        return
                }
        }
        c.memoryEnabled.Store(v)
        c.applyMemoryToEngine()
}

// applyMemoryToEngine rebinds the live engine's memory surface to the
// effective memory state. The raw global knob is pushed first
// (SetMemoryEnabled on the engine); the engine then folds it with the
// ACTIVE persona's memory_enabled in Engine.MemoryEnabled(), and the shared
// bank attaches or detaches to match — remember/recall tools, the per-turn
// inlet injection, and the background outlet consolidation all follow the
// same value. Every memory-touching path (palette toggle, persona switch,
// persona edit) funnels through this one call.
func (c *EngineController) applyMemoryToEngine() {
        if c == nil || c.engine == nil {
                return
        }
        c.engine.SetMemoryEnabled(c.memoryEnabled.Load())
        if c.engine.MemoryEnabled() {
                c.engine.SetMemoryStore(c.openMemoryStore())
        } else {
                c.engine.SetMemoryStore(nil)
        }
}

// MaxToolCalls returns the per-turn tool-call budget (0 = unlimited) shown
// in the palette and enforced by the engine. Yolo mode overrides it at
// enforcement time, so the stored value is untouched by mode switches.
func (c *EngineController) MaxToolCalls() int {
        if c == nil {
                return 0
        }
        return int(c.maxToolCalls.Load())
}

// SetMaxToolCalls updates the per-turn tool-call budget (n <= 0 = unlimited):
// it rebinds the live engine immediately (mid-loop changes take effect at the
// next tool round) and persists the choice to config.yaml so it survives
// restarts. The engine ignores the cap while yolo mode is active.
func (c *EngineController) SetMaxToolCalls(n int) error {
        if c == nil {
                return nil
        }
        if n < 0 {
                n = 0
        }
        c.maxToolCalls.Store(int64(n))
        if c.engine != nil {
                c.engine.SetMaxToolCalls(n)
        }
        if c.proj == nil {
                return nil
        }
        return project.SetMaxToolCalls(c.proj.Global(), n)
}

// MemoryEnabled reports whether the agent memory system is effectively
// active: the ACTIVE persona's explicit memory_enabled wins when set,
// otherwise the global Ctrl+K toggle. When false, the memory tools
// (remember, recall, revise, forget) are not registered and per-turn
// retrieval/injection + background consolidation are skipped.
func (c *EngineController) MemoryEnabled() bool {
        if c == nil {
                return false
        }
        if c.personaStore != nil {
                if p, err := c.personaStore.ActivePersona(); err == nil && p.MemoryEnabled != nil {
                        return *p.MemoryEnabled
                }
        }
        return c.memoryEnabled.Load()
}

// openMemoryStore lazily opens the SQLite memory store at
// ~/.a1/agent/memories.db when the memory system is enabled. Returns
// nil when memory is off (default) — the engine sees nil and skips
// memory tool registration entirely.
//
// The embedder is selected from the project config's semantic_search
// section: if embedding_model is set (typically "nomic-embed-text"),
// the OllamaAdapter wraps the existing OllamaEmbedder for use with
// the memory store. When semantic search isn't configured (no
// embedding_model), the FakeEmbedder is used — the activation math
// (power-law forgetting, encoding specificity, salience) still works,
// just with weaker semantic matching (bag-of-words instead of
// real embeddings).
func (c *EngineController) openMemoryStore() *memstore.Store {
        if !c.MemoryEnabled() {
                return nil
        }
        if c.memStore != nil {
                return c.memStore
        }
        // Bet 08: the shared bank is a namespaced family inside the vault
        // when it booted (the old ~/.a1/agent/memories.db was imported by
        // the migration); the file store remains the fallback.
        if c.vault != nil {
                emb := c.openEmbedder()
                store, err := memstore.OpenIn(c.vault.DB(), vault.SharedNS(), emb, memstore.DefaultParams())
                if err != nil {
                        debuglog.Logf("memory: cannot open vault store: %v", err)
                        return nil
                }
                c.memStore = store
                return store
        }
        home, err := os.UserHomeDir()
        if err != nil {
                debuglog.Logf("memory: cannot get home dir: %v", err)
                return nil
        }
        agentDir := filepath.Join(home, ".a1", "agent")
        if err := os.MkdirAll(agentDir, 0o755); err != nil {
                debuglog.Logf("memory: cannot create agent dir: %v", err)
                return nil
        }
        dbPath := filepath.Join(agentDir, "memories.db")

        // Try the Ollama embedder from the project config's semantic_search
        // section. Falls back to FakeEmbedder when Ollama isn't configured.
        var emb memstore.Embedder = memstore.FakeEmbedder{DimCount: 64}
        if c.proj != nil && c.proj.Config() != nil {
                ss := c.proj.Config().SemanticSearch
                if ollamaEmb := memstore.TryOllamaFromConfig(ss.OllamaBaseURL, ss.EmbeddingModel); ollamaEmb != nil {
                        emb = ollamaEmb
                        debuglog.Logf("memory: using Ollama embedder (model=%s, base=%s)", ss.EmbeddingModel, ss.OllamaBaseURL)
                } else {
                        debuglog.Logf("memory: Ollama not configured, using FakeEmbedder")
                }
        }

        store, err := memstore.Open(dbPath, emb, memstore.DefaultParams())
        if err != nil {
                debuglog.Logf("memory: cannot open store: %v", err)
                return nil
        }
        c.memStore = store
        return store
}

// openEmbedder returns the embedder used to open per-persona semantic
// memory stores. Mirrors the embedder selection in openMemoryStore() —
// Ollama when the project config has semantic_search configured,
// FakeEmbedder otherwise. Cached on first call so we don't re-probe
// Ollama on every per-persona store open.
//
// Unlike openMemoryStore(), this does NOT cache the embedder on the
// controller struct today (the persona.Store does that caching
// internally keyed by persona ID, so re-calling openEmbedder per
// persona switch is fine). A future optimization could memoize it
// here, but the cost is one config lookup per switch — negligible.
func (c *EngineController) openEmbedder() memstore.Embedder {
        if c.proj != nil && c.proj.Config() != nil {
                ss := c.proj.Config().SemanticSearch
                if ollamaEmb := memstore.TryOllamaFromConfig(ss.OllamaBaseURL, ss.EmbeddingModel); ollamaEmb != nil {
                        return ollamaEmb
                }
        }
        return memstore.FakeEmbedder{DimCount: 64}
}

// resolvePersonaModel looks up a persona's configured model name in the
// project's model catalog. Returns (zero, false) when the name is
// unknown or the project isn't configured — the engine keeps its
// previous modelCfg in that case (graceful degradation: a misnamed
// persona.Model should not break the persona switch).
//
// Wired to the engine via WithPersonaModelResolver so SetPersona can
// apply the active persona's Model field.
func (c *EngineController) resolvePersonaModel(name string) (llm.ModelConfig, bool) {
        if c.proj == nil || c.proj.Config() == nil {
                return llm.ModelConfig{}, false
        }
        return c.proj.Config().FindModel(name)
}

func (c *EngineController) modelForRole(role job.Role) llm.ModelConfig {
        role = job.NormalizeRole(string(role))
        if name := c.roleModels[role]; name != "" {
                if m, ok := c.proj.Config().FindModel(name); ok {
                        return m
                }
                debuglog.Logf("agents: unknown role model %q for %s; using parent", name, role)
        }
        return c.modelCfg
}

// SetRoleModel sets the session-only model name for a sub-agent role.
// Empty name clears the override (inherit parent). Name must exist in config.
func (c *EngineController) SetRoleModel(role, name string) error {
        r, err := job.ParseRole(role)
        if err != nil {
                return err
        }
        name = strings.TrimSpace(name)
        if name == "" {
                delete(c.roleModels, r)
                return nil
        }
        if _, ok := c.proj.Config().FindModel(name); !ok {
                return fmt.Errorf("unknown model %q", name)
        }
        c.roleModels[r] = name
        return nil
}

func (c *EngineController) engineJobs() *job.Manager {
        if !c.agentsEnabled.Load() {
                return nil
        }
        return c.jobs
}

// engineScheduler mirrors engineJobs: the Clock's tool registers only when
// sub-agents are enabled (a fire IS a job).
func (c *EngineController) engineScheduler() *schedule.Scheduler {
        if !c.agentsEnabled.Load() {
                return nil
        }
        return c.scheduler
}

// configScheduleDefs reads config.yaml's schedules block, logging (not
// failing) on a malformed trigger — the TUI must still boot.
func configScheduleDefs(proj *project.Project) []schedule.Schedule {
        if proj == nil || proj.Config() == nil {
                return nil
        }
        defs, err := proj.Config().ScheduleDefs()
        if err != nil {
                debuglog.Logf("schedule: config: %v", err)
                return nil
        }
        return defs
}

// parentGate returns the parent trajectory's gate for a child branch to fork
// from (L0c := Lp). It resolves through engine.Gate so the child inherits the
// live gate, including a mode switch that happened after startup.
func (c *EngineController) parentGate() permission.Gate {
        if c == nil || c.engine == nil {
                return c.gate
        }
        return c.engine.Gate()
}

// ConfigPath returns the path of the config file this session loaded, so
// /config edits the file that is actually read rather than a default that
// happens to exist.
func (c *EngineController) ConfigPath() string {
        if c == nil || c.proj == nil {
                return ""
        }
        return c.proj.Global().ConfigFile()
}

func (c *EngineController) Extensions() *extension.Runner {
        return c.extRunner.Load()
}

func (c *EngineController) ReloadExtensions() (loaded int, warns []extension.Warning, err error) {
        if c.proj == nil {
                return 0, nil, errors.New("project not available")
        }
        r, warns, err := extension.Load(c.proj.Global().ExtensionsDir(), c.proj.ExtensionsDir())
        if err != nil {
                return 0, warns, err
        }
        logExtensionWarnings(warns)
        c.swapExtensionRunner(r)
        if c.engine != nil {
                c.engine.SetExtensions(r)
        }
        if r == nil {
                return 0, warns, nil
        }
        return len(r.Loaded()), warns, nil
}

func (c *EngineController) swapExtensionRunner(r *extension.Runner) {
        if prev := c.extRunner.Swap(r); prev != nil {
                prev.Close()
        }
        c.bindExtensionHost(r)
        // Previous runner's UI state died with its subprocesses.
        c.publish(ExtSessionEffectsMsg{Status: "", StatusSet: true})
}

func (c *EngineController) ListExtensions() ([]extension.Discovered, []extension.Warning, error) {
        if c.proj == nil {
                return nil, nil, errors.New("project not available")
        }
        return extension.Discover(c.proj.Global().ExtensionsDir(), c.proj.ExtensionsDir())
}

// loadExtensions discovers ~/.a1/extensions and <cwd>/.a1/extensions.
// Load errors are non-fatal (fail-open: no extensions).
func loadExtensions(proj *project.Project) *extension.Runner {
        if proj == nil {
                return nil
        }
        r, warns, err := extension.Load(proj.Global().ExtensionsDir(), proj.ExtensionsDir())
        if err != nil {
                debuglog.Logf("extension: load failed: %v", err)
                return nil
        }
        logExtensionWarnings(warns)
        return r
}

func logExtensionWarnings(warns []extension.Warning) {
        for _, w := range warns {
                debuglog.Logf("extension: %s", w.String())
        }
}

func (c *EngineController) bindExtensionHost(r *extension.Runner) {
        if r == nil {
                return
        }
        cwd, sessionID := "", ""
        if c.engine != nil {
                cwd = c.engine.SessionCwd()
                sessionID = c.engine.SessionID()
        } else if c.proj != nil {
                cwd = c.proj.Root()
        }
        r.Bind(ext.HostOpts{
                UI: extension.BusUI{
                        NotifyFn: func(message, kind string) {
                                toastKind := toast.ToastSuccess
                                switch strings.ToLower(kind) {
                                case "warning":
                                        toastKind = toast.ToastWarning
                                case "error":
                                        toastKind = toast.ToastError
                                }
                                c.publish(ToastMsg{Message: message, Kind: toastKind, Duration: 3 * time.Second})
                        },
                        SetStatusFn: func(_, text string) {
                                c.publish(ExtSessionEffectsMsg{Status: text, StatusSet: true})
                        },
                        ConfirmFn: c.askExtConfirm,
                },
                Cwd:       cwd,
                SessionID: sessionID,
                HasUI:     true,
                RefreshTools: func() {
                        if c.engine != nil {
                                c.engine.SetExtensions(r)
                        }
                },
                SendUserMessage: func(text string) {
                        go c.StartPrompt(text, nil, nil)
                },
        })
}

func (c *EngineController) askTimeout() time.Duration {
        if c.askTimeoutSec > 0 {
                return time.Duration(c.askTimeoutSec) * time.Second
        }
        return time.Duration(defaultAskTimeoutSec) * time.Second
}

func waitReply[T any](ctx context.Context, ch <-chan T, timeout time.Duration, onAbort func()) (T, error) {
        var zero T
        timer := time.NewTimer(timeout)
        defer timer.Stop()
        select {
        case r := <-ch:
                return r, nil
        case <-ctx.Done():
                onAbort()
                return zero, ctx.Err()
        case <-timer.C:
                onAbort()
                return zero, nil
        }
}

func (c *EngineController) askPermission(
        ctx context.Context,
        req permission.Request,
        reason string,
) (permission.AskResult, error) {
        if c.allowAll.Load() {
                return permission.AskResult{Approved: true}, nil
        }
        reply := make(chan AskReply, 1)
        c.publish(OverlayMsg{Kind: OverlayPermissionAsk, Request: req, Reason: reason, PermReply: reply})
        r, err := waitReply(ctx, reply, c.askTimeout(), func() {
                c.publish(OverlayMsg{Kind: OverlayPermissionDismiss})
        })
        if err != nil {
                return permission.AskResult{}, err
        }
        if r.AllowSession {
                c.allowAll.Store(true)
        }
        if r.AllowlistPattern != "" && c.proj != nil {
                _ = project.AppendBashAllow(c.proj.Global(), r.AllowlistPattern)
                if sg := c.PermissionGate(); sg != nil {
                        _, _ = sg.AppendBashAllow(r.AllowlistPattern)
                }
        }
        return permission.AskResult{Approved: r.Approved, Feedback: r.Feedback}, nil
}

// AskQuestion shows a structured question overlay and blocks until the user
// answers. This is the AskUserQuestion transport: the same modal-blocking shape
// as a permission prompt, but nothing is being approved — an answer is being
// collected and handed back to the model as the tool result.
//
// A cancelled or timed-out question returns Cancelled rather than an empty
// choice, so the caller can tell "user skipped" from "user said nothing".
func (c *EngineController) AskQuestion(
        ctx context.Context,
        q Question,
) (QuestionReply, error) {
        if len(q.Options) == 0 {
                return QuestionReply{}, errors.New("ask: question needs at least one option")
        }
        reply := make(chan QuestionReply, 1)
        c.publish(OverlayMsg{Kind: OverlayQuestionAsk, Question: &q, QuestReply: reply})
        r, err := waitReply(ctx, reply, c.askTimeout(), func() {
                c.publish(OverlayMsg{Kind: OverlayQuestionDismiss})
        })
        if err != nil {
                return QuestionReply{}, err
        }
        return r, nil
}

// askUserQuestion bridges the AskUserQuestion tool to the overlay, adapting the
// tool-def shape to the controller's own message type. The types stay distinct
// so the tool layer never imports the TUI.
func (c *EngineController) askUserQuestion(
        ctx context.Context,
        q tooldef.Question,
) (tooldef.QuestionReply, error) {
        question := Question{Header: q.Header, Question: q.Question, MultiSelect: q.MultiSelect}
        for _, o := range q.Options {
                question.Options = append(question.Options, QuestionOption{Label: o.Label, Description: o.Description})
        }
        res, err := c.AskQuestion(ctx, question)
        if err != nil {
                return tooldef.QuestionReply{}, err
        }
        return tooldef.QuestionReply{Choice: res.Choice, Choices: res.Choices, Cancelled: res.Cancelled}, nil
}

func (c *EngineController) askContinue(ctx context.Context, maxRounds int) (bool, error) {
        reply := make(chan ContinueReply, 1)
        c.publish(OverlayMsg{Kind: OverlayContinueAsk, MaxRounds: maxRounds, ContReply: reply})
        r, err := waitReply(ctx, reply, c.askTimeout(), func() {
                c.publish(OverlayMsg{Kind: OverlayContinueDismiss})
        })
        if err != nil {
                return false, err
        }
        return r.Continue, nil
}

func (c *EngineController) askExtConfirm(req ext.ConfirmRequest) ext.ConfirmReply {
        reply := make(chan ExtConfirmReply, 1)
        c.publish(OverlayMsg{
                Kind:         OverlayExtConfirm,
                Title:        req.Title,
                Message:      req.Message,
                Yes:          req.Yes,
                No:           req.No,
                Danger:       req.Danger,
                ConfirmReply: reply,
        })
        r, _ := waitReply(context.Background(), reply, c.askTimeout(), func() {
                c.publish(OverlayMsg{Kind: OverlayExtConfirmDismiss})
        })
        return ext.ConfirmReply{OK: r.OK}
}

func (c *EngineController) SetModel(name string) error {
        name = strings.TrimSpace(name)
        if name == "" {
                return errors.New("empty model name")
        }
        if c.proj == nil {
                return errors.New("project not available")
        }
        if err := c.proj.LoadConfig(); err != nil {
                return err
        }
        cfg, ok := c.proj.Config().FindModel(name)
        if !ok {
                // Not a configured model: keep the primary's connection settings and
                // only swap the name (arbitrary-model workflow).
                cfg = c.proj.Config().Model()
                cfg.Name = name
        }
        c.Cancel()
        c.initGate(c.proj.Config().Permissions)
        if c.engine == nil {
                return errors.New("agent not configured")
        }
        c.engine.SetPermission(c.gate, c.askPermission)
        c.engine.SetContinueAsk(c.askContinue)
        c.engine.SetJobs(c.engineJobs())
        c.engine.SetScheduler(c.engineScheduler())
        if _, _, err := c.ReloadExtensions(); err != nil {
                debuglog.Logf("extension: reload on SetModel: %v", err)
        }
        // Resolve the "default" sentinel to the preset's thinking config before
        // handing cfg to the engine; modelCfg keeps the sentinel for display.
        c.engine.SetModelWithHooks(c.resolveThink(cfg), model.HooksFor(cfg.Name))
        c.modelCfg = cfg
        // Announce the switch so every surface re-scales: the composer label,
        // the footer's context-fill denominator (the new model's window), and
        // anything else keyed on the active model. Without this the ctx label
        // kept the previous model's window until restart.
        c.publish(ModelChangeMsg{Kind: "model", Value: cfg.Name})
        return nil
}

// EngineContextWindow returns the live engine context window — the
// engine's resolved runtime value (config → probe → assumed → learned), not
// the static startup config, so surfaces tracking a mid-session switch (or
// a window learned from a provider overflow error) stay accurate.
func (c *EngineController) EngineContextWindow() int {
        if c == nil {
                return 0
        }
        if c.engine != nil {
                return c.engine.ContextWindow()
        }
        return c.modelCfg.ContextWindow
}

// SetThinkLevel changes the thinking level for the current session. The
// "default" sentinel is preserved on modelCfg (so the palette marks the
// default row active) and resolved to the preset's thinking config by the
// engine's client builder before any request is sent.
func (c *EngineController) SetThinkLevel(mode llm.ThinkMode) {
        c.modelCfg.Think.Mode = mode
        c.modelCfg.Think.Enabled = mode != llm.Off
        if c.engine != nil {
                c.engine.SetModel(c.resolveThink(c.modelCfg))
        }
        c.publish(ModelChangeMsg{Kind: "think_level", Value: string(mode)})
}

// ThinkLevel returns the current thinking mode.
func (c *EngineController) ThinkLevel() llm.ThinkMode {
        return c.modelCfg.Think.Mode
}

// ModelName returns the active model name.
func (c *EngineController) ModelName() string {
        return c.modelCfg.Name
}

func (c *EngineController) ImageEnabled() bool {
        return c.modelCfg.ImageEnabled
}

func (c *EngineController) SessionID() string {
        if c.engine == nil {
                return ""
        }
        return c.engine.SessionID()
}

func (c *EngineController) SessionDir() string {
        return c.sessionDir
}

func (c *EngineController) LiveJobCount() int {
        if c.jobs == nil {
                return 0
        }
        return c.jobs.LiveCount()
}

func (c *EngineController) SessionFile() string {
        if c.engine == nil {
                return ""
        }
        return c.engine.SessionFile()
}

func (c *EngineController) Resume(id string) (cwdWarning string, err error) {
        id = strings.TrimSpace(id)
        if id == "" {
                return "", errors.New("empty session id")
        }
        prevID, err := c.beginSessionSwitch("resume", id, true)
        if err != nil {
                return "", err
        }
        cfg, err := c.resolveModel()
        if err != nil {
                return "", err
        }

        extRunner := loadExtensions(c.proj)
        c.swapExtensionRunner(extRunner)
        eng, err := c.openEngine(cfg, extRunner, id)
        if err != nil {
                return "", err
        }
        if sessCwd := eng.SessionCwd(); sessCwd != "" && c.cwd != "" && sessCwd != c.cwd {
                cwdWarning = fmt.Sprintf("cwd %s ≠ %s", filepath.Base(sessCwd), filepath.Base(c.cwd))
        }
        c.engine = eng
        c.modelCfg = cfg
        c.emitSessionStart("resume", eng.SessionID(), prevID)
        return cwdWarning, nil
}

// Clear starts a brand-new persisted session. Caller must ensure no agent
// stream / local bash is in flight.
func (c *EngineController) Clear() error {
        prevID, err := c.beginSessionSwitch("new", "", false)
        if err != nil {
                return err
        }
        cfg, err := c.resolveModel()
        if err != nil {
                return err
        }
        engine, err := c.openEngine(cfg, c.Extensions(), "")
        if err != nil {
                return err
        }
        c.engine = engine
        c.modelCfg = cfg
        c.emitSessionStart("new", engine.SessionID(), prevID)
        return nil
}

func (c *EngineController) beginSessionSwitch(reason, targetID string, cancelStream bool) (prevID string, err error) {
        if c.sessionDir == "" {
                return "", errors.New("session directory not configured")
        }
        prevID = c.SessionID()
        out := c.sessionBeforeSwitch(reason, prevID, targetID)
        c.publishSessionEffects(out)
        if out.Denied {
                msg := out.Reason
                if msg == "" {
                        msg = "session switch denied by extension"
                }
                return "", errors.New(msg)
        }
        if cancelStream {
                c.Cancel()
        }
        c.sessionShutdown(reason, prevID)
        return prevID, nil
}

func (c *EngineController) resolveModel() (llm.ModelConfig, error) {
        if c.modelCfg.Name != "" {
                return c.modelCfg, nil
        }
        if c.proj == nil {
                return llm.ModelConfig{}, errors.New("project not available")
        }
        if err := c.proj.LoadConfig(); err != nil {
                return llm.ModelConfig{}, err
        }
        return c.proj.Config().Model(), nil
}

// resolveThink returns a copy of cfg with the "default" thinking sentinel
// resolved to the preset's thinking config, so the engine and its LLM client
// never see the sentinel. The controller's own modelCfg keeps the sentinel so
// the palette can mark the default row active.
func (c *EngineController) resolveThink(cfg llm.ModelConfig) llm.ModelConfig {
        cfg.Think = model.ResolveThink(cfg.Name, cfg.Think)
        return cfg
}

func (c *EngineController) openEngine(
        cfg llm.ModelConfig,
        extRunner *extension.Runner,
        resumeID string,
) (*agent.Engine, error) {
        cfg = c.resolveThink(cfg)
        opts := []agent.SessionOption{
                agent.WithCwd(c.cwd),
                agent.WithSessionDir(c.sessionDir),
                agent.WithPersist(true),
        }
        if c.personaStore != nil {
                opts = append(opts, agent.WithPersonaStore(c.personaStore))
        }
        if c.weatherSvc != nil {
                opts = append(opts, agent.WithWeather(c.weatherSvc))
        }
        if resumeID != "" {
                opts = append(opts, agent.WithResumeID(resumeID))
        }
        sess, err := agent.NewSession(opts...)
        if err != nil {
                return nil, err
        }
        return agent.NewEngine(cfg, sess,
                agent.WithGate(c.gate),
                agent.WithAsk(c.askPermission),
                agent.WithQuestion(c.askUserQuestion),
                agent.WithContinueAsk(c.askContinue),
                agent.WithJobs(c.engineJobs()),
                agent.WithScheduler(c.engineScheduler()),
                agent.WithExtensions(extRunner),
                agent.WithMCP(c.mcpPool),
                agent.WithHooks(model.HooksFor(cfg.Name)),
                agent.WithMemoryStore(c.openMemoryStore()),
                // Config-defined evidence checks (config.yaml `checks:`).
                agent.WithEvidenceChecks(c.proj.Config().EvidenceChecks()),
                // The RAW global toggle; the engine folds it with the active
                // persona's memory_enabled (Engine.MemoryEnabled) so a persona
                // that opts in gets the bank even with the knob off.
                agent.WithMemoryEnabled(c.memoryEnabled.Load()),
                agent.WithEmbedder(c.openEmbedder()),
                agent.WithPersonaModelResolver(c.resolvePersonaModel),
                agent.WithPermissionModeSetter(func(mode permission.Mode) {
                        c.SetPermissionMode(mode)
                }),
                // Restored when the next persona (or the default) has no
                // mode of its own — keeps persona overrides scoped.
                agent.WithBasePermissionMode(c.PermissionMode()),
                agent.WithMaxToolCalls(int(c.maxToolCalls.Load())),
        )
}

func (c *EngineController) ReplaySnapshot() session.Snapshot {
        if c.engine == nil || c.engine.Session() == nil {
                return session.Snapshot{}
        }
        return session.ReplaySnapshot(c.engine.Session().PathEntries(), c.engine.ToolDetail)
}

func (c *EngineController) StartPrompt(text string, pendingSkills []string, images []llm.Image) {
        ctx, cancel := context.WithCancel(context.Background())
        c.streamMu.Lock()
        if c.streamCancel != nil {
                c.streamCancel()
        }
        c.streamCancel = cancel
        c.streamGen++
        gen := c.streamGen
        c.streamMu.Unlock()

        go c.runLoop(ctx, gen, text, pendingSkills, images)
}

func (c *EngineController) Cancel() {
        c.streamMu.Lock()
        cancel := c.streamCancel
        c.streamMu.Unlock()
        if cancel != nil {
                cancel()
        }
}

// ErrNothingToCompact is returned by ForceCompact when there was nothing
// to summarize (e.g. /compact on a fresh session, or on a session that
// was just compacted and hasn't grown since). Callers should distinguish
// this from a real error: a no-op compaction is not a failure.
var ErrNothingToCompact = errors.New("nothing to compact")

// ForceCompact triggers a user-initiated context compaction. It runs the
// same compaction pipeline the engine loop uses after a turn, but
// synchronously and unconditionally (force=true — skips the threshold
// checks). CompactionStarted / CompactionComplete events are published on
// the bus so the transcript shows the compaction as it happens.
//
// Returns nil if a compaction entry was appended. Returns
// ErrNothingToCompact if there was nothing to summarize (a no-op, not a
// failure). Returns a different error only when preparation or persistence
// failed. The /compact slash command toasts the three cases differently
// so the user can tell "nothing to compact" from "compaction failed".
//
// Must not be called while a stream is active: compaction mutates the
// session tree (appends a CompactionEntry) and rewrites the context the
// engine is mid-turn building, which would race. The slash command guards
// against this with the StreamActive check.
func (c *EngineController) ForceCompact(ctx context.Context) error {
        if c == nil || c.engine == nil {
                return errors.New("compaction: engine not configured")
        }
        did, err := c.engine.ForceCompact(ctx, func(ev session.Event, _ error) bool {
                if c.bus != nil && ev != nil {
                        c.publish(SessionEventMsg{Event: ev})
                }
                return true
        })
        if err != nil {
                return err
        }
        if !did {
                return ErrNothingToCompact
        }
        return nil
}

func (c *EngineController) Close() {
        c.sessionShutdown("quit", c.SessionID())
        c.Cancel()
        if c.unsubJobs != nil {
                c.unsubJobs()
                c.unsubJobs = nil
        }
        if c.jobs != nil {
                _ = c.jobs.Close(context.Background())
        }
        if c.mcpPool != nil {
                _ = c.mcpPool.Close()
                c.mcpPool = nil
        }
        if prev := c.extRunner.Swap(nil); prev != nil {
                prev.Close()
        }
}

func (c *EngineController) sessionBeforeSwitch(reason, fromID, targetID string) ext.SessionEffects {
        r := c.Extensions()
        if r == nil {
                return ext.SessionEffects{}
        }
        r.SetMeta(fromID, c.cwd)
        return r.EmitSessionBeforeSwitch(ext.SessionBeforeSwitchEvent{
                Reason:          reason,
                TargetSessionID: targetID,
        })
}

func (c *EngineController) sessionShutdown(reason, sessionID string) {
        r := c.Extensions()
        if r == nil {
                return
        }
        r.SetMeta(sessionID, c.cwd)
        c.publishSessionEffects(r.EmitSessionShutdown(ext.SessionShutdownEvent{Reason: reason}))
}

func (c *EngineController) emitSessionStart(reason, sessionID, previousID string) {
        r := c.Extensions()
        if r == nil {
                return
        }
        r.SetMeta(sessionID, c.cwd)
        c.publishSessionEffects(r.EmitSessionStart(ext.SessionStartEvent{
                Reason:            reason,
                PreviousSessionID: previousID,
        }))
}

func (c *EngineController) publishSessionEffects(out ext.SessionEffects) {
        if out.Toast == "" && !out.StatusSet {
                return
        }
        c.publish(ExtSessionEffectsMsg{
                Toast:     out.Toast,
                Status:    out.Status,
                StatusSet: out.StatusSet,
        })
}

func (c *EngineController) alive(gen int) bool {
        c.streamMu.Lock()
        ok := c.streamGen == gen
        c.streamMu.Unlock()
        return ok
}

func (c *EngineController) waitOrDone(ctx context.Context, gen int, d time.Duration) bool {
        timer := time.NewTimer(d)
        defer timer.Stop()
        select {
        case <-ctx.Done():
                return false
        case <-timer.C:
        }
        return c.alive(gen)
}

func (c *EngineController) publish(m Msg) {
        if c.bus != nil {
                c.bus.Publish(m)
        }
}

func (c *EngineController) publishLoopError(gen int, errText string) {
        if !c.alive(gen) {
                return
        }
        c.publish(SessionEventMsg{Event: session.AssistantMessageUpdate{Message: session.Message{
                ID:    fmt.Sprintf("assistant-error-%d", time.Now().UnixNano()),
                State: session.StateError,
                Text:  errText,
                Content: []session.ContentBlock{
                        {Type: session.BlockText, Text: errText},
                },
        }}})
}

func (c *EngineController) runLoop(
        ctx context.Context,
        gen int,
        prompt string,
        pendingSkills []string,
        images []llm.Image,
) {
        if !c.waitOrDone(ctx, gen, 120*time.Millisecond) {
                return
        }
        c.publish(FooterMsg{Kind: FooterSetActivity, Activity: ActivityStreaming})

        if c.engine == nil {
                c.publishLoopError(gen, "agent not configured")
                return
        }

        agent.SetActiveEngine(c.engine)
        defer agent.SetActiveEngine(nil)
        for ev, err := range c.engine.Loop(ctx, prompt, agent.LoopOpts{
                PendingSkills: pendingSkills,
                Images:        images,
        }) {
                if !c.alive(gen) {
                        c.publish(FooterMsg{Kind: FooterTurnEnd})
                        return
                }
                if err != nil {
                        c.publishLoopError(gen, err.Error())
                        c.publish(FooterMsg{Kind: FooterTurnEnd})
                        return
                }
                if ev != nil {
                        c.publish(SessionEventMsg{Event: ev})
                }
        }
        c.publish(FooterMsg{Kind: FooterTurnEnd})
}
