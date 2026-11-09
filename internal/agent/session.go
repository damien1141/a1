package agent

import (
        "errors"
        "fmt"
        "strings"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/persona"
        "github.com/damien1141/a1/internal/project"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/session/memory"
)

// Session owns the message store for the engine loop. It wraps a
// session.Manager and projects entries (including compaction summaries)
// into LLM context. Compaction policy lives on Engine, not here.
type Session struct {
        lastID            string
        manager           *session.Manager
        contextCache      []llm.Message
        contextCacheValid bool
        memoryBank        *memory.Bank
        memoryInjected    bool

        // personaBlock is the FROZEN persona injection: the active persona's
        // framing lines + per-persona memory bank entries, captured once per
        // persona epoch and re-prepended byte-identically on every context
        // rebuild. Freezing — not flag-gating — is what keeps two properties
        // true at once: a mid-session cache bust (every Append) must not drop
        // the persona from later turns, and consecutive request prefixes must
        // stay byte-identical so local prompt caching and provider cache reads
        // survive the bust. Cleared by SetPersona so a switch re-freezes under
        // the new personality.
        personaBlock []llm.Message

        // personaStore is injected via the WithPersonaStore option. Optional —
        // when nil, the session behaves identically to the pre-persona baseline
        // (default-neutral assistant).
        personaStore *persona.Store
}

// SessionOption configures NewSession.
type SessionOption func(*sessionConfig)

type sessionConfig struct {
        cwd          string
        sessionDir   string
        persist      bool
        resumePath   string
        resumeID     string
        parentID     string
        personaStore *persona.Store
}

// WithCwd sets the cwd written to SessionHeader (usually process cwd).
func WithCwd(cwd string) SessionOption {
        return func(c *sessionConfig) { c.cwd = cwd }
}

// WithSessionDir sets ~/.a1/session (required when Persist is true or resuming by id).
func WithSessionDir(dir string) SessionOption {
        return func(c *sessionConfig) { c.sessionDir = dir }
}

// WithPersist enables JSONL persistence (false → in-memory; tests default).
func WithPersist(persist bool) SessionOption {
        return func(c *sessionConfig) { c.persist = persist }
}

// WithResumePath opens this jsonl path (mutually exclusive with WithResumeID).
func WithResumePath(path string) SessionOption {
        return func(c *sessionConfig) { c.resumePath = path }
}

// WithResumeID resolves a session under SessionDir (mutually exclusive with WithResumePath).
func WithResumeID(id string) SessionOption {
        return func(c *sessionConfig) { c.resumeID = id }
}

// WithParentID links a new persisted session to a parent (sub-agents).
func WithParentID(id string) SessionOption {
        return func(c *sessionConfig) { c.parentID = id }
}

// WithPersonaStore attaches a persona store to the session. When set, the
// active persona's framing line and per-persona memory bank are injected
// into the agent context on every BuildContext. Without this option the
// session is identical to the pre-persona baseline.
func WithPersonaStore(store *persona.Store) SessionOption {
        return func(c *sessionConfig) { c.personaStore = store }
}

