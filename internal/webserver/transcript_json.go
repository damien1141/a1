package webserver

import (
        "net/http"
        "strings"

        "github.com/damien1141/a1/internal/session"
)

// transcriptMessageDTO is one replayed message in the JSON transcript. Text
// is the raw markdown source (user or assistant) — the client renders it
// through the same markdown pipeline as live bubbles. Tools are the runs the
// message issued, in issue order. ID is the session-tree entry id (absent
// for synthetic rows like the trailing-tool-run fallback).
type transcriptMessageDTO struct {
        Role      string        `json:"role"` // "user" | "assistant" | "system"
        ID        string        `json:"id,omitempty"`
        Text      string        `json:"text,omitempty"`
        Reasoning string        `json:"reasoning,omitempty"`
        Usage     *usageDTO     `json:"usage,omitempty"`
        Tools     []toolPayload `json:"tools,omitempty"`
}

// usageDTO is the token readout under a replayed assistant bubble.
type usageDTO struct {
        In    int     `json:"in"`
        Out   int     `json:"out"`
        TPS   float64 `json:"tps,omitempty"`
        Total int     `json:"total,omitempty"`
}

// transcriptResponse is the GET /api/transcript/messages envelope.
type transcriptResponse struct {
        SessionID string                `json:"sessionId"`
        Messages  []transcriptMessageDTO `json:"messages"`
}

// handleTranscriptMessages returns a stored session's messages as structured
// JSON. The HTML /api/transcript fragment exists for the htmx-era scrape, but
// the client's textContent extraction flattened markdown paragraphs into one
// unbroken wall and dropped reasoning, usage, and tool runs entirely. This
// endpoint hands over the raw parts so the GUI can re-render a past session
// exactly like a live one.
func (s *Server) handleTranscriptMessages(w http.ResponseWriter, r *http.Request) {
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
        writeJSON(w, http.StatusOK, transcriptFromSnapshot(id, snap))
}

// transcriptFromSnapshot projects a replay snapshot into the JSON wire shape.
// Tool runs attach to the assistant message whose tool_use block carries
// their ID; runs with no surviving block (e.g. queued calls cut by a
// compaction) ride with the last assistant message so the trace stays visible.
func transcriptFromSnapshot(sessionID string, snap session.Snapshot) transcriptResponse {
        out := make([]transcriptMessageDTO, 0, len(snap.Messages))
        lastAssistant := -1
        for i := range snap.Messages {
                m := snap.Messages[i]
                switch m.Role {
                case session.RoleUser:
                        out = append(out, transcriptMessageDTO{Role: "user", ID: m.ID, Text: m.Text})
                case session.RoleAssistant:
                        dto := transcriptMessageDTO{Role: "assistant", ID: m.ID}
                        var text strings.Builder
                        for _, b := range m.Content {
                                switch b.Type {
                                case session.BlockThinking:
                                        dto.Reasoning += b.Text
                                case session.BlockText:
                                        text.WriteString(b.Text)
                                case session.BlockToolUse:
                                        if run, ok := snap.Tools[b.ID]; ok {
                                                dto.Tools = append(dto.Tools, toolEventPayload(run))
                                                delete(snap.Tools, b.ID)
                                        }
                                }
                        }
                        dto.Text = text.String()
                        if m.Usage.Reported() {
                                dto.Usage = &usageDTO{
                                        In:    m.Usage.PromptTokens,
                                        Out:   m.Usage.CompletionTokens,
                                        TPS:   m.Usage.TPS,
                                        Total: m.Usage.TotalTokens,
                                }
                        }
                        out = append(out, dto)
                        lastAssistant = len(out) - 1
                case session.RoleCompaction:
                        out = append(out, transcriptMessageDTO{Role: "system", Text: compactionDividerText(m.TokensBefore)})
                }
        }
        // Leftover runs: attach to the nearest preceding assistant bubble (or a
        // trailing synthetic one) so nothing the agent did disappears on replay.
        if len(snap.Tools) > 0 {
                for _, run := range snap.Tools {
                        p := toolEventPayload(run)
                        if lastAssistant >= 0 && lastAssistant < len(out) {
                                out[lastAssistant].Tools = append(out[lastAssistant].Tools, p)
                                continue
                        }
                        out = append(out, transcriptMessageDTO{Role: "assistant", Tools: []toolPayload{p}})
                }
        }
        if out == nil {
                out = []transcriptMessageDTO{}
        }
        return transcriptResponse{SessionID: sessionID, Messages: out}
}
