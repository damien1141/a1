package permission

import (
	"errors"
	"fmt"
	"sync"
)

// EffectEntry records a committed tool effect.
type EffectEntry struct {
	Tool        string
	EffectToken string
}

// EffectLog is an append-only record of committed effects.
type EffectLog struct {
	mu      sync.Mutex
	entries []EffectEntry
	index   map[string]int
}

// NewEffectLog creates an empty effect log.
func NewEffectLog() *EffectLog {
	return &EffectLog{index: make(map[string]int)}
}

// Commit appends an effect token atomically. Empty tokens are ignored.
func (l *EffectLog) Commit(tool, token string) {
	if token == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, EffectEntry{Tool: tool, EffectToken: token})
	l.index[token]++
}

// Has reports whether token exists in the log.
func (l *EffectLog) Has(token string) bool {
	if token == "" || l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.index[token] > 0
}

// Count returns how many times token appears in the log.
func (l *EffectLog) Count(token string) int {
	if token == "" || l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.index[token]
}

// Entries returns a snapshot of committed entries.
func (l *EffectLog) Entries() []EffectEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]EffectEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// ErrMissingEffect is returned when a required effect token is absent.
var ErrMissingEffect = errors.New("required effect token not found")

// CheckRequiredEffects returns nil if all required tokens exist in the log.
func (l *EffectLog) CheckRequiredEffects(required []string) error {
	if l == nil || len(required) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, token := range required {
		if l.index[token] == 0 {
			return fmt.Errorf("%s: %s", ErrMissingEffect, token)
		}
	}
	return nil
}

// CheckNoPrior returns nil if token does NOT exist in the log.
func (l *EffectLog) CheckNoPrior(token string) error {
	if token == "" || l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.index[token] > 0 {
		return fmt.Errorf("effect already committed: %s", token)
	}
	return nil
}
