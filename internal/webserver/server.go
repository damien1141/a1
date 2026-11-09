// Package webserver serves the htmx web chat UI for the a1 harness.
//
// It is a separate surface from the TUI: same agent engine, same session store,
// delivered over HTTP. The UI is a single HTML document (embedded) plus a small
// JSON/SSE API, so the binary stays self-contained and needs no build step or
// node toolchain on the user's machine.
//
// Concurrency model: one Engine per session, guarded by a per-session turn
// mutex, so a second prompt on the same session queues behind the first instead
// of interleaving two streams into one transcript.
package webserver

import (
        "context"
        "errors"
        "fmt"
        "io/fs"
        "net"
        "net/http"
        "net/url"
        "os"
        "strings"
        "sync"
        "time"

        "github.com/damien1141/a1/internal/agent"
        "github.com/damien1141/a1/internal/configserver"
        "github.com/damien1141/a1/internal/job"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/memstore"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/project/model"
        "github.com/damien1141/a1/internal/schedule"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools"

        "github.com/damien1141/a1/internal/vault"
)

// DefaultPort is where `a1 --web` listens.
const DefaultPort = 9001

const (
        readHeaderTimeout = 15 * time.Second
        // askTimeout bounds how long a permission prompt may sit unanswered
        // before the tool call is denied.
        askTimeout = 10 * time.Minute
)

// Server owns the HTTP listener and the live engine sessions.
type Server struct {
        proj     *project.Project
        page     []byte
        http     *http.Server
        hub      *hub
        stopOnce sync.Once

        // vault is the one durable store (Bet 08) when it booted; nil =
        // degraded file layout.
        vault *vault.Vault

        mu      sync.Mutex
        modes   *modeState
        state   *serverState
        pins    *pinnedSessions
        titles  *sessionTitles
        engines map[string]*liveEngine
        pending map[string]chan permission.AskResult

        // configMux guards the MCP config files' read-modify-write cycles
        // (list → add/remove → save): two concurrent browser tabs editing
        // servers must not interleave their saves.
        configMux sync.Mutex

        // personaStore backs the per-session persona context injection.
        // Constructed once at Start from the project's global layout.
        personaStore *persona.Store

        // memStore is the shared semantic memory bank (~/.a1/agent/
        // memories.db — the same file the TUI's memory toggle opens),
        // opened lazily when the settings panel enables agent memory.
        // nil = memory off (default).
        memStore *memstore.Store

        // jobs + scheduler are the Clock (Bet 01), wired only by StartServe:
        // engines built while they are set register agent_* and schedule tools.
        // Set once at boot before the listener starts, so engineOptions reads
        // them without extra locking.
        jobs      *job.Manager
        scheduler *schedule.Scheduler
}

// liveEngine is one engine plus the turn mutex that serializes prompts on it
// and the cancel func for the in-flight turn.
type liveEngine struct {
        engine *agent.Engine
        turn   sync.Mutex
        cancel context.CancelFunc
}

// Start launches the web server on 127.0.0.1:port and returns the page URL
// plus a stop function. The caller owns the goroutine lifetime; stop is
// idempotent and safe to call from a defer.
func Start(ctx context.Context, proj *project.Project, port int) (string, func(), error) {
        if ctx == nil {
                ctx = context.Background()
        }
        if port <= 0 || port > 65535 {
                return "", nil, fmt.Errorf("web: port %d out of range", port)
        }
        if proj == nil {
                return "", nil, errors.New("web: project is required")
        }
        srv, err := newServer(proj)
        if err != nil {
                return "", nil, err
        }
        return listenAndServe(ctx, srv, "127.0.0.1", port, srv.routes())
}

// newServer builds the Server and boots the vault (Bet 08). Shared by Start
// (loopback GUI) and StartServe (the headless daemon host).
func newServer(proj *project.Project) (*Server, error) {
        if err := proj.LoadConfig(); err != nil {
                return nil, fmt.Errorf("web: load config: %w", err)
        }
        srv := &Server{
                proj:         proj,
                page:         page, // loaded from the embedded dist via init() in render.go
                hub:          newHub(),
                modes:        newModeState(),
                state:        newServerState(),
                pins:         newPinnedSessions(proj.SessionDir()),
                titles:       newSessionTitles(proj.SessionDir()),
                engines:      map[string]*liveEngine{},
                pending:      map[string]chan permission.AskResult{},
                personaStore: persona.NewStore(proj.Global().PersonasDir()),
        }
        // Bet 08 — the Vault: one durable store under ~/.a1/a1.db. Boot
        // runs the one-shot migration; failure degrades to the file layout.
        if v, err := vault.Boot(proj.Global().Root()); err != nil {
                srv.vault = nil
        } else {
                srv.vault = v
                session.SetDefaultVault(v)
                srv.personaStore.AttachVault(v)
        }
        return srv, nil
}

