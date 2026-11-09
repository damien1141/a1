// Package sessiontool provides session_search: raw-transcript recall
// across a project's past sessions.
//
// The memory bank answers "what do I know about X" with distilled facts;
// it cannot answer "what did the user actually say about X last week" —
// that lives in the raw transcripts, which until now only the human
// could browse. This tool searches them: user, assistant, and tool-result
// text plus compaction summaries, case-insensitive, newest sessions
// first, grouped snippets with the match marked. The current session is
// excluded — its transcript is already the model's context, so searching
// it would spend tokens restating what it knows.
package sessiontool

import (
        "bufio"
        "context"
        "encoding/json"
        "fmt"
        "os"
        "strings"
        "time"
        "unicode/utf8"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

const (
        // defaultLimit / maxLimit bound the matching messages returned. The
        // tool's job is to find the needle, not replay the haystack: the model
        // follows up (read, resume, a narrower query) when it needs more.
        defaultLimit = 8
        maxLimit     = 25
        // perSessionCap keeps one noisy session from eating the result set —
        // matches inside one conversation cluster tightly, so the first few
        // carry the where and the count carries the how much.
        perSessionCap = 4
        // snippetBefore / snippetAfter are the byte window around the first
        // match (UTF-8 aligned at the edges by alignStart/alignEnd).
        snippetBefore = 110
        snippetAfter  = 170
        // shortSessionID is the id prefix shown per session block. The CLI's
        // --session accepts unique prefixes, so the short form is not just
        // readable — it is directly usable to resume.
        shortSessionID = 8
)

const sessionSearchDescription = `Search the raw transcripts of this project's past sessions for a word or
phrase (case-insensitive substring). Use it when distilled memories (recall) are
too summarized and you need what was actually said: past decisions, error
messages, fixes, or the user's own words. Returns matching snippets grouped by
session, newest sessions first. The current session is not searched — its
transcript is already in your context.`

// SessionSearchTool returns the `session_search` tool definition + handler.
// sessionDir is the per-project session directory (project.SessionDir()).
// currentSessionID is excluded from results.
func SessionSearchTool(sessionDir, currentSessionID string) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "session_search",
                        Description: sessionSearchDescription,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "query": llm.Object{
                                                "type":        "string",
                                                "description": "Text to find in past session transcripts. Case-insensitive substring.",
                                        },
                                        "limit": llm.Object{
                                                "type":        "integer",
                                                "description": fmt.Sprintf("Max matching messages to return. Default %d, cap %d.", defaultLimit, maxLimit),
                                        },
                                },
                                Required: []string{"query"},
                        },
                        Readable: true,
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in searchInput
                        _ = json.Unmarshal(input, &in)
                        q := strings.TrimSpace(in.Query)
                        if q == "" {
                                return ""
                        }
                        if len(q) > 40 {
                                q = q[:40] + "..."
                        }
                        return fmt.Sprintf("session_search %q", q)
                },
                Run: func(ctx context.Context, args json.RawMessage) (tooldef.Result, error) {
                        var in searchInput
                        if err := json.Unmarshal(args, &in); err != nil {
                                return tooldef.Result{}, fmt.Errorf("session_search: %w", err)
                        }
                        query := strings.TrimSpace(in.Query)
                        if query == "" {
                                return tooldef.Result{}, fmt.Errorf("session_search: query is required")
                        }
                        limit := in.Limit
                        if limit <= 0 {
                                limit = defaultLimit
                        }
                        if limit > maxLimit {
                                limit = maxLimit
                        }
                        return searchSessions(sessionDir, currentSessionID, query, limit)
                },
        }
}

type searchInput struct {
        Query string `json:"query"`
        Limit int    `json:"limit,omitempty"`
}

// sessionHit is one matching entry, already windowed to a snippet.
type sessionHit struct {
        ts      time.Time
        role    string
        snippet string
}

