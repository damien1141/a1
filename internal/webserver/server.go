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
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/agent"
	"github.com/damien1141/a1/internal/configserver"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/project/model"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/tools"
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

	mu      sync.Mutex
	modes   *modeState
	pins    *pinnedSessions
	engines map[string]*liveEngine
	pending map[string]chan permission.AskResult
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
	if err := proj.LoadConfig(); err != nil {
		return "", nil, fmt.Errorf("web: load config: %w", err)
	}

	page, err := loadPage()
	if err != nil {
		return "", nil, err
	}

	ln, err := (&net.ListenConfig{KeepAlive: readHeaderTimeout}).Listen(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return "", nil, fmt.Errorf("web: listen on 127.0.0.1:%d: %w", port, err)
	}

	srv := &Server{
		proj:    proj,
		page:    page,
		hub:     newHub(),
		modes:   newModeState(),
		pins:    newPinnedSessions(proj.SessionDir()),
		engines: map[string]*liveEngine{},
		pending: map[string]chan permission.AskResult{},
	}
	srv.http = &http.Server{
		Handler:           srv.routes(),
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

	return fmt.Sprintf("http://127.0.0.1:%d/", port), stop, nil
}

// routes wires the page, the session API, the SSE stream, and the config editor.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handlePage)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("GET /api/transcript", s.handleTranscript)
	mux.HandleFunc("GET /api/session", s.handleSessionMeta)
	mux.HandleFunc("POST /api/prompt", s.handlePrompt)
	mux.HandleFunc("POST /api/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/ask", s.handleAsk)

	// Permission mode switching and session pins.
	mux.HandleFunc("GET /api/mode", s.handleGetMode)
	mux.HandleFunc("POST /api/mode", s.handleSetMode)
	mux.HandleFunc("GET /api/pins", s.handleGetPins)
	mux.HandleFunc("POST /api/pin", s.handleTogglePin)

	// The config editor is a second document served from the same origin, so a
	// browser already on the settings page can call back without CORS. Its API
	// paths (/api/config, /api/models, /api/stats) do not collide with the chat
	// API above, and the Handler itself enforces the loopback + JSON-origin
	// checks. Mounting it here means `a1 --web` needs no second server.
	s.configRoutes(mux)
	return mux
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

	for _, route := range []string{"GET /api/config", "GET /api/stats", "POST /api/config"} {
		mux.HandleFunc(route, cfg.ServeHTTP)
	}
	mux.HandleFunc("POST /api/models", cfg.ServeHTTP)
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
	// Resolve the gate before taking s.mu: gate() reads the mode store, and a
	// lock ordering mistake here would deadlock every handler at once.
	gate := s.gate()

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
	if sessionID != "" {
		opts = append(opts, agent.WithResumeID(sessionID))
	}
	sess, err := agent.NewSession(opts...)
	if err != nil {
		return nil, err
	}

	eng, err := agent.NewEngine(s.proj.Config().Model(), sess, s.engineOptions(gate)...)
	if err != nil {
		return nil, err
	}
	le := &liveEngine{engine: eng}
	s.engines[eng.SessionID()] = le
	return le, nil
}

// engineOptions are the shared options for every web engine: the project-scoped
// permission gate, a browser-backed Ask handler, and the default toolset. The
// gate is passed in rather than fetched, because the caller (liveFor) holds
// s.mu while building the engine.
func (s *Server) engineOptions(gate permission.Gate) []agent.EngineOption {
	cfg := s.proj.Config().Model()
	return []agent.EngineOption{
		agent.WithGate(gate),
		agent.WithAsk(s.ask),
		agent.WithContinueAsk(func(context.Context, int) (bool, error) {
			// No modal exists on the web surface: grant the budget so a turn
			// runs to completion instead of dead-ending at the round cap.
			return true, nil
		}),
		agent.WithTools(tools.DefaultTools()),
		agent.WithHooks(model.HooksFor(cfg.Name)),
	}
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

// listSessions returns persisted sessions for the sidebar.
func (s *Server) listSessions() ([]session.SessionMeta, error) {
	if s.proj == nil {
		return nil, errors.New("web: project is not configured")
	}
	return session.ListSessions(s.proj.SessionDir())
}