// NewSession creates a session wrapper according to opts.
func NewSession(opts ...SessionOption) (*Session, error) {
        var cfg sessionConfig
        for _, opt := range opts {
                opt(&cfg)
        }
        if cfg.resumePath != "" && cfg.resumeID != "" {
                return nil, errors.New("agent: ResumePath and ResumeID are mutually exclusive")
        }

        if cfg.resumePath != "" || cfg.resumeID != "" {
                path := cfg.resumePath
                if path == "" {
                        if cfg.sessionDir == "" {
                                return nil, errors.New("agent: SessionDir required to resume by id")
                        }
                        var err error
                        path, err = session.FindSessionFile(cfg.sessionDir, cfg.resumeID)
                        if err != nil {
                                return nil, err
                        }
                }
                m, err := session.OpenSession(path)
                if err != nil {
                        return nil, err
                }
                return &Session{
                        manager:      m,
                        lastID:       m.LeafID(),
                        personaStore: cfg.personaStore,
                }, nil
        }

        if cfg.persist {
                if cfg.sessionDir == "" {
                        return nil, errors.New("agent: SessionDir required when Persist is true")
                }
                var m *session.Manager
                var err error
                if v := session.DefaultVault(); v != nil {
                        // Bet 08: persisted sessions are rows in the one
                        // durable store. The synthetic File() path keeps
                        // every path-derived behavior (evidence scope,
                        // resume, listings) unchanged.
                        m, err = session.NewVaultManager(cfg.cwd, cfg.sessionDir, cfg.parentID, v,
                                session.WithSessionDir(cfg.sessionDir),
                                session.WithParent(cfg.parentID))
                } else {
                        m, err = session.NewSessionManager(cfg.cwd,
                                session.WithSessionDir(cfg.sessionDir),
                                session.WithShouldFlush(true),
                                session.WithParent(cfg.parentID),
                        )
                }
                if err != nil {
                        return nil, err
                }
                return &Session{
                        manager:      m,
                        personaStore: cfg.personaStore,
                }, nil
        }

        return &Session{
                manager:      session.NewManager(cfg.cwd),
                personaStore: cfg.personaStore,
        }, nil
}

// Fork creates a new Session that shares the same underlying manager state
// but starts from the current leaf, allowing independent conversation branches.
// The persona store is inherited from the parent so the forked branch keeps
// the same personality.
func (s *Session) Fork() (*Session, error) {
        fork, err := s.manager.Fork()
        if err != nil {
                return nil, fmt.Errorf("agent: failed to fork session: %w", err)
        }
        return &Session{
                manager:      fork,
                lastID:       fork.LeafID(),
                personaStore: s.personaStore,
        }, nil
}

// ID returns the durable session id (empty only if manager missing).
func (s *Session) ID() string {
        if s == nil || s.manager == nil {
                return ""
        }
        return s.manager.ID()
}

// CacheScope returns the session's logical prompt-cache scope — the
// rotation-stable bucket id the engine routes provider prompt caches by
// (see Manager.CacheScope). Empty only when the manager is missing, which
// leaves the wire as it was before scopes existed.
func (s *Session) CacheScope() string {
        if s == nil || s.manager == nil {
                return ""
        }
        return s.manager.CacheScope()
}

// File returns the JSONL path, or empty in memory mode / before first flush path assignment.
func (s *Session) File() string {
        if s == nil || s.manager == nil {
                return ""
        }
        return s.manager.File()
}

// SessionDir returns the directory this session persists under (empty in
// memory mode). Surfaces the storage root for features that read the
// project's other sessions — session_search needs it to list them.
func (s *Session) SessionDir() string {
        if s == nil || s.manager == nil {
                return ""
        }
        return s.manager.SessionDir()
}

// Cwd returns the session header cwd.
func (s *Session) Cwd() string {
        if s == nil || s.manager == nil {
                return ""
        }
        return s.manager.Cwd()
}

func (s *Session) invalidateContextCache() {
        s.contextCacheValid = false
        // The frozen persona block deliberately survives a cache bust: the
        // next rebuild re-prepends the same bytes, so the context stays
        // byte-stable across Appends (the property prefix caching bills
        // against) and the persona never vanishes mid-session. Only
        // SetPersona() clears it — a switch re-freezes under the new
        // personality.
}

// activePersona returns the persona store's currently active persona when
// the store is attached. The bool is false when there is no store or the
// active persona is the default (the bare a1 assistant), letting callers
// treat both as "no persona framing needed".
func (s *Session) activePersona() (persona.Persona, bool) {
        if s == nil || s.personaStore == nil {
                return persona.Persona{}, false
        }
        p, err := s.personaStore.ActivePersona()
        if err != nil || p.ID == persona.DefaultPersonaID {
                return persona.Persona{}, false
        }
        return p, true
}

