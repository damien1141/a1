package webserver

import (
        "encoding/json"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/session"
)

// The "webui eats what the model says" bug: text streamed before a tool call
// was overwritten by the post-tool round because the browser had no way to
// tell one round's deltas from the next. The fix tags every delta and tool
// event with the assistant round's msgId so the client can segment bubbles.
// These tests pin the server side of that contract.

// deltaEvent is the JSON shape of an SSE delta event as the browser parses it.
type deltaEvent struct {
        Kind    string `json:"kind"`
        Delta   struct {
                Text      string `json:"text"`
                Reasoning string `json:"reasoning"`
                MsgID     string `json:"msgId"`
        } `json:"delta"`
}

// toolEvent is the JSON shape of an SSE tool event as the browser parses it.
type toolEvent struct {
        Kind string `json:"kind"`
        Tool struct {
                ToolUseID string                 `json:"toolUseId"`
                Name      string                 `json:"name"`
                Status    string                 `json:"status"`
                MsgID     string                 `json:"msgId"`
                Sources   []session.ToolSource    `json:"sources"`
        } `json:"tool"`
}

// TestDeltaCarriesRoundMsgID pins: AssistantMessageUpdate events project to
// delta events whose msgId is the message's ID (accumulated text still
// replaces within the same round).
func TestDeltaCarriesRoundMsgID(t *testing.T) {
        s := &Server{}
        ch, release := s.events().subscribe()
        defer release()

        s.projectEvent("s1", 65536, "assistant-round-1", session.AssistantMessageUpdate{
                Message: session.Message{
                        ID:    "assistant-round-1",
                        State: session.StateStreaming,
                        Content: []session.ContentBlock{
                                {Type: session.BlockText, Text: "I'll check the file."},
                        },
                },
        })

        select {
        case ev := <-ch:
                var got deltaEvent
                raw, err := json.Marshal(ev)
                require.NoError(t, err)
                require.NoError(t, json.Unmarshal(raw, &got), "delta event JSON: %s", raw)
                assert.Equal(t, "assistant-round-1", got.Delta.MsgID, "delta must carry the round's msgId")
                assert.Equal(t, "I'll check the file.", got.Delta.Text)
        case <-time.After(2 * time.Second):
                t.Fatal("no delta event published")
        }
}

// TestToolEventCarriesRoundMsgIDAndSources pins two things:
//   - a ToolData event projects to a tool event tagged with the round id the
//     caller passes (the browser attaches the row to that round's bubble);
//   - search-mode fetch Sources ride along so the live source pills render
//     (previously only replayed transcripts carried them).
func TestToolEventCarriesRoundMsgIDAndSources(t *testing.T) {
        s := &Server{}
        ch, release := s.events().subscribe()
        defer release()

        s.projectEvent("s1", 65536, "assistant-round-1", session.ToolData{Run: session.ToolRun{
                ToolUseID: "tu-1",
                Name:      "fetch",
                Status:    session.ToolDone,
                Detail:    "query: go generics",
                Sources: []session.ToolSource{{
                        Rank:       1,
                        URL:        "https://go.dev/doc/tutorial/generics",
                        Title:      "Tutorial: Getting started with generics",
                        Host:       "go.dev",
                        FaviconURL: "https://icons.duckduckgo.com/ip3/go.dev.ico",
                        Snippet:    "This tutorial introduces the basics of generics in Go.",
                }},
        }})

        select {
        case ev := <-ch:
                var got toolEvent
                raw, err := json.Marshal(ev)
                require.NoError(t, err)
                require.NoError(t, json.Unmarshal(raw, &got), "tool event JSON: %s", raw)
                assert.Equal(t, "assistant-round-1", got.Tool.MsgID, "tool event must carry the round's msgId")
                require.Len(t, got.Tool.Sources, 1, "sources must ride the live tool event")
                assert.Equal(t, "go.dev", got.Tool.Sources[0].Host)
                assert.Equal(t, "https://icons.duckduckgo.com/ip3/go.dev.ico", got.Tool.Sources[0].FaviconURL)
        case <-time.After(2 * time.Second):
                t.Fatal("no tool event published")
        }
}

