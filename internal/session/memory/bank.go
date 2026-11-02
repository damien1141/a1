package memory

import (
        "bufio"
        "bytes"
        "encoding/json"
        "errors"
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "strings"
        "sync"
        "time"
)

// ErrNotFound is returned by DeleteAt when the index is out of range.
var ErrNotFound = errors.New("memory: entry not found")

// EntryType classifies a memory bank record.
type EntryType string

const (
        EntryDecision EntryType = "decision"
        EntryError    EntryType = "error"
        EntryContext  EntryType = "context"
        EntryInput    EntryType = "input"
        // EntryTrace marks per-tool-run bookkeeping the executor appends after
        // every successful tool call. Traces are a replay aid, not memories: the
        // persona manager's count/list and the context injections skip them so
        // "N memories" means facts worth keeping, not a tool-call log.
        EntryTrace EntryType = "trace"
        // EntryMemory is a fact the agent itself chose to remember (the remember
        // tool mirrors its save here so persona-scoped banks surface it).
        EntryMemory EntryType = "memory"
)

// Entry is one memory bank record.
type Entry struct {
        SessionID string    `json:"session_id"`
        Timestamp time.Time `json:"timestamp"`
        Type      EntryType `json:"type"`
        Content   string    `json:"content"`
}

// Bank is a session-scoped append-only memory store. The backing is a
// JSONL file (the original) or vault rows (Bet 08) — both keep the exact
// same lazy-load, index-handle, and delete semantics.
type Bank struct {
        store  bankStore
        mu     sync.Mutex
        once   sync.Once
        entries []Entry
        loaded bool
}

// bankStore is the persistence seam under a Bank: append one entry, load
// all entries oldest-first, or rewrite the whole set (the DeleteAt
// path). ID is the bank's identity (session or "persona:<id>").
type bankStore interface {
        ID() string
        Append(entry Entry) error
        Load() ([]Entry, error)
        ReplaceAll(entries []Entry) error
        PathLabel() string
}

// jsonlStore is the original file backing.
type jsonlStore struct {
        path string
}

func (j jsonlStore) ID() string      { return sessionIDFromPath(j.path) }
func (j jsonlStore) PathLabel() string { return j.path }