// listenAndServe owns the listener, the serve goroutine, and the idempotent
// stop function. Shared by Start and StartServe.
func listenAndServe(ctx context.Context, srv *Server, host string, port int, handler http.Handler) (string, func(), error) {
        ln, err := (&net.ListenConfig{KeepAlive: readHeaderTimeout}).Listen(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
        if err != nil {
                return "", nil, fmt.Errorf("web: listen on %s:%d: %w", host, port, err)
        }
        // The listener's address (not the requested host:port) is the truth —
        // port 0 asks the OS for an ephemeral port, which tests use.
        addr := ln.Addr().String()
        srv.http = &http.Server{
                Handler:           handler,
                ReadHeaderTimeout: readHeaderTimeout,
        }

        errc := make(chan error, 1)
        go func() { errc <- srv.http.Serve(ln) }()

        stopped := make(chan struct{})
        go func() {
                defer close(stopped)
                select {
                case err := <-errc:
                        if err != nil && !errors.Is(err, http.ErrServerClosed) {
                                fmt.Fprintf(os.Stderr, "web server error: %v\n", err)
                        }
                case <-ctx.Done():
                        // Shutdown with the caller's context: an interrupt already cancelled
                        // it, so a fresh Background would make this graceful drain hang for
                        // as long as any in-flight turn takes to finish.
                        _ = srv.http.Shutdown(ctx)
                }
        }()

        stop := func() {
                srv.stopOnce.Do(func() { _ = srv.http.Close() })
                <-stopped
        }

        return "http://" + addr + "/", stop, nil
}

// routes wires the page, the session API, the SSE stream, and the config editor.
func (s *Server) routes() http.Handler {
        mux := http.NewServeMux()
        mux.HandleFunc("GET /{$}", s.handlePage)

        // Serve static assets (JS, CSS) from the embedded dist filesystem.
        // The Vite build writes to dist/assets/{hash}.{js,css}, and
        // index.html references them as /assets/{hash}.{js,css}. Strip the
        // /assets/ URL prefix and serve from the dist/assets/ subdir of the
        // embed FS so the FileServer finds the files at the right path.
        // Previously this used fs.Sub(distFS, "dist") — the FS root was
        // dist/, so the FileServer looked up "index-HASH.js" at dist/
        // (where it doesn't exist; it lives at dist/assets/), and every
        // asset returned 404. That made the GUI page load but render as
        // an empty #app div with no JS to mount.
        assets, _ := fs.Sub(distFS, "dist/assets")
        mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))

        // Top-level static files (favicon, robots.txt, etc.) live at the dist
        // root rather than under /assets/. Vite copies web/public/* into dist/
        // at build time. favicon.svg is the Dodec wireframe snapshot (the same
        // geometry the welcome hero animates); dodecahedron.svg serves the same
        // wireframe for the config editor's <link>, which predates the rename.
        rootFS, _ := fs.Sub(distFS, "dist")
        mux.Handle("GET /dodecahedron.svg", http.FileServer(http.FS(rootFS)))
        mux.Handle("GET /favicon.svg", http.FileServer(http.FS(rootFS)))
        mux.Handle("GET /favicon.ico", http.FileServer(http.FS(rootFS)))
        mux.Handle("GET /robots.txt", http.FileServer(http.FS(rootFS)))

        mux.HandleFunc("GET /api/events", s.handleEvents)
        mux.HandleFunc("GET /api/sessions", s.handleListSessions)
        // Delete a stored session (jsonl + vault row + pin + live engine).
        // Guarded like every mutating route; a two-step confirm in the UI
        // plus the 409 on in-flight turns backs it up.
        mux.HandleFunc("DELETE /api/session", s.guard(s.handleDeleteSession))
        mux.HandleFunc("GET /api/transcript", s.handleTranscript)
        // Structured transcript for the React client: same snapshot, JSON
        // instead of the htmx HTML fragment, so past sessions re-render
        // through the live markdown/tool bubble pipeline.
        mux.HandleFunc("GET /api/transcript/messages", s.handleTranscriptMessages)
        mux.HandleFunc("GET /api/session", s.handleSessionMeta)
        mux.HandleFunc("GET /api/model", s.handleModelInfo)
        // CSRF guard: every mutating chat route is wrapped with guard() so a
        // cross-origin form POST from a malicious website can't drive the
        // user's session (browsers send Origin on cross-origin POSTs; we
        // reject anything whose Origin host ≠ r.Host). Same-origin fetch
        // calls from our own page pass through cleanly.
        mux.HandleFunc("POST /api/prompt", s.guard(s.handlePrompt))
        // Re-run surfaces: regenerate re-rolls the last turn, edit forks a
        // branch from an edited user message. Both rewind the session
        // cursor then stream a normal turn over SSE.
        mux.HandleFunc("POST /api/regenerate", s.guard(s.handleRegenerate))
        mux.HandleFunc("POST /api/edit", s.guard(s.handleEdit))
        mux.HandleFunc("POST /api/cancel", s.guard(s.handleCancel))
        mux.HandleFunc("POST /api/ask", s.guard(s.handleAsk))

        // Permission mode switching and session pins.
        mux.HandleFunc("GET /api/mode", s.handleGetMode)
        mux.HandleFunc("POST /api/mode", s.guard(s.handleSetMode))
        mux.HandleFunc("GET /api/pins", s.handleGetPins)
        mux.HandleFunc("POST /api/pin", s.guard(s.handleTogglePin))
        // Theme persistence (themeapi.go): the button's config-file toggle.
        // A UI preference, not personal data — no personalGate — but a
        // config write, so guard() applies like every mutating verb. Reads
        // ride the page serve itself (themedPage stamps the choice), so
        // there is no GET to register.
        mux.HandleFunc("PUT /api/theme", s.guard(s.handleSetTheme))

        // Personal-data surfaces (the dashboard's calendar / kanban / mail
        // widgets). Status is ungated — the dashboard needs it to render
        // locked states — while every content endpoint composes
        // personalGate (dashboard chunks 2-4): the switch is enforced
        // server-side, never just hidden in the UI.
        mux.HandleFunc("GET /api/personal/status", s.handlePersonalStatus)
        // Calendar (chunk 2): the agenda feed + the range window. Reads
        // only — the write face is the agent tool's, behind the model's
        // personal_write opt-in.
        mux.HandleFunc("GET /api/personal/calendar", s.personalGate(s.handleCalendarAgenda))
        mux.HandleFunc("GET /api/personal/calendar/range", s.personalGate(s.handleCalendarRange))
        // Kanban (chunk 3): the one personal surface the web WRITES — the
        // board is interactive by design, and the human driving their own
        // dashboard needs no per-model opt-in (those flags govern the
        // agent tool). The master switch still gates every route, and the
        // mutating verbs compose guard() like every other write: a
        // cross-origin page must not be able to rearrange the board.
        mux.HandleFunc("GET /api/personal/kanban", s.personalGate(s.handleKanbanBoard))
        mux.HandleFunc("POST /api/personal/kanban", s.personalGate(s.guard(s.handleKanbanAdd)))
        mux.HandleFunc("PUT /api/personal/kanban/card", s.personalGate(s.guard(s.handleKanbanMoveEdit)))
        mux.HandleFunc("DELETE /api/personal/kanban/card", s.personalGate(s.guard(s.handleKanbanDelete)))
        // Mail (chunk 4): the read-only reader — the rail, one box's
        // latest messages, and one message with its body. GETs only; the
        // surface would not know how to write mail, and the agent's
        // facade (personal_mail) does not even get the bodies.
        mux.HandleFunc("GET /api/personal/mail/mailboxes", s.personalGate(s.handlePersonalMailboxes))
        mux.HandleFunc("GET /api/personal/mail/messages", s.personalGate(s.handlePersonalMailMessages))
        mux.HandleFunc("GET /api/personal/mail/message", s.personalGate(s.handlePersonalMailMessage))
        // Mail accounts (chunk 8): the configurator — the keyring, not
        // the letters. GET never carries a password, PUT replaces the
        // saved list (guard()ed like every write — a cross-origin page
        // must not be able to edit credentials), and the test dial
        // persists nothing. The reads above stay the only mail surface
        // that could ever matter to a message.
        mux.HandleFunc("GET /api/personal/mail/accounts", s.personalGate(s.handleMailAccountsList))
        mux.HandleFunc("PUT /api/personal/mail/accounts", s.personalGate(s.guard(s.handleMailAccountsSave)))
        mux.HandleFunc("POST /api/personal/mail/test", s.personalGate(s.guard(s.handleMailAccountTest)))

        // Files (chunk 5): the split-screen file browser's endpoints.
        // Workspace machinery, not personal data — no personalGate — and
        // deliberately NOT the /api/code viewer's project confinement:
        // the ask was full-system navigation, so reads go anywhere the
        // server's user may. The one write is guard()ed like every other
        // mutating verb (a cross-origin page must not be able to edit
        // files), and A1_FILES=off is the env kill-switch (A1_MCP
        // precedent) covering reads and writes alike.
        mux.HandleFunc("GET /api/files/list", s.filesGate(s.handleFilesList))
        mux.HandleFunc("GET /api/files/read", s.filesGate(s.handleFilesRead))
        mux.HandleFunc("PUT /api/files/write", s.filesGate(s.guard(s.handleFilesWrite)))

        // Slash-command surface (matches the TUI's command registry).
        // State + settings drive the footer chrome + Ctrl+K palette;
        // compact/context/branches/diff/code/snapshot/permission/skills/
        // extensions drive the slash-command handlers behind the same UI.
        mux.HandleFunc("GET /api/state", s.handleGetState)
        mux.HandleFunc("POST /api/compact", s.guard(s.handleCompact))
        mux.HandleFunc("GET /api/context", s.handleContext)
        mux.HandleFunc("GET /api/branches", s.handleListBranches)
        mux.HandleFunc("POST /api/branch", s.guard(s.handleSwitchBranch))
        mux.HandleFunc("GET /api/diff", s.handleDiff)
        mux.HandleFunc("GET /api/code", s.handleCode)
        mux.HandleFunc("POST /api/snapshot", s.guard(s.handleSnapshot))
        mux.HandleFunc("GET /api/permission/info", s.handlePermissionInfo)
        mux.HandleFunc("GET /api/settings", s.handleGetSettings)
        mux.HandleFunc("POST /api/settings/model", s.guard(s.handleSetModel))
        mux.HandleFunc("POST /api/settings/think", s.guard(s.handleSetThink))
        mux.HandleFunc("POST /api/settings/agents", s.guard(s.handleSetAgents))
        mux.HandleFunc("POST /api/settings/memory", s.guard(s.handleSetMemory))
        mux.HandleFunc("GET /api/skills", s.handleListSkills)
        mux.HandleFunc("GET /api/extensions", s.handleListExtensions)

        // Persona registry: the PersonaManager's contract. Backed by the same
        // persona.Store the agent session reads from, so a switch through the
        // web GUI takes effect on the next turn — same as the TUI palette.
        mux.HandleFunc("GET /api/personas", s.handleListPersonas)
        mux.HandleFunc("POST /api/personas", s.guard(s.handleCreatePersona))
        mux.HandleFunc("PUT /api/personas/{id}", s.guard(s.handleUpdatePersona))
        mux.HandleFunc("PUT /api/personas", s.guard(s.handleUpdatePersona)) // legacy body-id form
        mux.HandleFunc("DELETE /api/personas/{id}", s.guard(s.handleDeletePersona))
        mux.HandleFunc("DELETE /api/personas", s.guard(s.handleDeletePersona)) // legacy ?id= form
        mux.HandleFunc("GET /api/personas/active", s.handleGetActivePersona)
        mux.HandleFunc("POST /api/personas/active", s.guard(s.handleSetActivePersona))
        // Memory bank inspection + forget, scoped per persona.
        mux.HandleFunc("GET /api/personas/memories", s.handlePersonaMemories)
        mux.HandleFunc("DELETE /api/personas/memories", s.guard(s.handlePersonaMemories))
        // Uploaded profile pictures (this backend's extension over the old
        // emoji-only avatars — the Avatar component prefers the image when set).
        mux.HandleFunc("POST /api/personas/avatar", s.guard(s.handlePersonaAvatar))
        mux.HandleFunc("GET /api/personas/{id}/avatar", s.handleServePersonaAvatar)
        // Model catalog for the persona editor's model dropdown (and the
        // footer's context-window scaling). Read-only sibling of the config
        // API's POST /api/models probe endpoint.
        mux.HandleFunc("GET /api/models", s.handleListModels)

        // MCP server registry (mcp.json): the config editor's MCP section.
        // Reads are open like every other GET; writes are guarded.
        mux.HandleFunc("GET /api/mcp", s.handleListMCP)
        mux.HandleFunc("PUT /api/mcp", s.guard(s.handlePutMCP))
        mux.HandleFunc("DELETE /api/mcp", s.guard(s.handleDeleteMCP))

        // The config editor is a second document served from the same origin, so a
        // browser already on the settings page can call back without CORS. Its API
        // paths (/api/config, /api/models, /api/stats) do not collide with the chat
        // API above, and the Handler itself enforces the loopback + JSON-origin
        // checks. Mounting it here means `a1 --web` needs no second server.
        s.configRoutes(mux)

        // Telemetry surfaces (Bet 07): the JSON rollup and the server-rendered
        // dashboard. Loopback-only like the rest of this server's reads.
        mux.HandleFunc("GET /api/telemetry", s.handleTelemetry)
        mux.HandleFunc("GET /stats", s.handleStatsDashboard)

        // The dashboard's usage heatmap: per-day activity counts rolled up
        // from the vault (sessions, entries, tool runs). Read-only and
        // loopbound like /api/telemetry; TTL-cached because it walks every
        // entry payload.
        mux.HandleFunc("GET /api/usage/daily", s.handleUsageDaily)
        return mux
}

