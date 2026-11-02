package job

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// handoffFile is the structured-result companion to result.md: the block a
// parent engine merges into its context when it waits on a child (Bet 04
// structured handoff).
const handoffFile = "handoff.json"

// Handoff is the structured result of one job: the prose summary plus the
// machine-checkable parts a fleet parent needs — which files the child
// touched and where every observed check stands. Written by runners that
// can produce it (agent.EngineRunner), read by agent_wait.
type Handoff struct {
	// Summary is the child's final prose reply (same content as result.md).
	Summary string `json:"summary"`
	// FilesRead / FilesModified are the workspace paths the child read and
	// modified, deduplicated and sorted.
	FilesRead     []string `json:"files_read,omitempty"`
	FilesModified []string `json:"files_modified,omitempty"`
	// Checks is the current state of every evidence check the child's
	// ledger observed, sorted by check id.
	Checks []HandoffCheck `json:"checks,omitempty"`
}

// HandoffCheck is one evidence check in a [Handoff].
type HandoffCheck struct {
	Check  string `json:"check"`
	Status string `json:"status"` // "pass" | "fail"
	Detail string `json:"detail,omitempty"`
}

// HandoffPath returns the handoff file path for a job dir ("" → "").
func HandoffPath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, handoffFile)
}

// WriteHandoff persists h to <dir>/handoff.json (atomic tmp+rename).
func WriteHandoff(dir string, h Handoff) error {
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return err
	}
	path := HandoffPath(dir)
	if path == "" {
		return os.ErrInvalid
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil { //nolint:gosec // G306: job handoffs are local tooling state
		return err
	}
	return os.Rename(tmp, path)
}

// ReadHandoff loads <dir>/handoff.json. A missing file is not an error:
// jobs run by runners without handoff support (or pre-Bet-04 jobs) report
// ok=false and the caller falls back to the prose summary alone.
func ReadHandoff(dir string) (Handoff, bool) {
	path := HandoffPath(dir)
	if path == "" {
		return Handoff{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Handoff{}, false
	}
	var h Handoff
	if err := json.Unmarshal(data, &h); err != nil {
		return Handoff{}, false
	}
	return h, true
}
