package matrix

import (
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
)

// State is the bridge's persistence file (~/.a1/matrix/state.json): the
// bridged session id, so a restart resumes the conversation instead of
// amnesia, and the cached Matrix token, so password logins do not mint a
// fresh device session on every boot (homeservers cap sessions and start
// evicting the oldest — including the one element is using).
//
// The file is deliberately not the config file's business: config is what
// the user wrote, state is what the bridge learned.
type State struct {
        path string

        // SessionID is the loopback session the room is bridged onto. "" means
        // the next message starts a fresh conversation.
        SessionID string `json:"session,omitempty"`
        // AccessToken is the cached login token (empty when config authenticates
        // with an explicit token, which always wins).
        AccessToken string `json:"access_token,omitempty"`
        // UserID is the account the cached token belongs to — informational, the
        // whoami handshake re-derives the authoritative value.
        UserID string `json:"user_id,omitempty"`
}

// LoadState reads the state file. A missing file is an empty state, not an
// error: the first boot has nothing to remember.
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

// Save writes the state back, creating the parent directory. Token-bearing
// file, so 0600.
func (s *State) Save() error {
        if s.path == "" {
                return nil
        }
        if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
                return fmt.Errorf("matrix: state dir: %w", err)
        }
        data, err := json.MarshalIndent(s, "", "  ")
        if err != nil {
                return fmt.Errorf("matrix: encode state: %w", err)
        }
        if err := os.WriteFile(s.path, data, 0o600); err != nil {
                return fmt.Errorf("matrix: write state: %w", err)
        }
        return nil
}
