package session

import (
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
)

// TestToolResultSourcesSurviveReload pins the source-pill reload contract:
// the ranked Sources a search-mode fetch produced ride the persisted tool
// result entry ("sources" on the JSONL line), and a reloaded session's
// ReplaySnapshot restores them onto the ToolRun — the live SSE stream
// already mirrors them, but without the persisted copy every reload
// dropped the pills.
func TestToolResultSourcesSurviveReload(t *testing.T) {
        dir := t.TempDir()
        sm, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true))
        require.NoError(t, err)

        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "search for a1 harness"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{
                Role: llm.RoleAssistant,
                ToolCalls: []llm.ToolCall{{
                        ID:   "call_1",
                        Type: "function",
                        Function: llm.Function{
                                Name:      "fetch",
                                Arguments: `{"query":"a1 harness","mode":"search"}`,
                        },
                }},
        })
        require.NoError(t, err)
        _, err = sm.AppendToolResult(
                llm.Message{Role: llm.RoleTool, ToolCallID: "call_1", Content: "1. a1 — https://example.com/a1"},
                []ToolSource{{
                        Rank:       1,
                        URL:        "https://example.com/a1",
                        Title:      "a1 — the agentic harness",
                        Host:       "example.com",
                        FaviconURL: "https://icons.duckduckgo.com/ip3/example.com.ico",
                        Snippet:    "An agentic CLI harness.",
                }},
        )
        require.NoError(t, err)

        // Reload from disk — the persisted entry must carry the sources through.
        reloaded, err := OpenSession(sm.File())
        require.NoError(t, err)
        snap := ReplaySnapshot(reloaded.BuildContext(), nil)
        run := snap.Tools["call_1"]
        require.NotNil(t, run, "tool run restored by tool_use id")
        require.Len(t, run.Sources, 1, "sources survive the persist/reload round-trip")
        assert.Equal(t, "https://example.com/a1", run.Sources[0].URL)
        assert.Equal(t, "example.com", run.Sources[0].Host)
        assert.Equal(t, ToolDone, run.Status)
}

// TestSetLeafBranchesAppend pins the branch primitive regenerate and
// edit-user-message build on: moving the cursor makes the next Append land
// as a SIBLING, the abandoned tail stays in the store, and the context path
// follows the new branch.
func TestSetLeafBranchesAppend(t *testing.T) {
        dir := t.TempDir()
        sm, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true))
        require.NoError(t, err)

        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "original question"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{Role: llm.RoleAssistant, Content: "stale answer"})
        require.NoError(t, err)
        user2, err := sm.Append(llm.Message{Role: llm.RoleUser, Content: "follow-up"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{Role: llm.RoleAssistant, Content: "follow-up answer"})
        require.NoError(t, err)

        // Rewind to user1's parent (the root) — the regenerate-of-first-turn
        // and edit-first-message shapes.
        require.NoError(t, sm.SetLeaf(""))

        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "edited question"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{Role: llm.RoleAssistant, Content: "fresh answer"})
        require.NoError(t, err)

        // The context path is the NEW branch only.
        entries := sm.BuildContext()
        var texts []string
        for _, e := range entries {
                if me, ok := e.(SessionMessageEntry); ok {
                        texts = append(texts, me.Message.Content)
                }
        }
        assert.Equal(t, []string{"edited question", "fresh answer"}, texts,
                "the context path follows the rewound branch, not the abandoned tail")

        // The abandoned branch is still in the store and readable off-path:
        // walking from the old user2 leaf yields [user2, stale answer, user1].
        branch := sm.GetBranch(user2)
        require.Len(t, branch, 3, "old branch remains readable off-path")
        var branchTexts []string
        for _, e := range branch {
                if me, ok := e.(SessionMessageEntry); ok {
                        branchTexts = append(branchTexts, me.Message.Content)
                }
        }
        assert.Equal(t, []string{"follow-up", "stale answer", "original question"}, branchTexts)

        // Unknown ids refuse rather than corrupting the cursor.
        assert.Error(t, sm.SetLeaf("no-such-entry"), "unknown entry id must be rejected")

        // A reload picks the LAST line as the leaf — the new branch — so the
        // rewind survives restarts.
        reloaded, err := OpenSession(sm.File())
        require.NoError(t, err)
        last := reloaded.BuildContext()
        require.NotEmpty(t, last)
        if me, ok := last[len(last)-1].(SessionMessageEntry); ok {
                assert.Equal(t, "fresh answer", me.Message.Content)
        } else {
                t.Fatalf("expected a message entry at the leaf, got %T", last[len(last)-1])
        }
}

// TestSetLeafToIntermediateEntry pins the mid-conversation edit shape:
// rewinding to the parent of the SECOND user message keeps the first
// exchange on the new branch's path.
func TestSetLeafToIntermediateEntry(t *testing.T) {
        dir := t.TempDir()
        sm, err := NewSessionManager(dir, WithSessionDir(dir), WithShouldFlush(true))
        require.NoError(t, err)

        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "q1"})
        require.NoError(t, err)
        asst1, err := sm.Append(llm.Message{Role: llm.RoleAssistant, Content: "a1"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "q2"})
        require.NoError(t, err)
        _, err = sm.Append(llm.Message{Role: llm.RoleAssistant, Content: "a2"})
        require.NoError(t, err)

        // Rewind to q2's parent (a1): the edit lands as q2's sibling.
        require.NoError(t, sm.SetLeaf(asst1))
        _, err = sm.Append(llm.Message{Role: llm.RoleUser, Content: "q2 edited"})
        require.NoError(t, err)

        var texts []string
        for _, e := range sm.BuildContext() {
                if me, ok := e.(SessionMessageEntry); ok {
                        texts = append(texts, me.Message.Content)
                }
        }
        assert.Equal(t, []string{"q1", "a1", "q2 edited"}, texts,
                "the first exchange stays on the path; the edit forks from q2's parent")
}