// searchSessions scans the project's sessions (newest first, per
// ListSessions) and renders the grouped result.
func searchSessions(dir, currentSessionID, query string, limit int) (tooldef.Result, error) {
        metas, err := session.ListSessions(dir)
        if err != nil {
                return tooldef.Result{}, fmt.Errorf("session_search: %w", err)
        }

        scanned := 0
        shown := 0
        matchedSessions := 0
        var blocks []string
        for i, meta := range metas {
                if meta.ID == currentSessionID {
                        continue
                }
                scanned++
                hits, total, err := scanSession(meta, query)
                if err != nil {
                        // One unreadable session must not kill the search — the
                        // listing stays honest about what exists; the search stays
                        // best-effort about what it can read.
                        continue
                }
                if total == 0 {
                        continue
                }
                matchedSessions++
                blocks = append(blocks, renderSessionBlock(meta, hits, total))
                shown += len(hits)
                if shown >= limit {
                        // Sessions older than this point were never opened. Say so —
                        // "no more matches" and "stopped looking" are different
                        // answers, and the model deserves to know which one it got.
                        if rest := len(metas) - i - 1; rest > 0 {
                                blocks = append(blocks, fmt.Sprintf(
                                        "Result limit (%d) reached with %d older session(s) unscanned — raise limit or narrow the query if the answer may be older.",
                                        limit, rest))
                        }
                        break
                }
        }

        detail := fmt.Sprintf("session_search %q", truncQuery(query))
        if scanned == 0 {
                return tooldef.Result{
                        Content: "No past sessions found in this project yet — nothing to search.",
                        Detail:  detail,
                        Summary: "no sessions",
                }, nil
        }
        if matchedSessions == 0 {
                return tooldef.Result{
                        Content: fmt.Sprintf(
                                "No matches for %q across %d past session(s). Try a shorter word, or a different phrasing the user might have used.",
                                query, scanned),
                        Detail:  detail,
                        Summary: "0 matches",
                }, nil
        }

        var b strings.Builder
        fmt.Fprintf(&b, "Searched %d past session(s) for %q — %d matching message(s) in %d session(s), newest first.\n\n",
                scanned, query, shown, matchedSessions)
        b.WriteString(strings.Join(blocks, "\n\n"))

        content := b.String()
        return tooldef.Result{
                Content: content,
                Detail:  detail,
                Summary: fmt.Sprintf("%d match(es) in %d session(s)", shown, matchedSessions),
                Output:  content,
        }, nil
}

// renderSessionBlock formats one session's hits: an identity line (date,
// short id, match count) followed by one line per shown hit.
func renderSessionBlock(meta session.SessionMeta, hits []sessionHit, total int) string {
        date := meta.Timestamp
        if t, err := time.Parse("2006-01-02T15-04-05", meta.Timestamp); err == nil {
                date = t.Format("2006-01-02")
        }
        id := meta.ID
        if len(id) > shortSessionID {
                id = id[:shortSessionID]
        }
        count := fmt.Sprintf("%d match", total)
        if total != 1 {
                count += "es"
        }
        if total > len(hits) {
                count += fmt.Sprintf(" (%d of %d shown)", len(hits), total)
        }

        var b strings.Builder
        fmt.Fprintf(&b, "%s · session %s · %s", date, id, count)
        for _, h := range hits {
                fmt.Fprintf(&b, "\n  %s %-9s %s", h.ts.Format("15:04"), h.role, h.snippet)
        }
        return b.String()
}

