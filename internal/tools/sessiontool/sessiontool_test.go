package sessiontool

import (
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools/tooldef"
        "github.com/damien1141/a1/internal/vault"
)

// writeSession persists one real session file under dir and returns its
// manager (id via m.ID()). A session only lands on disk after its first
// assistant message (lazy flush), so every session gets one.
func writeSession(t *testing.T, dir, cwd string, msgs []llm.Message) *session.Manager {
        t.Helper()
        m, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
        require.NoError(t, err)
        for _, msg := range msgs {
                _, err := m.Append(msg)
                require.NoError(t, err)
        }
        require.FileExists(t, m.File(), "session must flush once an assistant message lands")
        return m
}

func runTool(t *testing.T, tool tooldef.Tool, args any) (tooldef.Result, error) {
        t.Helper()
        raw, err := json.Marshal(args)
        require.NoError(t, err)
        return tool.Run(t.Context(), raw)
}

func TestSessionSearchToolDefinition(t *testing.T) {
        tool := SessionSearchTool("/some/dir", "")
        assert.Equal(t, "session_search", tool.Definition.Name)
        assert.True(t, tool.Definition.Readable, "read-only: parallel batchable")
        assert.NotNil(t, tool.Run)
        require.NotNil(t, tool.Definition.Params)
        assert.Equal(t, []string{"query"}, tool.Definition.Params.Required)
        assert.Contains(t, tool.Definition.Description, "transcripts", "description must say what surface is searched")
}

func TestSessionSearchFindsAcrossFileSessions(t *testing.T) {
        dir := t.TempDir()
        older := writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: "the goroutine leak is back"},
                {Role: llm.RoleAssistant, Content: "fixed the timer"},
        })
        newer := writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: "also the pool had a Goroutine LEAK again"},
                {Role: llm.RoleAssistant, Content: "stopped the workers on cancel"},
        })
        _ = newer
        // mtime is the listing order; pin it so "newest first" is asserted,
        // not inherited from test speed.
        back := time.Now().Add(-2 * time.Hour)
        require.NoError(t, os.Chtimes(older.File(), back, back))

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "goroutine leak"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "Searched 2 past session(s) for \"goroutine leak\"")
        assert.Contains(t, res.Content, "2 matching message(s) in 2 session(s)")
        assert.Contains(t, res.Content, "»goroutine leak«", "case of the transcript, not of the query")
        assert.Contains(t, res.Content, "»Goroutine LEAK«", "case-insensitive match keeps original bytes")
        // Newest session first.
        assert.Less(t,
                strings.Index(res.Content, "»Goroutine LEAK«"),
                strings.Index(res.Content, "the »goroutine leak« is back"),
                "newest session's block must come first")
        assert.Contains(t, res.Summary, "2 match")
        assert.Equal(t, `session_search "goroutine leak"`, res.Detail)
}

func TestSessionSearchExcludesCurrentSession(t *testing.T) {
        dir := t.TempDir()
        current := writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: "goroutine leak talk happening right now"},
                {Role: llm.RoleAssistant, Content: "noted"},
        })
        past := writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: "we discussed the goroutine leak here"},
                {Role: llm.RoleAssistant, Content: "ok"},
        })
        _ = past

        res, err := runTool(t, SessionSearchTool(dir, current.ID()), map[string]any{"query": "goroutine leak"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "Searched 1 past session(s)", "the live session is not scanned, not just not shown")
        assert.NotContains(t, res.Content, "right now")
        assert.Contains(t, res.Content, "discussed the »goroutine leak«")
}

func TestSessionSearchPerSessionCapAndCounts(t *testing.T) {
        dir := t.TempDir()
        msgs := []llm.Message{{Role: llm.RoleUser, Content: "the parser"}}
        for i := range 6 {
                msgs = append(msgs,
                        llm.Message{Role: llm.RoleAssistant, Content: fmt.Sprintf("parser attempt %d done", i)},
                        llm.Message{Role: llm.RoleUser, Content: "and?"},
                )
        }
        m := writeSession(t, dir, "/w", msgs)
        _ = m

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "parser"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "7 matches (4 of 7 shown)", "per-session cap with honest counts")
        assert.Equal(t, 4, strings.Count(res.Content, "\n  "), "only the capped rows are rendered")
}

func TestSessionSearchLimitStopsWithUnscannedNote(t *testing.T) {
        dir := t.TempDir()
        for i := range 3 {
                writeSession(t, dir, "/w", []llm.Message{
                        {Role: llm.RoleUser, Content: fmt.Sprintf("session %d mentions the token cap", i)},
                        {Role: llm.RoleAssistant, Content: "ok"},
                })
        }

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "token cap", "limit": 2})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "Result limit (2) reached with 1 older session(s) unscanned",
                "stopped-looking is not no-more-matches; the note says which one happened")
}

func TestSessionSearchNoMatches(t *testing.T) {
        dir := t.TempDir()
        writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: "unrelated"},
                {Role: llm.RoleAssistant, Content: "fine"},
        })

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "zebra"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, `No matches for "zebra" across 1 past session(s)`)
        assert.Equal(t, "0 matches", res.Summary)
}