func (j jsonlStore) Append(entry Entry) error {
        line, err := json.Marshal(entry)
        if err != nil {
                return fmt.Errorf("memory: encode entry: %w", err)
        }
        f, err := os.OpenFile(j.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
        if err != nil {
                return fmt.Errorf("memory: open bank: %w", err)
        }
        defer f.Close()
        if _, err := f.Write(append(line, '\n')); err != nil {
                return fmt.Errorf("memory: write entry: %w", err)
        }
        return nil
}

func (j jsonlStore) Load() ([]Entry, error) {
        var entries []Entry
        f, err := os.Open(j.path)
        if err != nil {
                if os.IsNotExist(err) {
                        return nil, nil
                }
                return nil, fmt.Errorf("memory: open bank: %w", err)
        }
        defer f.Close()
        sc := bufio.NewScanner(f)
        sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
        for sc.Scan() {
                line := strings.TrimSpace(sc.Text())
                if line == "" {
                        continue
                }
                var entry Entry
                if err := json.Unmarshal([]byte(line), &entry); err != nil {
                        continue
                }
                entries = append(entries, entry)
        }
        return entries, sc.Err()
}

func (j jsonlStore) ReplaceAll(entries []Entry) error {
        var buf bytes.Buffer
        enc := json.NewEncoder(&buf)
        for _, e := range entries {
                if err := enc.Encode(e); err != nil {
                        return fmt.Errorf("memory: encode entry: %w", err)
                }
        }
        if err := os.WriteFile(j.path, buf.Bytes(), 0o644); err != nil { //nolint:gosec // G306: bank file is user-readable by design
                return fmt.Errorf("memory: rewrite bank: %w", err)
        }
        return nil
}

// OpenBank opens or creates a memory bank for the given session ID under
// memoryDir (typically ~/.a1/sessions/memory).
func OpenBank(sessionID, memoryDir string) (*Bank, error) {
        sessionID = strings.TrimSpace(sessionID)
        if sessionID == "" {
                return nil, errors.New("memory: sessionID is required")
        }
        if memoryDir == "" {
                memoryDir = filepath.Join(projectGlobalRoot(), "sessions", "memory")
        }
        if err := os.MkdirAll(memoryDir, 0o755); err != nil {
                return nil, fmt.Errorf("memory: create memory dir: %w", err)
        }
        path := filepath.Join(memoryDir, sessionID+".jsonl")
        return &Bank{store: jsonlStore{path: path}}, nil
}

// Append adds a new entry to the bank.
func (b *Bank) Append(entryType EntryType, content string) error {
        if b == nil {
                return nil
        }
        entry := Entry{
                SessionID: b.store.ID(),
                Timestamp: time.Now().UTC(),
                Type:      entryType,
                Content:   strings.TrimSpace(content),
        }

        b.mu.Lock()
        defer b.mu.Unlock()

        if err := b.store.Append(entry); err != nil {
                return err
        }
        b.entries = append(b.entries, entry)
        return nil
}

// Recent returns the last n entries from the bank, newest first.
func (b *Bank) Recent(n int) ([]Entry, error) {
        if b == nil {
                return nil, nil
        }
        if n <= 0 {
                n = 10
        }

        b.mu.Lock()
        defer b.mu.Unlock()

        if !b.loaded {
                b.once.Do(b.load)
        }
        if len(b.entries) == 0 {
                return nil, nil
        }
        if n > len(b.entries) {
                n = len(b.entries)
        }
        out := make([]Entry, n)
        for i := range n {
                out[i] = b.entries[len(b.entries)-1-i]
        }
        return out, nil
}

// RecentInputHistory returns the last n input history entries, newest first.
func (b *Bank) RecentInputHistory(n int) ([]string, error) {
        if b == nil {
                return nil, nil
        }
        if n <= 0 {
                n = 50
        }

        b.mu.Lock()
        defer b.mu.Unlock()

        if !b.loaded {
                b.once.Do(b.load)
        }

        var inputs []string
        for i := len(b.entries) - 1; i >= 0 && len(inputs) < n; i-- {
                if b.entries[i].Type == EntryInput {
                        inputs = append(inputs, b.entries[i].Content)
                }
        }
        return inputs, nil
}

// IsTrace reports whether an entry is tool-run bookkeeping rather than a
// memory. Legacy banks recorded successful tool calls as plain "context"
// entries in the executor's "name(args) => result" shape; they are recognized
// by that marker so pre-existing banks stop counting spam as memories.
func IsTrace(e Entry) bool {
        if e.Type == EntryTrace {
                return true
        }
        return e.Type == EntryContext && traceShapeRe.MatchString(e.Content)
}

// traceShapeRe matches the executor's legacy "tool(args) => output" marker.
// The " => " arrow only ever appears in those appends; decision/context prose
// written by tools and users does not contain it.
var traceShapeRe = regexp.MustCompile(`\) => `)

// RecentMemories returns the last n entries that are NOT tool-run traces,
// newest first. It is what context injection and the persona manager read:
// the raw Recent includes per-call traces, which would crowd the ten most-
// recent slots with bash noise and push real decisions out of the window.
func (b *Bank) RecentMemories(n int) ([]Entry, error) {
        if b == nil {
                return nil, nil
        }
        if n <= 0 {
                n = 10
        }
        b.mu.Lock()
        defer b.mu.Unlock()
        if !b.loaded {
                b.once.Do(b.load)
        }
        out := make([]Entry, 0, n)
        for i := len(b.entries) - 1; i >= 0 && len(out) < n; i-- {
                if !IsTrace(b.entries[i]) {
                        out = append(out, b.entries[i])
                }
        }
        if len(out) == 0 {
                return nil, nil
        }
        return out, nil
}

// MemoryCount returns the number of entries that are not tool-run traces.
// The persona manager uses it as the bank's "N memories" figure.
func (b *Bank) MemoryCount() int {
        if b == nil {
                return 0
        }
        b.mu.Lock()
        defer b.mu.Unlock()
        if !b.loaded {
                b.once.Do(b.load)
        }
        count := 0
        for i := range b.entries {
                if !IsTrace(b.entries[i]) {
                        count++
                }
        }
        return count
}

// ListMemoriesIndexed returns non-trace entries paired with their raw bank
// index, oldest first. The raw index is the stable handle DeleteAt takes, so
// the persona manager can offer "forget" on a filtered view without the
// filtered position drifting from the backing file.
func (b *Bank) ListMemoriesIndexed() ([]IndexedEntry, error) {
        if b == nil {
                return nil, nil
        }
        b.mu.Lock()
        defer b.mu.Unlock()
        if !b.loaded {
                b.once.Do(b.load)
        }
        out := make([]IndexedEntry, 0, len(b.entries))
        for i, e := range b.entries {
                if !IsTrace(e) {
                        out = append(out, IndexedEntry{Index: i, Entry: e})
                }
        }
        return out, nil
}

// IndexedEntry pairs a bank entry with its raw index in the backing file.
type IndexedEntry struct {
        Index int
        Entry Entry
}

// List returns every entry in the bank, oldest first. The slice index of
// each entry IS its stable ID within one process lifetime — DeleteAt takes
// the same index. (Entries carry no persistent ID field; the persona
// memory manager uses the index as the handle and re-lists after every
// delete, so an index that went stale between list and delete can at worst
// drop the wrong line of an unchanged bank. The manager UI deletes one
// entry per action and refreshes immediately, which keeps that window
// practically closed.)
func (b *Bank) List() ([]Entry, error) {
        if b == nil {
                return nil, nil
        }
        b.mu.Lock()
        defer b.mu.Unlock()
        if !b.loaded {
                b.once.Do(b.load)
        }
        out := make([]Entry, len(b.entries))
        copy(out, b.entries)
        return out, nil
}

// DeleteAt removes the entry at index i (as returned by List) by rewriting
// the backing store without that line. The in-memory slice is kept in sync.
// Out-of-range indices return ErrNotFound so a stale UI handle fails loudly
// instead of silently dropping a neighboring memory.
func (b *Bank) DeleteAt(i int) error {
        if b == nil {
                return ErrNotFound
        }
        b.mu.Lock()
        defer b.mu.Unlock()
        if !b.loaded {
                b.once.Do(b.load)
        }
        if i < 0 || i >= len(b.entries) {
                return ErrNotFound
        }
        remaining := make([]Entry, 0, len(b.entries)-1)
        remaining = append(remaining, b.entries[:i]...)
        remaining = append(remaining, b.entries[i+1:]...)
        if err := b.store.ReplaceAll(remaining); err != nil {
                return err
        }
        b.entries = remaining
        return nil
}

// Len returns the number of loaded entries.
func (b *Bank) Len() int {
        if b == nil {
                return 0
        }
        b.mu.Lock()
        defer b.mu.Unlock()

        if !b.loaded {
                b.once.Do(b.load)
        }
        return len(b.entries)
}

// Path returns the backing file path (or "vault:<bank>" for row-backed
// banks).
func (b *Bank) Path() string {
        if b == nil {
                return ""
        }
        return b.store.PathLabel()
}

func (b *Bank) load() {
        entries, err := b.store.Load()
        if err == nil {
                b.entries = entries
        }
        b.loaded = true
}

func sessionIDFromPath(path string) string {
        base := filepath.Base(path)
        return strings.TrimSuffix(base, ".jsonl")
}

func projectGlobalRoot() string {
        home, err := os.UserHomeDir()
        if err != nil {
                return filepath.Join(os.TempDir(), ".a1")
        }
        return filepath.Join(home, ".a1")
}