// MemoryBank returns the memory bank the remember/recall tools should use.
//
// When a persona store is attached and the ACTIVE persona is not the
// default, the bank is the persona's own bank ("persona:<id>" namespace) —
// each profile builds up its own long-term knowledge instead of one shared
// soup, matching the PersonaManager's "everything this persona remembers
// lands here — other personas never see it" contract. The default persona
// keeps the legacy per-session bank so pre-persona sessions stay readable.
//
// The persona-bank decision is re-evaluated on every call (not cached on
// s.memoryBank) so a mid-session persona switch immediately retargets
// where memories are written. The session-bank handle itself IS cached as
// before.
func (s *Session) MemoryBank() *memory.Bank {
        if s == nil {
                return nil
        }
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil && p.ID != persona.DefaultPersonaID {
                        bank, err := s.personaStore.MemoryBank(p)
                        if err == nil {
                                return bank
                        }
                        // Fall through to the session bank on error — a broken persona
                        // dir should degrade to shared memory, not lose the write.
                }
        }
        if s.memoryBank != nil {
                return s.memoryBank
        }
        sid := s.ID()
        if sid == "" {
                return nil
        }
        bank, err := memory.OpenBank(sid, project.GetDefaultProject().Global().MemoryDir())
        if err != nil {
                return nil
        }
        s.memoryBank = bank
        return bank
}

// Append records one or more messages.
func (s *Session) Append(message ...llm.Message) error {
        s.invalidateContextCache()
        for _, msg := range message {
                id, err := s.manager.Append(msg)
                if err != nil {
                        return err
                }
                s.lastID = id
        }
        return nil
}

// AppendCompaction records a compaction entry and invalidates the context cache.
func (s *Session) AppendCompaction(c session.Compaction) error {
        s.invalidateContextCache()
        id, err := s.manager.AppendCompaction(c)
        if err != nil {
                return err
        }
        s.lastID = id
        return nil
}

// AppendToolResult records a tool result carrying UI metadata (today: the
// ranked Sources of a search-mode fetch) alongside the model-facing
// message, so a reloaded session can still render its source pills.
func (s *Session) AppendToolResult(msg llm.Message, sources []session.ToolSource) error {
        s.invalidateContextCache()
        id, err := s.manager.AppendToolResult(msg, sources)
        if err != nil {
                return err
        }
        s.lastID = id
        return nil
}

// RewindTo moves the session cursor so the next Append lands as a SIBLING
// of the entry with the given id ("" rewinds to just after the header).
// The abandoned tail stays in the append-only store as an off-path branch.
// This is the branch point for regenerate (re-roll the last turn) and
// edit-user-message (fork with edited text): rewind to the user message's
// parent, then the normal Loop append replays the prompt as a new branch.
func (s *Session) RewindTo(entryID string) error {
        s.invalidateContextCache()
        return s.manager.SetLeaf(entryID)
}

// AllEntries returns the FULL leaf-to-root branch, oldest first, with no
// compaction cut applied. PathEntries (the LLM context path) stops at the
// newest compaction marker; the web transcript renders this instead, so a
// compacted session still shows its early messages above the cut marker
// and the user can scroll the whole conversation after compaction.
// Delegates to Manager.FullBranch — the session-layer primitive the
// scrollback contract tests pin — so the walk has exactly one
// implementation.
func (s *Session) AllEntries() []session.MessageEntry {
        return s.manager.FullBranch()
}

// PathEntries returns the current leaf-to-root session entries for compaction.
func (s *Session) PathEntries() []session.MessageEntry {
        return s.manager.BuildContext()
}