// guard is the CSRF/Origin guard for mutating chat routes. It rejects any
// POST/PUT/DELETE whose Origin header points at a different host than r.Host
// (browsers always send Origin on cross-origin POSTs, and on same-origin
// POSTs in modern browsers). Requests with no Origin header (curl, older
// Safari) pass through — the JSON content-type and decodable body are
// enforced downstream by decodeBody.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
                        if origin := r.Header.Get("Origin"); origin != "" {
                                expectedScheme := "http"
                                if r.TLS != nil {
                                        expectedScheme = "https"
                                }
                                u, err := url.Parse(origin)
                                if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil ||
                                        !strings.EqualFold(u.Scheme, expectedScheme) ||
                                        !strings.EqualFold(u.Host, r.Host) {
                                        writeStatus(w, http.StatusForbidden, "request origin is not allowed")
                                        return
                                }
                        }
                }
                next(w, r)
        }
}

// configRoutes mounts the config editor's page and API. The configserver
// Handler is reused as-is (same code path as `a1 config`), so the editor can
// never drift from the CLI surface.
func (s *Server) configRoutes(mux *http.ServeMux) {
        cfg := &configserver.Handler{ConfigPath: s.configPath()}

        // The editor document lives at /settings, but configserver.Handler matches
        // on the exact path "/" for its page. Rewrite just that one request so the
        // document is served, and leave every API path untouched: config.html's
        // fetches are root-relative (/api/config) and routed directly below.
        mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
                cfg.ServeHTTP(w, editorPageRequest(r))
        })

        for _, route := range []string{"GET /api/config", "GET /api/stats"} {
                mux.HandleFunc(route, cfg.ServeHTTP)
        }
        // POST /api/config is wrapped so a successful save triggers a project
        // config reload: the web GUI persona picker needs to reflect the new
        // values on the next turn, without restarting the server.
        mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
                // Capture the response so we only refresh on success.
                rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
                cfg.ServeHTTP(rec, r)
                if rec.status >= 200 && rec.status < 300 {
                        s.onConfigSaved()
                }
        })
        mux.HandleFunc("POST /api/models", cfg.ServeHTTP)
}

