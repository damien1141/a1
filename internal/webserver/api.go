package webserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/session"
)

// promptRequest is the body posted by the chat composer. SessionID selects the
// conversation ("" starts a new one); Prompt is the user text.
type promptRequest struct {
	SessionID string `json:"sessionId"`
	Prompt    string `json:"prompt"`
}

// askRequest answers a permission prompt raised mid-turn.
type askRequest struct {
	ID       string `json:"id"`
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}

// cancelRequest names the session whose in-flight turn should stop.
type cancelRequest struct {
	SessionID string `json:"sessionId"`
}

// handlePage serves the single-document web UI.
func (s *Server) handlePage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.page)
}

// handleListSessions renders the sidebar session list as an htmx fragment.
func (s *Server) handleListSessions(w http.ResponseWriter, _ *http.Request) {
	if !s.requireProject(w) {
		return
	}
	sessions, err := s.listSessions()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "list sessions: "+err.Error())
		return
	}
	renderFragment(w, sessionListFragment(sessions, s.pinStore().list()))
}

// handleTranscript renders a stored session's messages as an htmx fragment so
// switching sessions in the sidebar repaints the main pane.
func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "session id is required")
		return
	}
	snap, err := s.transcript(id)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	renderFragment(w, transcriptFragment(snap))
}

// handleSessionMeta returns one session's display title. The chat topbar shows
// a human label rather than the id, and the id is only minted server-side at the
// first prompt — before the sidebar has a row to read one from.
func (s *Server) handleSessionMeta(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "session id is required")
		return
	}
	title, err := s.sessionTitle(id)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "title": title})
}

// decodeBody reads the request body into v, accepting either JSON (the shape
// curl and fetch send) or form encoding (what htmx.ajax sends when called with
// values:). Saving one content-type branch in the client would mean the browser
// only ever sees one of the two, and both are part of this API's contract.
func decodeBody(r *http.Request, v any) error {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if err := r.ParseForm(); err != nil {
			return fmt.Errorf("bad form: %w", err)
		}
		form := formTarget(v)
		if form == nil {
			return errors.New("form decoding is not supported for this type")
		}
		return form(r.PostForm)
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("bad request: %w", err)
	}
	return nil
}

// formTarget returns the bound setter for a supported request type, so a new
// handler opts in to form support explicitly rather than silently ignoring
// posted fields.
func formTarget(v any) func(map[string][]string) error {
	switch t := v.(type) {
	case *promptRequest:
		return func(f map[string][]string) error {
			t.SessionID = firstFormValue(f, "sessionId")
			t.Prompt = firstFormValue(f, "prompt")
			return nil
		}
	case *askRequest:
		return func(f map[string][]string) error {
			t.ID = firstFormValue(f, "id")
			t.Approved = firstFormValue(f, "approved") == "true"
			t.Feedback = firstFormValue(f, "feedback")
			return nil
		}
	case *cancelRequest:
		return func(f map[string][]string) error {
			t.SessionID = firstFormValue(f, "sessionId")
			return nil
		}
	case *modeRequest:
		return func(f map[string][]string) error {
			t.Mode = firstFormValue(f, "mode")
			return nil
		}
	case *pinRequest:
		return func(f map[string][]string) error {
			t.SessionID = firstFormValue(f, "sessionId")
			return nil
		}
	}
	return nil
}

func firstFormValue(f map[string][]string, key string) string {
	if v, ok := f[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

// handlePrompt starts one agent turn. The HTTP call returns as soon as the turn
// is scheduled so the browser is never holding an open connection while the
// model thinks; results stream over the SSE endpoint.
func (s *Server) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	var req promptRequest
	if err := decodeBody(r, &req); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeStatus(w, http.StatusBadRequest, "prompt is empty")
		return
	}

	le, err := s.liveFor(strings.TrimSpace(req.SessionID))
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	sessionID := le.engine.SessionID()

	// Serialize turns: a second prompt waits for the first instead of
	// interleaving two streams into one transcript.
	//
	// The turn context is deliberately detached from r.Context(): this handler
	// returns 202 as soon as the turn is scheduled, so a request-scoped context
	// would be cancelled by the response write itself. It lives until the turn
	// finishes or /api/cancel cancels it.
	le.turn.Lock()
	//nolint:gosec // G118: the turn must outlive this request; /api/cancel stops it
	ctx, cancel := context.WithCancel(context.Background())
	le.cancel = cancel
	go func(prompt string) {
		defer le.turn.Unlock()
		defer cancel()
		s.runTurn(withSession(ctx, sessionID), le, prompt)
	}(req.Prompt)

	writeJSON(w, http.StatusAccepted, map[string]string{"sessionId": sessionID})
}