// BuildContext returns the messages for LLM inference, oldest first.
// Compaction entries are projected as user messages carrying the summary.
// Persona framing is injected once per session as system messages.
//
// What is deliberately NOT here: the wall clock, the weather forecast, the
// semantic memory bundle, and job notices. Every provider builder hoists
// RoleSystem text into the leading system block — the block prompt caching
// is pinned to — so anything regenerated per turn here re-billed the whole
// context as a cache write on every request (the clock carried seconds and
// was rebuilt on every tool round). The memory bundle and job notices ride
// as a persisted, Runtime-marked user message appended at each turn
// boundary by the engine (see runtime_rider.go); the clock and forecast
// are fetch engines the model calls on demand.
//
// Persona injection:
//   - persona: when a persona.Store is attached, the active persona's framing
//     line ("Active persona: Sherlock. A brilliant detective persona.") is
//     injected as a system message, and the persona's per-persona memory
//     bank entries are injected alongside (but not confused with) the
//     session's own memory bank. Both injections happen once per session
//     and are sticky across context-cache busts; switching the active persona
//     via SetPersona() clears personaBlock so the next BuildContext
//     reflects the new personality. When the persona is configured to
//     replace the default system prompt (OverrideSystemPrompt), the engine's
//     client already carries its prompt as THE system prompt — the direction
//     and summary lines are skipped here and only the persona's memories
//     are injected.
func (s *Session) BuildContext() []llm.Message {
        if s.contextCacheValid {
                return s.contextCache
        }
        entries := s.manager.BuildContext()
        msgs := make([]llm.Message, 0, len(entries)+2)

        // Persona framing + per-persona memory bank. Frozen once per persona
        // epoch: the first build captures the injection, and every later
        // rebuild (cache busts fire on every Append) re-prepends the same
        // bytes. The freeze is what keeps the request prefix byte-stable —
        // re-reading live persona memories per build would flip the prefix
        // whenever the bank changed, re-billing the whole context — and it
        // is also what keeps the persona from silently vanishing after the
        // first cache bust, which the old one-shot flag allowed. A persona
        // switch clears the block via SetPersona() and re-freezes.
        if s.personaBlock == nil && s.personaStore != nil {
                var block []llm.Message
                s.injectPersonaContext(&block)
                s.personaBlock = block
        }
        if len(s.personaBlock) > 0 {
                msgs = append(msgs, s.personaBlock...)
        }

        // JSONL memory bank injection is disabled — the semantic memory
        // inlet (engine.injectSemanticMemories) now handles auto-injection
        // with better relevance scoring via vector retrieval. Keeping the
        // legacy JSONL bank injection would double-inject the same memories
        // and waste ~200 tokens per turn. If semantic memory is not
        // configured, the explicit remember/recall tools still work against
        // engine.memStore.
        s.memoryInjected = true
        for _, entry := range entries {
                switch entry.GetType() {
                case session.EntryCompaction:
                        m := entry.(session.CompactionEntry)
                        msgs = append(msgs, llm.Message{
                                Role:    llm.RoleUser,
                                Content: m.Compaction.Summary,
                        })
                case session.EntryMessage:
                        m := entry.(session.SessionMessageEntry)
                        msgs = append(msgs, m.Message)
                }
        }
        s.contextCache = msgs
        s.contextCacheValid = true
        return msgs
}

