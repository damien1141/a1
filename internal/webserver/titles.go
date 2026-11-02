package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/llm"
	llmclient "github.com/damien1141/a1/internal/llm/client"
	"github.com/damien1141/a1/internal/session"
)

// titles.go — smart session names. After a session's first exchange the
// web server asks the memory-ops model (the persona's dedicated cheap
// second model, falling back to the main one) for a 2–6 word title and
// stores it beside the session data. The sidebar and topbar then show a
// real name instead of a truncated first message.
//
// Storage is a JSON file under the project session directory (next to
// pins.json), deliberately NOT the vault or config: titles are per-project
// UI state that must apply to both file-backed and vault-backed sessions
// without a schema migration, and a corrupt file degrades to "no titles".

const (
	titleMaxTokens = 32
	titleMaxRunes  = 64
	titleTimeout   = 45 * time.Second
)

// sessionTitles is the id→title store. Writes replace the whole file (a
// title set is a rare, small read-modify-write under the mutex).
type sessionTitles struct {
	mu   sync.Mutex
	path string
	// attempted is deliberately in-memory: one naming attempt per session
	// per SERVER RUN. Persisting it (as an earlier version did) let a
	// single failed call — nil ops client, one 429 — permanently silence
	// naming for that session, surviving restarts, for a cosmetic label.
	attempted map[string]bool
}

type titlesFile struct {
	Titles map[string]string `json:"titles"`
}

func newSessionTitles(dir string) *sessionTitles {
	return &sessionTitles{path: filepath.Join(dir, "titles.json")}
}

// snapshot returns the persisted titles (never nil; a missing or corrupt
// file reads as empty — titles are cosmetic, never load-bearing).
func (t *sessionTitles) snapshot() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.read()
	return f.Titles
}

// get returns one session's title ("" when absent).
func (t *sessionTitles) get(id string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.read().Titles[id]
}

// set stores a title.
func (t *sessionTitles) set(id, title string) {
	if id == "" || strings.TrimSpace(title) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	f := t.read()
	if f.Titles == nil {
		f.Titles = map[string]string{}
	}
	f.Titles[id] = strings.TrimSpace(title)
	delete(t.attempted, id)
	t.write(f)
}

// attemptedHas reports whether a naming pass already ran for id during
// this server run.
func (t *sessionTitles) attemptedHas(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.attempted[id]
}

// markAttempted records that a naming pass ran for id, so a failed
// generation (provider down, empty reply) is not retried on every turn —
// once per server run per session is plenty for a cosmetic label. The
// mark is in-memory: a restart may try again, which is the point.
func (t *sessionTitles) markAttempted(id string) {
	if id == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.attempted == nil {
		t.attempted = map[string]bool{}
	}
	t.attempted[id] = true
}

// read locks nothing: callers hold t.mu.
func (t *sessionTitles) read() titlesFile {
	if t.path == "" {
		return titlesFile{}
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return titlesFile{}
	}
	var f titlesFile
	if err := json.Unmarshal(data, &f); err != nil {
		// Corrupt titles degrade to none — never break the sidebar over a
		// cosmetic store.
		return titlesFile{}
	}
	if f.Titles == nil {
		f.Titles = map[string]string{}
	}
	return f
}

