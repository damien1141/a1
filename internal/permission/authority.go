package permission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"
)

// CallHash returns a stable SHA-256 hex digest for the exact rendered call.
// It binds the ruling to tool name, paths/command, and input labels so the
// authority cannot approve a different call by accident.
func CallHash(req Request) string {
	h := sha256.New()
	fmt.Fprintf(h, "tool:%s\n", req.Tool)
	fmt.Fprintf(h, "action:%s\n", req.Action)
	if len(req.Paths) > 0 {
		sorted := make([]string, len(req.Paths))
		copy(sorted, req.Paths)
		sort.Strings(sorted)
		for _, p := range sorted {
			fmt.Fprintf(h, "path:%s\n", p)
		}
	}
	if req.Command != "" {
		fmt.Fprintf(h, "cmd:%s\n", req.Command)
	}
	for _, lab := range req.InputLabels {
		fmt.Fprintf(h, "trust:%d\n", lab.Trust)
		for _, r := range lab.ReaderSet {
			fmt.Fprintf(h, "reader:%s\n", r)
		}
		for _, e := range lab.EffectTokens {
			fmt.Fprintf(h, "effect:%s\n", e)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Authority evaluates a single call-scoped ruling for an exact call hash.
type Authority interface {
	Authorize(ctx context.Context, callHash string, req Request) (Decision, string)
}

// AuthorityFunc adapts a function to Authority.
type AuthorityFunc func(ctx context.Context, callHash string, req Request) (Decision, string)

// Authorize calls fn.
func (f AuthorityFunc) Authorize(ctx context.Context, callHash string, req Request) (Decision, string) {
	return f(ctx, callHash, req)
}

// Ruling records a single call-scoped authority decision.
type Ruling struct {
	// CallHash is the exact call hash the ruling is bound to.
	CallHash string
	// AuthorityName identifies which authority produced this ruling.
	AuthorityName string
	// GapsCovered lists the gaps this ruling addresses (e.g., "trust_floor", "narrowing").
	GapsCovered []string
	// Decision is the authority's decision.
	Decision Decision
	// Reason is the authority's explanation.
	Reason string
	// Timestamp is when the ruling was issued.
	Timestamp time.Time
}

// RulingLog is an append-only log of authority rulings for a trajectory.
type RulingLog struct {
	entries []Ruling
}

// NewRulingLog creates an empty ruling log.
func NewRulingLog() *RulingLog {
	return &RulingLog{}
}

// Append adds a ruling to the log.
func (l *RulingLog) Append(r Ruling) {
	l.entries = append(l.entries, r)
}

// All returns all logged rulings.
func (l *RulingLog) All() []Ruling {
	out := make([]Ruling, len(l.entries))
	copy(out, l.entries)
	return out
}

// Len returns the number of logged rulings.
func (l *RulingLog) Len() int {
	return len(l.entries)
}

// CoveredFor returns true if the log contains at least one ruling covering
// all of the requested gaps for the given call hash.
func (l *RulingLog) CoveredFor(callHash string, gaps ...string) bool {
	if len(gaps) == 0 {
		return false
	}
	needed := make(map[string]bool, len(gaps))
	for _, g := range gaps {
		needed[g] = true
	}
	for _, r := range l.entries {
		if r.CallHash != callHash {
			continue
		}
		for _, g := range r.GapsCovered {
			delete(needed, g)
		}
		if len(needed) == 0 {
			return true
		}
	}
	return false
}
