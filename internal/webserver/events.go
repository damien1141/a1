package webserver

import (
        "sync"

        "github.com/damien1141/a1/internal/session"
)

// serverEvent is one item pushed to a browser over the SSE channel. Kind picks
// which payload field is populated, so the wire shape stays flat and the
// frontend switch has no ambiguity between omitted vs zero values.
type serverEvent struct {
        Kind eventKind `json:"kind"`

        // SessionID tags the conversation the event belongs to. The browser keeps a
        // bubble per session so a page showing more than one conversation (or a
        // session created mid-stream) still routes text to the right place.
        SessionID string `json:"sessionId"`

        // Delta is assistant text or reasoning streamed for the in-flight turn.
        Delta *deltaPayload `json:"delta,omitempty"`

        // Tool is a tool run update (queued → in-progress → done/error).
        Tool *toolPayload `json:"tool,omitempty"`

        // Ask is an outstanding permission prompt. Answered through POST /api/ask.
        Ask *askPayload `json:"ask,omitempty"`

        // Status is a one-line lifecycle note ("thinking…", "compacting…", "done").
        Status *statusPayload `json:"status,omitempty"`

        // Marker is a persistent transcript row — the divider a compaction
        // cut leaves behind. Unlike Status (a transient line the status pill
        // overwrites), a Marker lands in the running transcript, because the
        // reload path renders the stored compaction entry as a divider and the
        // live view must draw the same cut or the two visibly disagree.
        Marker *markerPayload `json:"marker,omitempty"`

        // Usage carries the turn's final token usage plus the engine's live
        // context window, so the browser can render "ctx N%/W" — including a
        // window the engine learned from a provider overflow error or probed
        // from a local llama.cpp / ollama server.
        Usage *usagePayload `json:"usage,omitempty"`

        // Toast is a transient notification overlay (success/warning/error/info).
        Toast *toastPayload `json:"toast,omitempty"`

        // State reports the live UI knobs so the footer chrome stays in sync.
        State *statePayload `json:"state,omitempty"`

        // Failed marks the turn as ended with an error.
        Failed bool `json:"failed,omitempty"`
}

type eventKind string

const (
        eventSession eventKind = "session"
        eventDelta   eventKind = "delta"
        eventTool    eventKind = "tool"
        eventAsk     eventKind = "ask"
        eventStatus  eventKind = "status"
        eventUsage   eventKind = "usage"
        eventDone    eventKind = "done"
        eventToast   eventKind = "toast"
        eventState   eventKind = "state"
        eventMarker  eventKind = "marker"
)

// deltaPayload carries streamed assistant text. Text and Reasoning are the two
// visible streams; the client keeps each turn's authoritative copy and
// re-renders, because the engine emits accumulated (not incremental) text.
//
// MsgID identifies WHICH assistant round the text belongs to. The engine
// mints a fresh id per stream round (one per tool-call cycle), so a turn
// that says something, calls a tool, then says something else produces two
// distinct ids. Without it the browser had no way to tell round 2's text
// from round 1's and overwrote the earlier text — the "webui eats what the
// model says" bug. Empty id (hand-built events) routes to the legacy
// last-bubble behavior.
type deltaPayload struct {
        Text      string `json:"text"`
        Reasoning string `json:"reasoning"`
        MsgID     string `json:"msgId,omitempty"`
}

// toolPayload mirrors the fields of session.ToolRun the web UI renders. It
// exists so the wire names are pinned by this package rather than by whatever
// Go happens to export on the shared TUI type. Status is pre-stringified for
// the same reason.
type toolPayload struct {
        ToolUseID string `json:"toolUseId"`
        Name      string `json:"name"`
        Status    string `json:"status"`
        Detail    string `json:"detail,omitempty"`
        Output    string `json:"output,omitempty"`
        Error     string `json:"error,omitempty"`
        // MsgID is the assistant round the tool call belongs to (same id the
        // round's delta events carry). The browser uses it to attach the tool
        // row to the right bubble when a turn spans multiple rounds.
        MsgID string `json:"msgId,omitempty"`
        // Sources carries search-mode fetch results for the source-pill row
        // the web frontend renders below the tool row. Nil for non-search
        // calls. Mirrored from session.ToolRun.Sources so the wire shape
        // matches the replay transcript's exactly.
        Sources []session.ToolSource `json:"sources,omitempty"`
}

func toolEventPayload(run session.ToolRun) toolPayload {
        p := toolPayload{
                ToolUseID: run.ToolUseID,
                Name:      run.Name,
                Status:    run.Status.String(),
                Detail:    run.Detail,
                Output:    run.Output,
                Error:     run.Error,
                Sources:  run.Sources,
        }
        if len(p.Sources) == 0 {
                p.Sources = nil // omit, don't emit an empty array
        }
        return p
}

