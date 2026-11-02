package session

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/vault"
)

// defaultVault is the process-wide vault handle used by the path-based
// resolution helpers (OpenSession, FindSessionFile, ListSessions) so the
// file-era signatures keep working when sessions live in the vault.
// Surfaces set it once at startup; tests leave it nil (pure file mode).
var (
	defaultVaultMu sync.RWMutex
	defaultVault   *vault.Vault
)

// SetDefaultVault installs the process-wide vault handle (Bet 08). Nil
// restores pure file mode.
func SetDefaultVault(v *vault.Vault) {
	defaultVaultMu.Lock()
	defer defaultVaultMu.Unlock()
	defaultVault = v
}

// DefaultVault returns the installed handle (nil in file mode).
func DefaultVault() *vault.Vault {
	defaultVaultMu.RLock()
	defer defaultVaultMu.RUnlock()
	return defaultVault
}

// Backing persists one session's entries (Bet 08). The JSONL file is the
// original backing; vault rows are the durable-store backing. Lines are
// the same JSON either way, so replay is byte-identical.
type Backing interface {
	// Append inserts entry lines (one flush batch).
	Append(lines []string) error
	// Load returns every entry line in order.
	Load() ([]string, error)
	// Path is the session's identity path (real file or synthetic).
	Path() string
}

// vaultBacking persists one session's entries as rows in the vault.
type vaultBacking struct {
	v       *vault.Vault
	project string
	id      string
	ts      string
	cwd     string
	parent  string
	created time.Time

	mu      sync.Mutex
	nextSeq int64
	idToSeq map[string]int64
	started bool
}

// NewVaultManager creates a session Manager backed by vault rows. The
// session row itself is inserted lazily at first flush — an unflushed
// session (no assistant message yet) leaves no rows behind, matching
// the file era's nothing-on-disk-until-first-flush behavior.
func NewVaultManager(cwd, project, parentID string, v *vault.Vault, opt ...ManagerOption) (*Manager, error) {
	if v == nil {
		return nil, fmt.Errorf("session: vault is required")
	}
	var config ManagerConfig
	for _, o := range opt {
		config = o.Apply(config)
	}
	config.shouldFlush = true

	sessionID := generateSessionID()
	ts := time.Now().Format("2006-01-02T15-04-05")
	b := &vaultBacking{
		v: v, project: project, ts: ts, cwd: cwd, parent: parentID,
		created: time.Now().UTC(), id: sessionID,
		idToSeq: make(map[string]int64),
	}
	header := SessionHeader{
		Type:          EntrySession,
		ParentSession: parentID,
		ID:            sessionID,
		Timestamp:     time.Now().Format("2006-01-02T15-04-05"),
		Cwd:           cwd,
	}
	m := &Manager{
		cwd:         cwd,
		config:      config,
		entries:     make([]MessageEntry, 0, 64),
		byIDs:       make(map[string]MessageEntry, 64),
		leafID:      nil,
		sessionID:   sessionID,
		flushed:     false,
		shouldFlush: true,
		backing:     b,
	}
	m.sessionFile = b.Path()
	m.entries = append(m.entries, header)
	return m, nil
}

// Path implements Backing: the synthetic identity path.
func (b *vaultBacking) Path() string {
	return vault.SessionPath(b.project, b.ts, b.id)
}