// scanSession reads one session's entries and returns up to perSessionCap
// hits plus the total match count. Backing resolution mirrors OpenSession
// (vault-first): when the process vault holds the session id, its rows are
// the truth and any same-named file is a stale pre-migration shadow; only
// vault-less sessions read the file.
func scanSession(meta session.SessionMeta, query string) ([]sessionHit, int, error) {
        var hits []sessionHit
        total := 0
        record := func(h sessionHit) {
                total++
                if len(hits) < perSessionCap {
                        hits = append(hits, h)
                }
        }
        match := func(raw []byte) {
                entry, err := session.DecodeEntryLine(raw, 0)
                if err != nil {
                        return // malformed line: listings stay honest, search stays best-effort
                }
                switch e := entry.(type) {
                case session.SessionMessageEntry:
                        switch e.Message.Role {
                        case llm.RoleUser, llm.RoleAssistant, llm.RoleTool:
                        default:
                                return // system framing and anything exotic is noise
                        }
                        if content := e.Message.Content; content != "" {
                                if i := ciIndex(content, query); i >= 0 {
                                        record(sessionHit{
                                                ts:      e.Timestamp,
                                                role:    string(e.Message.Role),
                                                snippet: snippetAround(content, i, len(query)),
                                        })
                                }
                        }
                case session.CompactionEntry:
                        // Compaction summaries are the only surviving record of
                        // everything before the cut — skipping them would blind the
                        // search to exactly the oldest material.
                        if s := e.Compaction.Summary; s != "" {
                                if i := ciIndex(s, query); i >= 0 {
                                        record(sessionHit{
                                                ts:      e.Timestamp,
                                                role:    "summary",
                                                snippet: snippetAround(s, i, len(query)),
                                        })
                                }
                        }
                }
        }

        if v := session.DefaultVault(); v != nil {
                if has, err := v.HasSession(meta.ID); err == nil && has {
                        lines, err := v.LoadSessionEntries(meta.ID)
                        if err != nil {
                                return nil, 0, err
                        }
                        for _, line := range lines {
                                if strings.TrimSpace(line) == "" {
                                        continue
                                }
                                match([]byte(line))
                        }
                        return hits, total, nil
                }
        }

        f, err := os.Open(meta.File)
        if err != nil {
                return nil, 0, err
        }
        defer f.Close()
        sc := bufio.NewScanner(f)
        // Session lines can carry large tool payloads (same bound as the
        // listing reader).
        sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
        for sc.Scan() {
                line := sc.Bytes()
                if len(strings.TrimSpace(string(line))) == 0 {
                        continue
                }
                match(line)
        }
        if err := sc.Err(); err != nil {
                return nil, 0, err
        }
        return hits, total, nil
}

// ciIndex returns the byte offset of the first case-insensitive
// occurrence of needle in content, or -1. The fast path lowercases both
// sides — exact only when lowering preserves BOTH lengths (Go's simple
// lowercase of "İ" is one-byte "i"), which the guards check. The slow
// path compares fixed-length byte windows with EqualFold, which can miss
// a needle whose lowercase form changes length; an acceptable edge for
// transcript search — ASCII and the common Unicode cases are exact, and
// a miss is quiet, never a false hit.
func ciIndex(content, needle string) int {
        if needle == "" {
                return -1
        }
        lc := strings.ToLower(content)
        ln := strings.ToLower(needle)
        if len(lc) == len(content) && len(ln) == len(needle) {
                return strings.Index(lc, ln)
        }
        n := len(needle)
        for i := 0; i+n <= len(content); i++ {
                if strings.EqualFold(content[i:i+n], needle) {
                        return i
                }
        }
        return -1
}

// snippetAround windows content around the match at byte offset i of
// length n, marking the matched span with » « so the eye lands on it.
// Newlines and whitespace runs collapse to single spaces: one snippet,
// one line.
func snippetAround(content string, i, n int) string {
        start := i - snippetBefore
        if start < 0 {
                start = 0
        }
        end := i + n + snippetAfter
        if end > len(content) {
                end = len(content)
        }
        start = alignStart(content, start)
        end = alignEnd(content, end)

        prefix, suffix := "…", "…"
        if start == 0 {
                prefix = ""
        }
        if end == len(content) {
                suffix = ""
        }
        window := content[start:end]
        mStart := i - start
        mEnd := mStart + n
        marked := window[:mStart] + "»" + window[mStart:mEnd] + "«" + window[mEnd:]
        return prefix + collapseSpaces(marked) + suffix
}

// alignStart backs i up to the nearest rune boundary at or before it.
func alignStart(s string, i int) int {
        for i > 0 && !utf8.RuneStart(s[i]) {
                i--
        }
        return i
}

// alignEnd advances i to the nearest rune boundary at or after it.
func alignEnd(s string, i int) int {
        for i < len(s) && !utf8.RuneStart(s[i]) {
                i++
        }
        return i
}

// collapseSpaces replaces runs of whitespace with a single space.
func collapseSpaces(s string) string {
        var b strings.Builder
        b.Grow(len(s))
        inSpace := false
        for _, r := range s {
                switch r {
                case ' ', '\t', '\n', '\r':
                        inSpace = true
                default:
                        if inSpace {
                                b.WriteByte(' ')
                                inSpace = false
                        }
                        b.WriteRune(r)
                }
        }
        return b.String()
}

func truncQuery(q string) string {
        if len(q) > 40 {
                return q[:40] + "..."
        }
        return q
}