// onConfigSaved reloads the project config so the next turn reflects the
// saved values without restarting the server. Best-effort: errors are
// logged to stderr rather than surfaced, because the save itself already
// succeeded by the time this runs. Live engines also rebind their tool
// sets: the personal-data tools (chunk 2+) mount and unmount with the
// master switch and the per-model flags, so a save is live on the next
// turn of an OPEN chat, not only in new ones.
func (s *Server) onConfigSaved() {
        if s.proj == nil {
                return
        }
        if err := s.proj.LoadConfig(); err != nil {
                fmt.Fprintf(os.Stderr, "web: reload config after save: %v\n", err)
        }
        var engines []*agent.Engine
        s.mu.Lock()
        for _, le := range s.engines {
                if le != nil && le.engine != nil {
                        engines = append(engines, le.engine)
                }
        }
        s.mu.Unlock()
        for _, e := range engines {
                e.RefreshPersonalTools()
        }
}

// statusRecorder wraps http.ResponseWriter so handlers can run unchanged
// while the wrapper observes the status code set by the inner handler.
type statusRecorder struct {
        http.ResponseWriter
        status int
}

func (r *statusRecorder) WriteHeader(code int) {
        r.status = code
        r.ResponseWriter.WriteHeader(code)
}

// configPath resolves the config file the editor should read and write. It is
// the project's global config file, matching what `a1 config` opens.
func (s *Server) configPath() string {
        if s.proj != nil {
                return s.proj.Global().ConfigFile()
        }
        // A Server built without a project (tests) still answers the API; the
        // Handler treats a non-existent path as an empty document.
        return ""
}