// injectPersonaContext prepends the active persona's direction, framing line
// and recent per-persona memory bank entries to msgs in place. Returns true
// when a non-default persona is active (i.e. the persona's own bank is now the
// memory target and its entries were surfaced here). No-op when the persona
// store is unset or the active persona is the default (which inherits the
// base system prompt and framing unchanged, and keeps the legacy session
// bank).
func (s *Session) injectPersonaContext(msgs *[]llm.Message) bool {
        if s.personaStore == nil {
                return false
        }
        p, err := s.personaStore.ActivePersona()
        if err != nil {
                return false
        }
        if p.ID == persona.DefaultPersonaID {
                return false
        }
        // The persona's own prompt — the "Personality" textarea in the
        // manager — is the actual directive. It used to sit in the registry
        // unused: only the one-line summary below reached the model, so
        // switching personas changed the label but not the behavior. Inject
        // it verbatim as its own system message, ahead of the summary.
        //
        // A persona with OverrideSystemPrompt skips this injection: the
        // engine already installed its prompt as THE system prompt, and
        // repeating it here (wrapped in "follow it over your default
        // voice") would reintroduce exactly the framing the override is
        // meant to remove.
        if !p.ReplacesSystemPrompt() {
                if prompt := strings.TrimSpace(s.personaStore.SystemPromptOverride(p)); prompt != "" {
                        content := fmt.Sprintf("Persona direction for %s — follow it over your default voice:\n\n%s", p.Name, prompt)
                        *msgs = append(*msgs, llm.Message{Role: llm.RoleSystem, Content: content})
                }
                if summary := s.personaStore.PersonaSummary(p); summary != "" {
                        *msgs = append(*msgs, llm.Message{Role: llm.RoleSystem, Content: summary})
                }
        }
        // Per-persona memory bank: separate from session memory, prefixed with
        // "persona:" namespace inside the shared memory dir. When a non-default
        // persona is active this IS the memory target for remember/recall, so
        // surfacing it here covers both the persona framing and the memory
        // injection in one message. RecentMemories skips tool-run traces: ten
        // bash calls would otherwise crowd the window and push real facts out.
        if bank, err := s.personaStore.MemoryBank(p); err == nil && bank != nil {
                if recent, err := bank.RecentMemories(10); err == nil && len(recent) > 0 {
                        var sb strings.Builder
                        sb.WriteString("Persona memory for ")
                        sb.WriteString(p.Name)
                        sb.WriteString(" (recent decisions, errors, context):\n")
                        for _, e := range recent {
                                sb.WriteString(
                                        fmt.Sprintf("- [%s] %s: %s\n", e.Timestamp.Format("2006-01-02T15:04:05Z"), e.Type, e.Content),
                                )
                        }
                        *msgs = append(*msgs, llm.Message{Role: llm.RoleSystem, Content: sb.String()})
                }
        }
        return true
}

// SetPersona changes the active persona and invalidates the cached context so
// the next BuildContext reflects the new personality. Errors from the persona
// store are returned as-is. Calling SetPersona("") reverts to the default.
func (s *Session) SetPersona(id string) error {
        if s.personaStore == nil {
                return errors.New("agent: persona store not configured on this session")
        }
        if id == "" {
                id = persona.DefaultPersonaID
        }
        if err := s.personaStore.SetActive(id); err != nil {
                return err
        }
        // Clear the frozen persona block so the next BuildContext re-runs
        // the persona injection with the new active persona and re-freezes
        // it. The session-memory injections are untouched because they are
        // not persona-dependent.
        s.personaBlock = nil
        s.invalidateContextCache()
        return nil
}

// Len returns the number of stored entries (including the session header).
func (s *Session) Len() int {
        return s.manager.Len()
}

// LastID returns the ID of the most recently appended entry.
func (s *Session) LastID() string {
        return s.lastID
}

// AddUser records a single user message for this chat turn.
func (s *Session) AddUser(text string) error {
        return s.Append(llm.Message{
                Role:    llm.RoleUser,
                Content: text,
        })
}

// AddAssistant records an assistant message from an LLM response (including tool_calls).
func (s *Session) AddAssistant(assistant llm.Message, usage llm.Usage) error {
        return s.Append(llm.Message{
                Role:             llm.RoleAssistant,
                Content:          assistant.Content,
                ReasoningContent: assistant.ReasoningContent,
                ToolCalls:        assistant.ToolCalls,
                Usage:            usage,
        })
}

// AddFinalAssistant records the last assistant message when it carries text or tool_calls.
func (s *Session) AddFinalAssistant(final *llm.Message) error {
        if final == nil {
                return nil
        }
        if strings.TrimSpace(final.Content) == "" && len(final.ToolCalls) == 0 {
                return nil
        }
        return s.AddAssistant(*final, final.Usage)
}