// Append implements Backing: insert entry lines, creating the session
// row on first flush.
func (b *vaultBacking) Append(lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		if err := b.v.PutSession(vault.SessionRow{
			ID: b.id, Project: b.project, Ts: b.ts,
			ParentSession: b.parent, Cwd: b.cwd, CreatedAt: b.created,
		}); err != nil {
			return err
		}
		b.started = true
	}
	rows := make([]vault.SessionEntryRow, 0, len(lines))
	for _, line := range lines {
		var probe struct {
			Type     string  `json:"type"`
			ID       string  `json:"id"`
			ParentID *string `json:"parentID"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		seq := b.nextSeq
		b.nextSeq++
		if probe.ID != "" {
			b.idToSeq[probe.ID] = seq
		}
		var parentSeq *int64
		if probe.ParentID != nil {
			if ps, ok := b.idToSeq[*probe.ParentID]; ok {
				psCopy := ps
				parentSeq = &psCopy
			}
		}
		kind := probe.Type
		if kind == "" {
			kind = EntryMessage
		}
		rows = append(rows, vault.SessionEntryRow{
			SessionID: b.id, Seq: seq, ParentSeq: parentSeq, Kind: kind, Payload: line,
		})
	}
	return b.v.AppendSessionEntries(rows)
}

// Load implements Backing: replay the vault rows as lines.
func (b *vaultBacking) Load() ([]string, error) {
	return b.v.LoadSessionEntries(b.id)
}

// openVaultSession rebuilds a Manager from vault rows: decode lines in
// order (the same walk OpenSession does), then re-attach a backing so
// further appends land in the same vault session.
func openVaultSession(v *vault.Vault, id string) (*Manager, error) {
	row, ok, err := v.GetSession(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("session: %s not in vault", id)
	}
	lines, err := v.LoadSessionEntries(id)
	if err != nil {
		return nil, err
	}
	m, err := managerFromLines(lines, vault.SessionPath(row.Project, row.Ts, row.ID))
	if err != nil {
		return nil, err
	}
	b := &vaultBacking{
		v: v, project: row.Project, ts: row.Ts, id: row.ID,
		cwd: row.Cwd, parent: row.ParentSession,
		created: row.CreatedAt, started: true,
		nextSeq: int64(len(lines)), idToSeq: make(map[string]int64, len(lines)),
	}
	for _, line := range lines {
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err == nil && probe.ID != "" {
			// Later duplicates win, same as replay's byIDs overwrite.
			b.idToSeq[probe.ID] = int64(len(b.idToSeq))
		}
	}
	m.backing = b
	return m, nil
}

// managerFromLines is the shared replay walk: decode entry lines, enforce
// header-first, rebuild the tree index. The returned Manager is fully
// functional; callers attach a backing for continued persistence.
func managerFromLines(lines []string, path string) (*Manager, error) {
	var (
		entries         []MessageEntry
		byIDs           = make(map[string]MessageEntry, 64)
		header          *SessionHeader
		leafID          *string
		hasAssistantMsg bool
	)
	for i, raw := range lines {
		line := []byte(raw)
		if len(trimSpaceBytes(line)) == 0 {
			continue
		}
		entry, err := decodeEntryLine(line, i+1)
		if err != nil {
			return nil, err
		}
		switch e := entry.(type) {
		case SessionHeader:
			if header != nil {
				return nil, fmt.Errorf("session: duplicate header at %s:%d", path, i+1)
			}
			h := e
			header = &h
			entries = append(entries, h)
		default:
			if header == nil {
				return nil, fmt.Errorf("session: first entry must be session header at %s:%d", path, i+1)
			}
			id := entry.GetID()
			byIDs[id] = entry
			entries = append(entries, entry)
			leafID = &id
			if msg, ok := entry.(SessionMessageEntry); ok && msg.Message.Role == "assistant" {
				hasAssistantMsg = true
			}
		}
	}
	if header == nil {
		return nil, fmt.Errorf("session: missing header in %s", path)
	}
	return &Manager{
		cwd:             header.Cwd,
		entries:         entries,
		byIDs:           byIDs,
		sessionFile:     path,
		leafID:          leafID,
		shouldFlush:     true,
		flushed:         true,
		sessionID:       header.ID,
		config:          ManagerConfig{sessionDir: parentDir(path), shouldFlush: true, parentID: header.ParentSession},
		hasAssistantMsg: hasAssistantMsg,
	}, nil
}

func trimSpaceBytes(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

func parentDir(path string) string {
	for i := len(path) - 1; i > 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return ""
}