// liveFor returns the engine for a session id, creating it on first use.
// An empty id starts a brand-new persisted session (the "new chat" path).
func (s *Server) liveFor(sessionID string) (*liveEngine, error) {
        // Resolve the gate, memory bank, and live model BEFORE taking s.mu:
        // gate() reads the mode store, stateStore() and memStoreFor() take
        // s.mu themselves — nesting any of them under liveFor's own lock is
        // a self-deadlock (the locked-section engine build must stay
        // lock-free apart from s.mu itself).
        gate := s.gate()
        // Memory: attach the shared bank only when memory is EFFECTIVELY on —
        // the active persona's explicit memory_enabled wins over the global
        // settings toggle, mirroring what applyPersonaToEngines does for live
        // engines and what Engine.MemoryEnabled() resolves at runtime.
        var mem *memstore.Store
        if s.effectiveMemoryEnabled(s.stateStore().snapshot().MemoryEnabled) {
                mem = s.memStoreFor()
        }
        // Resolve the live model + think level so a model switch applies to
        // brand-new sessions too (not just already-running engines). The
        // resolution folds the ACTIVE persona's Model in when the manual
        // knob is untouched — persona settings are the defaults.
        modelCfg := s.proj.Config().Model()
        if cfg, ok := s.effectiveModelConfig(); ok {
                modelCfg = cfg
        }
        // engineOptions reads the mode store (WithBasePermissionMode needs the
        // surface's pre-persona mode), and modeStore() takes s.mu — so the whole
        // option set must be built before the locked section too.
        engOpts := s.engineOptions(gate)
        if mem != nil {
                engOpts = append(engOpts, agent.WithMemoryStore(mem))
        }
        // Capture the ACTIVE persona before the locked section: a freshly
        // built engine must adopt its settings (model, permission mode,
        // memory-ops model) on creation, not only on the next switch — a
        // user who configured a persona yesterday and opens a new chat
        // today expects the new session to run that persona's settings.
        // The DEFAULT persona included: its settings are real settings the
        // editor writes, and skipping the apply was why fresh sessions
        // fell back to the startup model. The application itself runs
        // AFTER s.mu releases (see the deferred func below): the
        // permission-mode setter re-enters s.mu through applyToEngines, so
        // applying under the lock would self-deadlock.
        activePersona := ""
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil {
                        activePersona = p.ID
                }
        }
        // fresh is set only on the path that BUILT a new engine. Deferred
        // funcs run LIFO: s.mu.Unlock() (registered next) runs first, then
        // this one — so SetPersona runs outside the lock, exactly like
        // applyPersonaToEngines applies switches to already-live engines.
        var fresh *liveEngine
        defer func() {
                if fresh == nil || activePersona == "" {
                        return
                }
                // SetPersona("") reverts to the default persona, so the
                // always-run form is safe for every id — applyPersonaSettings
                // applies the (possibly empty) overrides and restores the
                // engine's base where the persona declares none.
                _ = fresh.engine.SetPersona(activePersona)
        }()

        s.mu.Lock()
        defer s.mu.Unlock()
        if le, ok := s.engines[sessionID]; ok {
                return le, nil
        }
        s.initMaps()

        cwd, err := os.Getwd()
        if err != nil {
                return nil, fmt.Errorf("web: getwd: %w", err)
        }
        opts := []agent.SessionOption{
                agent.WithCwd(cwd),
                agent.WithSessionDir(s.proj.SessionDir()),
                agent.WithPersist(true),
        }
        if s.personaStore != nil {
                opts = append(opts, agent.WithPersonaStore(s.personaStore))
        }
        if sessionID != "" {
                opts = append(opts, agent.WithResumeID(sessionID))
        }
        sess, err := agent.NewSession(opts...)
        if err != nil {
                return nil, err
        }

        if le, ok := s.engines[sess.ID()]; ok {
                // NewSession with WithResumeID resolves to the session's real
                // ID, which may already have a live engine (e.g. a transcript
                // load raced the first prompt). Hand the caller the existing
                // engine instead of stacking a second one over the same
                // session tree.
                return le, nil
        }
        eng, err := agent.NewEngine(modelCfg, sess, engOpts...)
        if err != nil {
                return nil, err
        }
        le := &liveEngine{engine: eng}
        s.engines[eng.SessionID()] = le
        fresh = le // this call built it: adopt the active persona post-unlock
        return le, nil
}