// askPayload is a permission prompt rendered in the browser.
type askPayload struct {
        ID     string `json:"id"`
        Tool   string `json:"tool"`
        Reason string `json:"reason"`
}

// statusPayload is a transient lifecycle line for the status pill.
type statusPayload struct {
        Text string `json:"text"`
}

// markerPayload is a persistent transcript row. Text is byte-identical to
// the divider the reload path renders for the same stored entry
// (compactionDividerText), so a session watched live and the same session
// reloaded show the same cut at the same place.
type markerPayload struct {
        Text string `json:"text"`
}

// usagePayload is the turn-end token readout for the status bar.
type usagePayload struct {
        ContextTokens int `json:"contextTokens"`
        Window        int `json:"window"`
}

// toastLevel mirrors the browser Toast stack's levels: success, warning,
// error, info. The default (empty) renders as info.
type toastLevel string

const (
        toastSuccess toastLevel = "success"
        toastWarning toastLevel = "warning"
        toastError   toastLevel = "error"
        toastInfo    toastLevel = "info"
)

// toastPayload is a transient notification the browser renders as an
// overlay. SessionID may be empty: a toast with no session is broadcast to
// every tab (e.g. "Model switched: claude-opus"). A toast with a session id
// is rendered only by the tab following that conversation.
type toastPayload struct {
        Level    toastLevel `json:"level"`
        Message  string     `json:"message"`
        Duration int        `json:"duration,omitempty"` // milliseconds; 0 → client default
}

// statePayload reports the live UI knobs (model, mode, branch, context
// window, agents, memory, think, persona) so the browser footer chrome stays
// in sync without polling. Server pushes a state event whenever one of those
// values changes; the browser also fetches the same shape via GET /api/state
// on initial load.
type statePayload struct {
        Model         string      `json:"model"`
        Think         string      `json:"think"`
        Mode          string      `json:"mode"`
        Branch        string      `json:"branch"`
        ContextWindow int         `json:"contextWindow"`
        ToolBytes     int         `json:"toolBytes"`
        ToolCount     int         `json:"toolCount"`
        // ContextTokens is the estimated total context usage (system
        // prompt + project context + messages + tool outputs + tool
        // schemas) in tokens. The footer uses this instead of
        // toolBytes/4 because toolBytes only counts the tool schemas,
        // not the messages or system prompt — the old counter showed
        // ~5.8k when the real usage was ~9.8k.
        ContextTokens int         `json:"contextTokens"`
        AgentsEnabled bool        `json:"agentsEnabled"`
        MemoryEnabled bool        `json:"memoryEnabled"`
        Persona       personaDTO  `json:"persona"`
}

// hub fans live events out to every connected browser. One stream serves the
// whole process: events are tagged with their session id and the client filters,
// which keeps a tab's subscription alive across session switches.
//
// There is deliberately no replay buffer. A client that attaches mid-turn (or
// reconnects after a drop) re-reads the transcript from the session store, which
// is the authoritative record, then follows the live stream from there.
type hub struct {
        mu  sync.Mutex
        sub map[chan serverEvent]struct{}
}

func newHub() *hub {
        return &hub{sub: map[chan serverEvent]struct{}{}}
}

// subscribe registers a channel for live events. The returned func must be
// called to release it.
//
// On detach we close(ch) in addition to delete(h.sub, ch): without the close,
// the SSE handler's `case ev, ok := <-ch:` stays open and the handler keeps
// writing keepalives to a tab that will never see another real event. Closing
// forces the receive to return !ok, the handler exits, and EventSource on the
// client reconnects cleanly instead of looking alive-but-dead.
func (h *hub) subscribe() (chan serverEvent, func()) {
        ch := make(chan serverEvent, 64)
        h.mu.Lock()
        if h.sub == nil {
                h.sub = map[chan serverEvent]struct{}{}
        }
        h.sub[ch] = struct{}{}
        h.mu.Unlock()
        return ch, func() {
                h.mu.Lock()
                delete(h.sub, ch)
                close(ch)
                h.mu.Unlock()
        }
}

// publish delivers an event to every subscriber. Delivery is non-blocking: a
// stalled browser tab must not throttle the agent loop, so a subscriber that
// cannot keep up is detached instead of back-pressuring the turn.
func (h *hub) publish(sessionID string, ev serverEvent) {
        if ev.SessionID == "" {
                ev.SessionID = sessionID
        }
        h.mu.Lock()
        defer h.mu.Unlock()
        for ch := range h.sub {
                select {
                case ch <- ev:
                default:
                        delete(h.sub, ch)
                }
        }
}