// TestToolEventOmitsEmptySources pins: a non-search tool event must not carry
// an empty sources array — the browser's pill row keys off the field's
// presence, and `sources: []` would render an empty row.
func TestToolEventOmitsEmptySources(t *testing.T) {
        s := &Server{}
        ch, release := s.events().subscribe()
        defer release()

        s.projectEvent("s1", 65536, "assistant-round-2", session.ToolData{Run: session.ToolRun{
                ToolUseID: "tu-2",
                Name:      "bash",
                Status:    session.ToolInProgress,
                Detail:    "ls -la",
        }})

        select {
        case ev := <-ch:
                raw, err := json.Marshal(ev)
                require.NoError(t, err)
                var got map[string]any
                require.NoError(t, json.Unmarshal(raw, &got))
                tool, ok := got["tool"].(map[string]any)
                require.True(t, ok, "tool payload present")
                _, hasSources := tool["sources"]
                assert.False(t, hasSources, "empty sources must be omitted, not an empty array")
        case <-time.After(2 * time.Second):
                t.Fatal("no tool event published")
        }
}

// TestMultiRoundTurnSegmentsMsgIDs pins the whole multi-round turn contract:
// round 1 deltas + round 1 tools + round 2 deltas each carry the ids the
// browser needs to keep round 1's text on screen. This is the exact sequence
// the engine emits for "say something, call a tool, say something else".
func TestMultiRoundTurnSegmentsMsgIDs(t *testing.T) {
        s := &Server{}
        ch, release := s.events().subscribe()
        defer release()

        // Round 1: streaming text before the tool call.
        s.projectEvent("s1", 65536, "r1", session.AssistantMessageUpdate{Message: session.Message{
                ID: "r1", State: session.StateStreaming,
                Content: []session.ContentBlock{{Type: session.BlockText, Text: "Let me check that."}},
        }})
        // Round 1: tool runs (between the complete event and round 2's first
        // delta, so they inherit r1 — the caller tracks it).
        s.projectEvent("s1", 65536, "r1", session.ToolData{Run: session.ToolRun{
                ToolUseID: "tu-1", Name: "read", Status: session.ToolDone,
        }})
        // Round 2: post-tool text — a DIFFERENT round id.
        s.projectEvent("s1", 65536, "r2", session.AssistantMessageUpdate{Message: session.Message{
                ID: "r2", State: session.StateStreaming,
                Content: []session.ContentBlock{{Type: session.BlockText, Text: "The file looks good."}},
        }})

        var ids []string
        for range 3 {
                select {
                case ev := <-ch:
                        raw, err := json.Marshal(ev)
                        require.NoError(t, err)
                        var kindOnly struct {
                                Kind  string `json:"kind"`
                                Delta *struct {
                                        MsgID string `json:"msgId"`
                                } `json:"delta"`
                                Tool *struct {
                                        MsgID string `json:"msgId"`
                                } `json:"tool"`
                        }
                        require.NoError(t, json.Unmarshal(raw, &kindOnly))
                        if kindOnly.Delta != nil {
                                ids = append(ids, kindOnly.Delta.MsgID)
                        } else if kindOnly.Tool != nil {
                                ids = append(ids, kindOnly.Tool.MsgID)
                        }
                case <-time.After(2 * time.Second):
                        t.Fatal("expected 3 events, got", len(ids))
                }
        }

        assert.Equal(t, []string{"r1", "r1", "r2"}, ids,
                "round-1 text, round-1 tool, round-2 text must each be tagged with their round's id")
}

// TestEmptyMsgIDFallsBackLegacy pins: events without a round id (ForceCompact
// passes "", hand-built test events) still project — the field is omitted
// rather than emitted as an empty string the client would treat as a real id.
func TestEmptyMsgIDFallsBackLegacy(t *testing.T) {
        s := &Server{}
        ch, release := s.events().subscribe()
        defer release()

        s.projectEvent("s1", 65536, "", session.AssistantMessageUpdate{Message: session.Message{
                State: session.StateStreaming,
                Content: []session.ContentBlock{{Type: session.BlockText, Text: "compaction summary"}},
        }})

        select {
        case ev := <-ch:
                raw, err := json.Marshal(ev)
                require.NoError(t, err)
                var got deltaEvent
                require.NoError(t, json.Unmarshal(raw, &got))
                assert.Empty(t, got.Delta.MsgID, "empty round id must be omitted on the wire")
        case <-time.After(2 * time.Second):
                t.Fatal("no delta event published")
        }
}
