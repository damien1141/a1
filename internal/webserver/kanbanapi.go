package webserver

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/kanban"
	"github.com/damien1141/a1/internal/session"
)

// kanbanapi.go serves the dashboard's board widget (chunk 3): GET/POST
// /api/personal/kanban (the board / add a card), PUT /api/personal/kanban/
// card (move + edit, one body), DELETE /api/personal/kanban/card?id=.
//
// The ruling that separates this from the calendar (chunk 2): the web
// WRITES here. Calendar writes are commitments the model makes on the
// user's behalf, so the write face lives in the agent tool behind the
// model's personal_write opt-in; the kanban board is the opposite —
// dragging a card IS the product, and the human clicking their own
// dashboard is not an agent that needs a per-model opt-in. The gate is the
// master switch alone (personalGate, like every /api/personal/* route),
// and every mutating verb composes guard() on top: a cross-origin page
// must not be able to rearrange the board.
//
// The board is vault-backed (a1.db, the kanban_cards table internal/kanban
// owns). The handle is resolved live per request — the same
// session.DefaultVault() path the session handlers use — and a server with
// no vault answers 503, never a fabricated empty board: an empty board is
// a valid state and must not be conflated with "no store".
//
// Input validation lives in the handlers (400s, before the library runs);
// the library's own friendly errors are backstops. A missing card is a 404
// (ErrNoCard, errors.Is through the wrapping).

// kanbanBoardJSON is the whole board over the wire: the three columns in
// board order (the product ruling, so the widget never invents its own)
// and every card, top-to-bottom per column.
type kanbanBoardJSON struct {
	Columns []string         `json:"columns"`
	Cards   []kanbanCardJSON `json:"cards"`
}

