package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/session"
)

// pinnedSessions is the web surface's session-pin list: a JSON file under the
// project session directory, so pins travel with the workspace and the GUI never
// writes to the config file the CLI and TUI read. Writes replace the whole list
// (a pin toggle is a read-modify-write under the mutex, never a partial edit).
type pinnedSessions struct {
	mu   sync.Mutex
	path string
}

// pinnedFile is the on-disk shape. Sessions is the ordered pin list, newest
// pin first.
type pinnedFile struct {
	Sessions []string `json:"sessions"`
}

func newPinnedSessions(dir string) *pinnedSessions {
	return &pinnedSessions{path: filepath.Join(dir, "pins.json")}
}

// list returns the pinned session ids in order.
func (p *pinnedSessions) list() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.read()
}

// toggle adds id to the pin list or removes it, reporting whether it is now
// pinned. An empty directory or an unreadable file reads as "nothing pinned".
func (p *pinnedSessions) toggle(id string) (pinned bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.path == "" {
		return false, errors.New("web: pin store is not configured")
	}
	current := p.read()
	out := make([]string, 0, len(current)+1)
	found := false
	for _, s := range current {
		if s == id {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		out = append([]string{id}, out...)
	}
	if err := p.write(out); err != nil {
		return !found, err
	}
	return !found, nil
}

// read locks nothing: callers hold p.mu.
func (p *pinnedSessions) read() []string {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil
	}
	var f pinnedFile
	if err := json.Unmarshal(data, &f); err != nil {
		// A corrupt pin file is cosmetic: an empty list degrades to "no pins",
		// which is what the sidebar renders for a fresh workspace anyway.
		return nil
	}
	out := make([]string, 0, len(f.Sessions))
	for _, s := range f.Sessions {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// write locks nothing: callers hold p.mu. It creates the parent directory so a
// first pin on a project that has never had a session works.
func (p *pinnedSessions) write(ids []string) error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return fmt.Errorf("web: create pin dir: %w", err)
	}
	data, err := json.Marshal(pinnedFile{Sessions: ids})
	if err != nil {
		return fmt.Errorf("web: encode pins: %w", err)
	}
	// 0600: pins are user-local state, not project config other users read.
	if err := os.WriteFile(p.path, data, 0o600); err != nil {
		return fmt.Errorf("web: write pins: %w", err)
	}
	return nil
}

// sessionTitle resolves a session id to its sidebar label: the first user
// message, trimmed. It falls back to the id's short form for a session whose
// file is unreadable or has no user text, so the topbar never shows a bare
// 32-char hex string.
func (s *Server) sessionTitle(id string) (string, error) {
	if s.proj == nil {
		return "", errors.New("web: project is not configured")
	}
	sessions, err := s.listSessions()
	if err != nil {
		return "", fmt.Errorf("web: list sessions: %w", err)
	}
	for _, m := range sessions {
		if m.ID == id {
			if t := strings.TrimSpace(m.Preview); t != "" {
				return t, nil
			}
			break
		}
	}
	return shortSessionID(id), nil
}

// shortSessionID renders an id as a stable, readable fallback label.
func shortSessionID(id string) string {
	if len(id) > 12 {
		return id[:12] + "…"
	}
	return id
}

// sessionTitleFromMeta maps a listing row to its display label.
func sessionTitleFromMeta(m session.SessionMeta) string {
	if t := strings.TrimSpace(m.Preview); t != "" {
		return t
	}
	return shortSessionID(m.ID)
}