// runTurn drives one engine.Loop and projects its events onto the SSE hub.
func (s *Server) runTurn(ctx context.Context, le *liveEngine, prompt string) {
	sessionID := sessionFromContext(ctx)
	s.events().publish(sessionID, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "thinking…"}})

	var runErr error
	for ev, err := range le.engine.Loop(ctx, prompt, loopOpts()) {
		if err != nil {
			runErr = err
			break
		}
		s.projectEvent(sessionID, ev)
	}
	if runErr != nil {
		s.events().publish(sessionID, serverEvent{
			Kind:   eventStatus,
			Status: &statusPayload{Text: "error: " + runErr.Error()},
			Failed: true,
		})
		return
	}
	s.events().publish(sessionID, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "done"}})
	s.events().publish(sessionID, serverEvent{Kind: eventDone})
}

// projectEvent maps one session.Event onto the wire event kinds.
func (s *Server) projectEvent(sessionID string, ev session.Event) {
	switch e := ev.(type) {
	case session.AssistantMessageUpdate:
		var delta deltaPayload
		for _, b := range e.Message.Content {
			switch b.Type {
			case session.BlockText:
				delta.Text += b.Text
			case session.BlockThinking:
				delta.Reasoning += b.Text
			}
		}
		// The engine re-emits the whole accumulated message on every stream
		// chunk (including tool-call chunks that carry no text at all), so an
		// empty delta is noise the browser would only re-render over.
		if delta.Text == "" && delta.Reasoning == "" {
			return
		}
		s.events().publish(sessionID, serverEvent{Kind: eventDelta, Delta: &delta})
	case session.ToolData:
		run := toolEventPayload(e.Run)
		s.events().publish(sessionID, serverEvent{Kind: eventTool, Tool: &run})
	case session.CompactionStarted:
		s.events().
			publish(sessionID, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "compacting context…"}})
	case session.CompactionComplete:
		text := "compacted context"
		if e.Failed {
			text = "compaction failed"
		} else if e.TokensBefore > 0 {
			text = fmt.Sprintf("compacted context (%d tokens)", e.TokensBefore)
		}
		s.events().publish(sessionID, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: text}})
	}
}

// handleCancel cancels the in-flight turn for a session.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var req cancelRequest
	if err := decodeBody(r, &req); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	s.mu.Lock()
	le, ok := s.engines[req.SessionID]
	if ok && le.cancel != nil {
		le.cancel()
	}
	s.mu.Unlock()
	if !ok {
		writeStatus(w, http.StatusNotFound, "session is not running")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// handleAsk answers an outstanding permission prompt.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var body askRequest
	if err := decodeBody(r, &body); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.ID == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	s.mu.Lock()
	ch, ok := s.pending[body.ID]
	s.mu.Unlock()
	if !ok {
		writeStatus(w, http.StatusNotFound, "no pending permission prompt with that id")
		return
	}
	select {
	case ch <- permissionAskResult(body.Approved, body.Feedback):
	default:
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleEvents streams SSE for the browser. One stream serves every live
// session; each event carries its session id so a page can follow several
// conversations and stay subscribed across sidebar switches.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatus(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, release := s.events().subscribe()
	defer release()

	flusher.Flush()
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSE encodes one event as a named SSE message with a JSON data payload.
func writeSSE(w http.ResponseWriter, ev serverEvent) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
	return err
}

// transcript loads a stored session and projects it for rendering.
func (s *Server) transcript(id string) (session.Snapshot, error) {
	le, err := s.liveFor(id)
	if err != nil {
		return session.Snapshot{}, err
	}
	return session.ReplaySnapshot(le.engine.Session().PathEntries(), le.engine.ToolDetail), nil
}

// ---- request / response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeStatus(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
