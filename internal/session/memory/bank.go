package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EntryType classifies a memory bank record.
type EntryType string

const (
	EntryDecision EntryType = "decision"
	EntryError    EntryType = "error"
	EntryContext  EntryType = "context"
	EntryInput    EntryType = "input"
)

// Entry is one memory bank record.
type Entry struct {
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      EntryType `json:"type"`
	Content   string    `json:"content"`
}

// Bank is a session-scoped append-only memory store backed by a JSONL file.
type Bank struct {
	path    string
	mu      sync.Mutex
	once    sync.Once
	entries []Entry
	loaded  bool
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
	return &Bank{path: path}, nil
}

// Append adds a new entry to the bank.
func (b *Bank) Append(entryType EntryType, content string) error {
	if b == nil {
		return nil
	}
	entry := Entry{
		SessionID: sessionIDFromPath(b.path),
		Timestamp: time.Now().UTC(),
		Type:      entryType,
		Content:   strings.TrimSpace(content),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("memory: encode entry: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.OpenFile(b.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memory: open bank: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("memory: write entry: %w", err)
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

// Path returns the backing file path.
func (b *Bank) Path() string {
	if b == nil {
		return ""
	}
	return b.path
}

func (b *Bank) load() {
	b.entries = nil
	f, err := os.Open(b.path)
	if err != nil {
		return
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
		b.entries = append(b.entries, entry)
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