// write locks nothing: callers hold t.mu.
func (t *sessionTitles) write(f titlesFile) {
	if t.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(t.path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(f)
	if err != nil {
		return
	}
	// 0600: user-local UI state.
	_ = os.WriteFile(t.path, data, 0o600)
}

// titlesStore returns the server's title store, initializing on first use
// like every other lazily-built registry.
func (s *Server) titlesStore() *sessionTitles {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.titles == nil {
		dir := ""
		if s.proj != nil {
			dir = s.proj.SessionDir()
		}
		s.titles = newSessionTitles(dir)
	}
	return s.titles
}

// maybeNameSession generates a smart title for a session right after a
// turn completes — but only when it has none yet (first exchange, or a
// pre-feature session's next turn). The call runs on a background
// goroutine with its own timeout: naming must never delay the turn's done
// event or its toast.
func (s *Server) maybeNameSession(sessionID string, le *liveEngine) {
	if s.proj == nil || s.proj.Config() == nil {
		return
	}
	if !s.proj.Config().Web.AutoNameSessionsResolved() {
		return
	}
	titles := s.titlesStore()
	if titles.get(sessionID) != "" {
		return
	}
	if titles.attemptedHas(sessionID) {
		return
	}

	users := pathUserEntries(le)
	if len(users) == 0 {
		return
	}
	userText := users[0].text
	assistantText := lastAssistantText(le)
	if strings.TrimSpace(userText) == "" && strings.TrimSpace(assistantText) == "" {
		return
	}
	if le.engine.OpsClient() == nil {
		// No memory-ops model configured: a config gap, not an attempt.
		// Leave the session unnamed and retry-eligible; re-checking per
		// turn costs nothing.
		return
	}

	go func() {
		// Attempt-mark BEFORE generating: a provider error on the naming
		// call must not retry on every subsequent turn.
		titles.markAttempted(sessionID)
		ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
		defer cancel()
		title, err := generateSessionTitle(ctx, le.engine.OpsClient(), userText, assistantText)
		if err != nil {
			debuglog.Logf("web: session naming failed for %s: %v", sessionID, err)
			return
		}
		titles.set(sessionID, title)
		// Tell the tab following this conversation its session got a name
		// (the sidebar row refreshes on the next list reload).
		s.events().publish(sessionID, serverEvent{
			Kind:  eventToast,
			Toast: &toastPayload{Level: toastInfo, Message: "Session named — " + title, Duration: 2500},
		})
	}()
}

// lastAssistantText returns the most recent assistant text on the path
// (trimmed for the naming prompt; "" when the turn produced none).
func lastAssistantText(le *liveEngine) string {
	entries := le.engine.Session().PathEntries()
	for i := len(entries) - 1; i >= 0; i-- {
		me, ok := entries[i].(session.SessionMessageEntry)
		if !ok || me.Message.Role != llm.RoleAssistant {
			continue
		}
		return me.Message.Content
	}
	return ""
}

// generateSessionTitle asks the ops model for a short session name. The
// prompt is deliberately tiny (both excerpts truncated hard) so the call
// costs pennies on the main model and is instant on a local 8B.
func generateSessionTitle(ctx context.Context, client *llmclient.Client, userText, assistantText string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("no ops client")
	}
	var b strings.Builder
	b.WriteString("You name chat sessions. Read the exchange below and reply with ONLY a short session title: 2 to 6 words, plain text, no quotes, no trailing punctuation, no emoji, no prefix like \"Title:\".\n\n")
	b.WriteString("User:\n")
	b.WriteString(truncateRunes(userText, 400))
	b.WriteString("\n\nAssistant:\n")
	b.WriteString(truncateRunes(assistantText, 600))
	b.WriteString("\n\nTitle:")

	resp, err := client.Compact(ctx, llm.CompactRequest{Prompt: b.String(), MaxTokens: titleMaxTokens})
	if err != nil {
		return "", err
	}
	return sanitizeTitle(resp.Text), nil
}

// sanitizeTitle forces the model's reply into a single clean line: strip
// wrapping quotes and title prefixes, collapse whitespace, cap the length.
// An over-long title is truncated at a word boundary so the sidebar never
// wraps.
func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "Title:")
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"'“”‘’`")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ".!?:;，。！？")
	if s == "" {
		return ""
	}
	if len([]rune(s)) > titleMaxRunes {
		s = string([]rune(s)[:titleMaxRunes])
		if idx := strings.LastIndexByte(s, ' '); idx > titleMaxRunes/2 {
			s = s[:idx]
		}
	}
	return s
}

// truncateRunes caps a string at n runes with an ellipsis marker.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
