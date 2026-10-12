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
	eventDone    eventKind = "done"
)

// deltaPayload carries streamed assistant text. Text and Reasoning are the two
// visible streams; the client keeps each turn's authoritative copy and
// re-renders, because the engine emits accumulated (not incremental) text.
type deltaPayload struct {
	Text      string `json:"text"`
	Reasoning string `json:"reasoning"`
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
}

func toolEventPayload(run session.ToolRun) toolPayload {
	return toolPayload{
		ToolUseID: run.ToolUseID,
		Name:      run.Name,
		Status:    run.Status.String(),
		Detail:    run.Detail,
		Output:    run.Output,
		Error:     run.Error,
	}
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