// engineOptions are the shared options for every web engine: the project-scoped
// permission gate, a browser-backed Ask handler, and the default toolset. The
// gate is passed in rather than fetched, because the caller (liveFor) holds
// s.mu while building the engine.
func (s *Server) engineOptions(gate permission.Gate) []agent.EngineOption {
        cfg := s.proj.Config().Model()
        opts := []agent.EngineOption{
                agent.WithGate(gate),
                agent.WithAsk(s.ask),
                agent.WithContinueAsk(func(context.Context, int) (bool, error) {
                        // No modal exists on the web surface: grant the budget so a turn
                        // runs to completion instead of dead-ending at the round cap.
                        return true, nil
                }),
                agent.WithTools(tools.DefaultTools()),
                agent.WithHooks(model.HooksFor(cfg.Name)),
                // Honor the per-turn tool-call budget configured via the TUI
                // palette (tools.max_tool_calls in config.yaml). Yolo mode
                // still overrides it inside the engine.
                agent.WithMaxToolCalls(s.proj.Config().Tools.MaxToolCalls),
                // Config-defined evidence checks (config.yaml `checks:`):
                // named commands whose completed runs record check:<name>
                // evidence on the ledger.
                agent.WithEvidenceChecks(s.proj.Config().EvidenceChecks()),
                // Per-persona semantic memory: wire the embedder + the
                // model/permission-mode apply-on-switch hooks so persona
                // switches fully retarget the agent's stack.
                agent.WithEmbedder(s.openEmbedder()),
                agent.WithPersonaModelResolver(s.resolvePersonaModel),
                agent.WithPermissionModeSetter(func(mode permission.Mode) {
                        _ = s.setPermissionMode(mode)
                }),
                // The mode the surface ran before any persona override —
                // restored when the next persona (or the default) declares
                // no mode of its own.
                agent.WithBasePermissionMode(s.modeStore().mode()),
                // The RAW memory toggle (pre-persona). The engine folds it
                // with the active persona's memory_enabled in MemoryEnabled()
                // — passed separately from WithMemoryStore so a persona that
                // opts in can have the bank attached while the global knob
                // stays off.
                agent.WithMemoryEnabled(s.stateStore().snapshot().MemoryEnabled),
                // Personal-data tools (dashboard chunks 2+): the live
                // section off the same project the gate reads — a config
                // save flips mounts on the next refresh (onConfigSaved).
                agent.WithPersonalConfig(func() project.PersonalConfig { return s.personalConfig() }),
        }
        // The Clock (Bet 01), when the daemon host wired it: fleet tools +
        // wake-ups for every engine this server builds.
        if s.jobs != nil {
                opts = append(opts, agent.WithJobs(s.jobs))
        }
        if s.scheduler != nil {
                opts = append(opts, agent.WithScheduler(s.scheduler))
        }
        return opts
}

