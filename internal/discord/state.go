package discord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is the bridge's persistence file (~/.a1/discord/state.json): the
// bridged session id, so a restart resumes the conversation instead of
// amnesia. That is ALL it holds — the bot token is config's business and
// is never cached to disk by the bridge (a stable credential the user
// pasted on purpose).
type State struct {
	path string

	// SessionID is the loopback session the channel is bridged onto. ""
	// means the next message starts a fresh conversation.
	SessionID string `json:"session,omitempty"`
}

// LoadState reads the state file. A missing file is an empty state, not
// an error: the first boot has nothing to remember.
func LoadState(path string) *State {
	s := &State{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	// A corrupt state file degrades to empty rather than failing the boot —
	// the worst case is a fresh session, which is recoverable by asking
	// again; refusing to start is not.
	_ = json.Unmarshal(data, s)
	return s
}

// Save writes the state back, creating the parent directory.
func (s *State) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("discord: state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("discord: encode state: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("discord: write state: %w", err)
	}
	return nil
}
