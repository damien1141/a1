package webserver

import (
        "bytes"
        "context"
        "encoding/json"
        "errors"
        "fmt"
        "net/http"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/llm"
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

// htmlThemeTag is the opening tag the theme stamp replaces — the built
// document's own <html lang="en">. The boot snippet in that same document
// yields to a pre-set data-theme before applying its own fallback chain,
// so a stamped choice wins pre-paint. (themeapi.go owns the write side.)
const htmlThemeTag = `<html lang="en">`

// handlePage serves the single-document web UI. When config.yaml carries
// an explicit theme choice (web.dark_mode, written by PUT /api/theme) it
// is stamped onto the <html> tag before the bytes leave the server: the
// browser paints the right theme FIRST — no localStorage round-trip, no
// flash — and the choice reaches every browser, not just the one that
// made it.
func (s *Server) handlePage(w http.ResponseWriter, _ *http.Request) {
        w.Header().Set("Content-Type", "text/html; charset=utf-8")
        w.Header().Set("Cache-Control", "no-store")
        _, _ = w.Write(s.themedPage())
}

// themedPage returns the embedded page with the persisted theme stamped
// on. No choice — or no project (tests build bare Servers) — serves the
// document untouched and the boot snippet's own chain (stored choice,
// then OS preference) applies.
func (s *Server) themedPage() []byte {
        if s.proj == nil || s.proj.Config() == nil {
                return s.page
        }
        dark := s.proj.Config().Web.DarkMode
        if dark == nil {
                return s.page
        }
        t := "light"
        if *dark {
                t = "dark"
        }
        return bytes.Replace(s.page, []byte(htmlThemeTag), []byte(`<html lang="en" data-theme="`+t+`">`), 1)
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

        // currentMsg tracks the assistant round the in-flight events belong
        // to. AssistantMessageUpdate carries the round's id on the message;
        // ToolData does not, so it inherits whatever round is current — tool
        // calls always run between one round's complete event and the next
        // round's first delta, which is exactly the bubble they belong to.
        currentMsg := ""
        var runErr error
        for ev, err := range le.engine.Loop(ctx, prompt, loopOpts()) {
                if err != nil {
                        runErr = err
                        break
                }
                if upd, ok := ev.(session.AssistantMessageUpdate); ok && upd.Message.ID != "" {
                        currentMsg = upd.Message.ID
                }
                s.projectEvent(sessionID, le.engine.ContextWindow(), currentMsg, ev)
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
        // Smart session naming (background, best-effort): the first
        // completed exchange gets a short title from the memory-ops model.
        s.maybeNameSession(sessionID, le)
}

// projectEvent maps one session.Event onto the wire event kinds. window is
// the engine's live context window — it rides the usage event so the status
// bar's denominator tracks model switches and learned windows without a
// page refresh. msgID is the assistant round the event belongs to (tracked
// by the caller); it tags deltas and tool rows so the browser can segment
// a multi-round turn into per-round bubbles.
func (s *Server) projectEvent(sessionID string, window int, msgID string, ev session.Event) {
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
                // MsgID rides every delta so the browser segments rounds:
                // same id = same bubble (accumulated text replaces), new id
                // = new bubble. This is what keeps text said BEFORE a tool
                // call on screen when the next round starts streaming.
                delta.MsgID = msgID
                // The engine re-emits the whole accumulated message on every stream
                // chunk (including tool-call chunks that carry no text at all), so an
                // empty delta is noise the browser would only re-render over.
                if delta.Text != "" || delta.Reasoning != "" {
                        s.events().publish(sessionID, serverEvent{Kind: eventDelta, Delta: &delta})
                }
                // A completed message carries the turn's token usage: publish the
                // ctx readout the status bar renders ("ctx 42%/64k").
                if e.Message.State == session.StateComplete && e.Message.Usage.Reported() {
                        s.events().publish(sessionID, serverEvent{
                                Kind: eventUsage,
                                Usage: &usagePayload{
                                        ContextTokens: e.Message.Usage.ContextTokens(),
                                        Window:        window,
                                },
                        })
                }
        case session.ToolData:
                run := toolEventPayload(e.Run)
                run.MsgID = msgID
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
                if !e.Failed {
                        // A successful compaction lands a persistent entry in the
                        // session tree, so a reloaded transcript renders a divider
                        // for it — the live stream must draw the same cut or the
                        // two views visibly disagree. A failed compaction appends
                        // no entry, so it gets no marker either: only the
                        // transient status line above.
                        s.events().publish(sessionID, serverEvent{
                                Kind:   eventMarker,
                                Marker: &markerPayload{Text: compactionDividerText(e.TokensBefore)},
                        })
                }
        }
}

// handleModelInfo reports the active model and its resolved context window
// so the web status bar can show the ctx denominator before the first turn.
// A live engine's values win — its model NAME follows persona switches and
// its window reflects the engine's resolution order (explicit config/preset,
// probe, assumed fallback, learned limits); the config default is only the
// engine-less fallback. A learned or probed window also reaches the client
// through the usage event once a turn runs.
func (s *Server) handleModelInfo(w http.ResponseWriter, _ *http.Request) {
        if !s.requireProject(w) {
                return
        }
        cfg := s.proj.Config().Model()
        name := cfg.Name
        window := cfg.ContextWindow

        s.mu.Lock()
        for _, le := range s.engines {
                if le == nil || le.engine == nil {
                        continue
                }
                if n := le.engine.ModelName(); n != "" {
                        name = n
                }
                if w := le.engine.ContextWindow(); w > 0 {
                        window = w
                }
                break
        }
        s.mu.Unlock()

        if window <= 0 {
                window = llm.AssumedContextWindow
        }
        writeJSON(w, http.StatusOK, modelInfoResponse{
                Model:         name,
                ContextWindow: window,
        })
}

type modelInfoResponse struct {
        Model         string `json:"model"`
        ContextWindow int    `json:"contextWindow"`
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

// transcript loads a stored session and projects it for rendering. The
// projection walks the FULL leaf-to-root branch (AllEntries), not the
// compaction-cut context path: the web transcript is for the human, who
// should still be able to scroll every message above a compaction marker
// — only the model's context stops at the cut.
func (s *Server) transcript(id string) (session.Snapshot, error) {
        le, err := s.liveFor(id)
        if err != nil {
                return session.Snapshot{}, err
        }
        return session.ReplaySnapshot(le.engine.Session().AllEntries(), le.engine.ToolDetail), nil
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