// kanbanCardJSON is one card as the widget renders it. Times are RFC3339
// instants; an empty notes field is omitted.
type kanbanCardJSON struct {
	ID        string `json:"id"`
	Col       string `json:"col"`
	Position  int    `json:"position"`
	Title     string `json:"title"`
	Notes     string `json:"notes,omitempty"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// kanbanCardResponse is the shared body of the mutating endpoints: the
// stored truth after the write, not the caller's input.
type kanbanCardResponse struct {
	Card kanbanCardJSON `json:"card"`
}

// kanbanDeleteResponse confirms a removal.
type kanbanDeleteResponse struct {
	Deleted bool   `json:"deleted"`
	ID      string `json:"id"`
}

func kanbanCardToJSON(c kanban.Card) kanbanCardJSON {
	return kanbanCardJSON{
		ID:        c.ID,
		Col:       c.Col,
		Position:  c.Position,
		Title:     c.Title,
		Notes:     c.Notes,
		CreatedAt: c.CreatedAt.Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.Format(time.RFC3339),
	}
}

// requireKanbanDB resolves the board's store or writes the honest refusal.
func (s *Server) requireKanbanDB(w http.ResponseWriter) (*sql.DB, bool) {
	v := session.DefaultVault()
	if v == nil {
		writeStatus(w, http.StatusServiceUnavailable, "kanban: no vault is available (the board lives in a1.db)")
		return nil, false
	}
	return v.DB(), true
}

// decodeKanbanBody reads and decodes a capped JSON body; a body that will
// not decode answers 400 without the handler running.
func decodeKanbanBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "read body: "+err.Error())
		return false
	}
	if err := json.Unmarshal(body, dst); err != nil {
		writeStatus(w, http.StatusBadRequest, "bad request: "+err.Error())
		return false
	}
	return true
}

// mapKanbanErr answers the library's errors: a missing card is a 404, a
// vanished vault a 503; the validation errors are pre-checked in the
// handlers, so anything else reaching here is unexpected and a 500.
func mapKanbanErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, kanban.ErrNoCard):
		writeStatus(w, http.StatusNotFound, err.Error())
	case errors.Is(err, kanban.ErrNoVault):
		writeStatus(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeStatus(w, http.StatusInternalServerError, err.Error())
	}
}

// handleKanbanBoard serves the whole board — every column, board order,
// soonest-created cards first within a column (position order).
func (s *Server) handleKanbanBoard(w http.ResponseWriter, _ *http.Request) {
	db, ok := s.requireKanbanDB(w)
	if !ok {
		return
	}
	cards, err := kanban.List(db)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "kanban: "+err.Error())
		return
	}
	out := make([]kanbanCardJSON, 0, len(cards))
	for _, c := range cards {
		out = append(out, kanbanCardToJSON(c))
	}
	writeJSON(w, http.StatusOK, kanbanBoardJSON{Columns: kanban.Columns, Cards: out})
}

// kanbanAddInput is a POST body: title required, col defaults to todo (the
// backlog), an absent position appends — the "+ add card" affordance never
// thinks about ordering.
type kanbanAddInput struct {
	Col      string `json:"col"`
	Title    string `json:"title"`
	Notes    string `json:"notes"`
	Position *int   `json:"position"`
}

// handleKanbanAdd creates a card.
func (s *Server) handleKanbanAdd(w http.ResponseWriter, r *http.Request) {
	var in kanbanAddInput
	if !decodeKanbanBody(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Title) == "" {
		writeStatus(w, http.StatusBadRequest, "a card needs a title")
		return
	}
	col := strings.ToLower(strings.TrimSpace(in.Col))
	if col == "" {
		col = kanban.ColTodo
	}
	if !kanban.ValidColumn(col) {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("unknown column %q (want todo, doing, or done)", in.Col))
		return
	}
	db, ok := s.requireKanbanDB(w)
	if !ok {
		return
	}
	card, err := kanban.Add(db, kanban.AddInput{
		Col:      col,
		Title:    in.Title,
		Notes:    in.Notes,
		Position: in.Position,
	})
	if err != nil {
		mapKanbanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, kanbanCardResponse{Card: kanbanCardToJSON(card)})
}

// kanbanCardInput is a PUT body: any subset of move fields (col, position)
// and edit fields (title, notes) in one request. Absent fields are
// untouched; col/position absent keeps the current placement; a present
// empty notes string clears the notes — presence, not emptiness, carries
// intent (the calendar patch semantics).
type kanbanCardInput struct {
	ID       string  `json:"id"`
	Col      string  `json:"col"`
	Position *int    `json:"position"`
	Title    *string `json:"title"`
	Notes    *string `json:"notes"`
}

// handleKanbanMoveEdit is PUT /api/personal/kanban/card: a drag & drop is
// a move (col + position — the drop target's index in the destination
// column), an inline edit is title/notes, and the two compose because a
// widget that offers both should not need two round-trips.
func (s *Server) handleKanbanMoveEdit(w http.ResponseWriter, r *http.Request) {
	var in kanbanCardInput
	if !decodeKanbanBody(w, r, &in) {
		return
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	col := strings.ToLower(strings.TrimSpace(in.Col))
	if col != "" && !kanban.ValidColumn(col) {
		writeStatus(w, http.StatusBadRequest, fmt.Sprintf("unknown column %q (want todo, doing, or done)", in.Col))
		return
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
		writeStatus(w, http.StatusBadRequest, "a card needs a title")
		return
	}
	moves := col != "" || in.Position != nil
	edits := in.Title != nil || in.Notes != nil
	if !moves && !edits {
		writeStatus(w, http.StatusBadRequest, "nothing to change (pass col, position, title, and/or notes)")
		return
	}
	db, ok := s.requireKanbanDB(w)
	if !ok {
		return
	}
	var card kanban.Card
	var err error
	if moves {
		card, err = kanban.Move(db, kanban.MoveInput{ID: id, Col: col, Position: in.Position})
		if err != nil {
			mapKanbanErr(w, err)
			return
		}
	}
	if edits {
		card, err = kanban.Edit(db, id, kanban.EditInput{Title: in.Title, Notes: in.Notes})
		if err != nil {
			mapKanbanErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, kanbanCardResponse{Card: kanbanCardToJSON(card)})
}

// handleKanbanDelete is DELETE /api/personal/kanban/card?id=.
func (s *Server) handleKanbanDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	db, ok := s.requireKanbanDB(w)
	if !ok {
		return
	}
	if err := kanban.Delete(db, id); err != nil {
		mapKanbanErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, kanbanDeleteResponse{Deleted: true, ID: id})
}