// initMaps lazily creates the server's registries. Start builds a fully
// initialized Server, but a zero-valued Server (tests, embedding) must not
// panic on first use.
func (s *Server) initMaps() {
        if s.hub == nil {
                s.hub = newHub()
        }
        if s.engines == nil {
                s.engines = map[string]*liveEngine{}
        }
        if s.pending == nil {
                s.pending = map[string]chan permission.AskResult{}
        }
        if s.modes == nil {
                s.modes = newModeState()
        }
        if s.state == nil {
                s.state = newServerState()
        }
        if s.pins == nil {
                s.pins = newPinnedSessions("")
        }
}

// modeStore returns the live mode state, initializing it on first use.
func (s *Server) modeStore() *modeState {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.initMaps()
        return s.modes
}

// stateStore returns the live UI-knob state (model, think, agents,
// memory, persona), initializing it on first use so a hand-built Server
// (tests, embedding) never nil-panics on a state read.
func (s *Server) stateStore() *serverState {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.initMaps()
        return s.state
}

// pinStore returns the pin list, initializing it on first use. A Server built
// without a project gets an unconfigured store: reads report no pins and writes
// fail with a clear error instead of panicking.
func (s *Server) pinStore() *pinnedSessions {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.initMaps()
        return s.pins
}

// events returns the live hub, initializing it on first use so callers (and
// tests) never have to remember build order.
func (s *Server) events() *hub {
        s.mu.Lock()
        defer s.mu.Unlock()
        s.initMaps()
        return s.hub
}

// policy returns the project's permission policy, falling back to the package
// defaults when the config is not loaded. Start loads it before serving, but a
// hand-built Server (tests, embedding) may not have.
//
// The fallback must be DefaultPolicy, not a zero Policy: the zero value leaves
// BashDefault at Allow, which fails open and lets every command through.
func (s *Server) policy() permission.Policy {
        if s.proj != nil && s.proj.Config() != nil {
                return s.proj.Config().Permissions
        }
        return permission.DefaultPolicy()
}

// gate builds the permission gate for web sessions from the project policy and
// the live permission mode. The gate is cached per mode so switching back and
// forth does not reset the APPA trajectory state every time.
func (s *Server) gate() permission.Gate {
        modes := s.modeStore()
        if cached, ok := modes.loadGate(modes.mode()); ok {
                return cached
        }
        g, err := modes.rebuildGate(s.policy(), s.workspace(), nil)
        if err != nil {
                fmt.Fprintf(os.Stderr, "web: permission gate fallback: %v\n", err)
                return permission.AllowAll{}
        }
        modes.storeGate(modes.mode(), g)
        return g
}

// workspace returns the project root used for permission confinement.
func (s *Server) workspace() string {
        if s.proj != nil {
                return s.proj.Root()
        }
        return permission.WorkspaceRoot()
}