func TestSessionSearchNoSessions(t *testing.T) {
        res, err := runTool(t, SessionSearchTool(filepath.Join(t.TempDir(), "empty"), ""), map[string]any{"query": "anything"})
        require.NoError(t, err)
        assert.Equal(t, "No past sessions found in this project yet — nothing to search.", res.Content)
}

func TestSessionSearchRequiresQuery(t *testing.T) {
        _, err := runTool(t, SessionSearchTool(t.TempDir(), ""), map[string]any{"query": "  "})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "query is required")
}

func TestSessionSearchSnippetWindow(t *testing.T) {
        dir := t.TempDir()
        content := strings.Repeat("a", 300) + "NeedleHere" + strings.Repeat("b", 300)
        writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleUser, Content: content},
                {Role: llm.RoleAssistant, Content: "done"},
        })

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "needlehere"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "…aaa", "window opens with an ellipsis before the leading context")
        assert.Contains(t, res.Content, "bbb…", "window closes with an ellipsis after the trailing context")
        assert.Contains(t, res.Content, "»NeedleHere«")
        assert.NotContains(t, res.Content, strings.Repeat("a", 200), "the window is a snippet, not the message")
}

func TestSessionSearchSkipsToolCallOnlyMessages(t *testing.T) {
        dir := t.TempDir()
        writeSession(t, dir, "/w", []llm.Message{
                {Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{
                        ID: "1", Type: "function", Function: llm.Function{Name: "bash", Arguments: `{"cmd":"echo magnet"}`},
                }}},
                {Role: llm.RoleTool, ToolCallID: "1", Content: "magnet output"},
                {Role: llm.RoleAssistant, Content: "done"},
        })

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "magnet"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "»magnet«", "tool RESULT text is searchable")
        assert.Equal(t, 1, strings.Count(res.Content, "»magnet«"), "tool-call arguments are not transcript text")
}

func TestSessionSearchMatchesCompactionSummary(t *testing.T) {
        dir := t.TempDir()
        m, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
        require.NoError(t, err)
        _, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "early talk"})
        require.NoError(t, err)
        _, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "early answer"})
        require.NoError(t, err)
        _, err = m.AppendCompaction(session.Compaction{Summary: "earlier: the user set up the ingestion pipeline and hit the nozzle clog twice"})
        require.NoError(t, err)

        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "nozzle clog"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "summary", "compaction hits ride the summary role")
        assert.Contains(t, res.Content, "»nozzle clog«")
}

func TestSessionSearchVaultBacking(t *testing.T) {
        dir := t.TempDir()
        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        t.Cleanup(func() { _ = v.Close() })
        session.SetDefaultVault(v)
        t.Cleanup(func() { session.SetDefaultVault(nil) })

        sid := strings.Repeat("ab", 16) // 32-hex-shaped id
        header, err := json.Marshal(session.SessionHeader{
                Type: session.EntrySession, ID: sid, Timestamp: "2026-11-01T10-00-00", Cwd: "/w",
        })
        require.NoError(t, err)
        userLine, err := json.Marshal(session.SessionMessageEntry{
                SessionBaseEntry: session.SessionBaseEntry{
                        Type: session.EntryMessage, ID: "e1", Timestamp: time.Date(2026, 11, 1, 10, 1, 0, 0, time.UTC),
                },
                Message: llm.Message{Role: llm.RoleUser, Content: "the vault remembers the flange"},
        })
        require.NoError(t, err)
        assistantLine, err := json.Marshal(session.SessionMessageEntry{
                SessionBaseEntry: session.SessionBaseEntry{
                        Type: session.EntryMessage, ID: "e2", Timestamp: time.Date(2026, 11, 1, 10, 2, 0, 0, time.UTC),
                },
                Message: llm.Message{Role: llm.RoleAssistant, Content: "noted"},
        })
        require.NoError(t, err)

        require.NoError(t, v.PutSession(vault.SessionRow{
                ID: sid, Project: dir, Ts: "2026-11-01T10-00-00", Cwd: "/w", CreatedAt: time.Now(),
        }))
        require.NoError(t, v.AppendSessionEntries([]vault.SessionEntryRow{
                {SessionID: sid, Seq: 1, Kind: "header", Payload: string(header)},
                {SessionID: sid, Seq: 2, Kind: "message", Payload: string(userLine)},
                {SessionID: sid, Seq: 3, Kind: "message", Payload: string(assistantLine)},
        }))

        // No file on disk anywhere under dir — the rows are the only copy.
        res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": "flange"})
        require.NoError(t, err)

        assert.Contains(t, res.Content, "Searched 1 past session(s)")
        assert.Contains(t, res.Content, "»flange«")
        assert.Contains(t, res.Content, sid[:shortSessionID], "short id shown is a usable resume prefix")
}

func TestCIIndex(t *testing.T) {
        assert.Equal(t, 4, ciIndex("has Needle inside", "needle"))
        assert.Equal(t, -1, ciIndex("nothing here", "needle"))
        assert.Equal(t, -1, ciIndex("anything", ""))
        assert.Equal(t, 0, ciIndex("needle first", "NEEDLE"))
        // Length-changing lowercase (İ folds to two runes) must not crash and
        // must not produce a false hit.
        assert.Equal(t, -1, ciIndex("ascii only", "İ"))
}