// openEmbedder returns the embedder used to open per-persona semantic
// memory stores for web engines. Mirrors the TUI controller's
// openEmbedder: Ollama when the project config has semantic_search
// configured, FakeEmbedder otherwise.
func (s *Server) openEmbedder() memstore.Embedder {
        if s.proj != nil && s.proj.Config() != nil {
                ss := s.proj.Config().SemanticSearch
                if ollamaEmb := memstore.TryOllamaFromConfig(ss.OllamaBaseURL, ss.EmbeddingModel); ollamaEmb != nil {
                        return ollamaEmb
                }
        }
        return memstore.FakeEmbedder{DimCount: 64}
}

// resolvePersonaModel looks up a persona's configured model name in the
// project's model catalog. Wired to the engine via WithPersonaModelResolver
// so SetPersona can apply the active persona's Model field.
func (s *Server) resolvePersonaModel(name string) (llm.ModelConfig, bool) {
        if s.proj == nil || s.proj.Config() == nil {
                return llm.ModelConfig{}, false
        }
        return s.proj.Config().FindModel(name)
}

// setPermissionMode switches the live mode and installs the new gate on every
// running engine, so a change takes effect on the next tool call without
// restarting the session.
func (s *Server) setPermissionMode(mode permission.Mode) error {
        modes := s.modeStore()
        // Validate before mutating anything, so a bad mode leaves the previous gate
        // (and the previous mode) exactly as they were.
        if _, ok := modeSummary[mode]; !ok {
                return fmt.Errorf("web: unknown permission mode %q", mode)
        }
        var prevGate permission.Gate
        if g, ok := modes.loadGate(modes.mode()); ok {
                prevGate = g
        }
        // Publish the mode first: the gate is built from it, so building first
        // would stamp the old mode into the new gate and every decision would fold
        // as if nothing changed.
        prev, err := modes.setMode(mode)
        if err != nil {
                return err
        }
        var gate permission.Gate
        if g, ok := modes.loadGate(mode); ok {
                gate = g
        } else {
                g, err := modes.rebuildGate(s.policy(), s.workspace(), prevGate)
                if err != nil {
                        // Roll the mode back: a gate we could not build is not a state to
                        // leave the server in.
                        _, _ = modes.setMode(prev)
                        return fmt.Errorf("web: build gate for %s: %w", mode, err)
                }
                gate = g
                modes.storeGate(mode, gate)
        }
        s.applyToEngines(gate)
        return nil
}

// ask blocks until the browser answers a permission prompt, or the timeout
// elapses (which denies). Feedback from a denial is forwarded to the model.
func (s *Server) ask(ctx context.Context, req permission.Request, reason string) (permission.AskResult, error) {
        id := permission.CallHash(req)
        ch := make(chan permission.AskResult, 1)

        s.mu.Lock()
        s.initMaps()
        s.pending[id] = ch
        s.mu.Unlock()

        // Publish after unlocking so a stalled browser cannot hold the engine mutex.
        // The session id rides on the turn context so the prompt lands on the
        // browser tab that started it.
        s.events().publish(sessionFromContext(ctx), serverEvent{
                Kind: eventAsk,
                Ask:  &askPayload{ID: id, Tool: req.Tool, Reason: reason},
        })

        defer func() {
                s.mu.Lock()
                delete(s.pending, id)
                s.mu.Unlock()
        }()

        timer := time.NewTimer(askTimeout)
        defer timer.Stop()
        select {
        case res := <-ch:
                return res, nil
        case <-timer.C:
                return permission.AskResult{}, errors.New("web: permission prompt timed out")
        case <-ctx.Done():
                return permission.AskResult{}, ctx.Err()
        }
}

// requireProject guards handlers that need the loaded project. It writes the
// error response itself and returns false, so a Server built without a project
// (or a config that failed to load) reports a clean 500 instead of panicking
// on a nil dereference inside the handler.
func (s *Server) requireProject(w http.ResponseWriter) bool {
        if s.proj == nil {
                writeStatus(w, http.StatusInternalServerError, "web: project is not configured")
                return false
        }
        return true
}

// listSessions returns persisted sessions for the sidebar, with smart
// titles overlaid: a generated name (titles.json) wins over the
// first-user-message preview, so the sidebar shows what the conversation
// became, not how it started.
func (s *Server) listSessions() ([]session.SessionMeta, error) {
        if s.proj == nil {
                return nil, errors.New("web: project is not configured")
        }
        metas, err := session.ListSessions(s.proj.SessionDir())
        if err != nil {
                return nil, err
        }
        titles := s.titlesStore().snapshot()
        if len(titles) == 0 {
                return metas, nil
        }
        for i := range metas {
                if t := titles[metas[i].ID]; t != "" {
                        metas[i].Preview = t
                }
        }
        return metas, nil
}
