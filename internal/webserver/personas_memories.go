package webserver

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/damien1141/a1/internal/debuglog"
	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/persona"
)

// memorySummaryDTO is one row in the persona manager's memory panel.
//
// ID is the SEMANTIC store's row id (memstore), not a bank index: the panel
// lists what the memory engine actually keeps — the rows `remember`, the
// outlet's consolidation, and `recall` all operate on — so the delete and
// edit handles it hands back are the ids Revise/Forget take. The bank-derived
// view this replaced showed a different table entirely (the executor's
// per-call trace mirror), which is why the panel could read "empty bank"
// while the persona remembered plenty.
type memorySummaryDTO struct {
	ID        int64  `json:"id"`
	Content   string `json:"content"`
	Kind      string `json:"kind"`
	Scope     string `json:"scope"`
	CreatedAt string `json:"createdAt"`
}

// personaSemanticStore opens the persona's semantic memory store — the engine
// that remember/recall/consolidation all write to. Returns nil (no error) when
// the server has no embedder path, so the handler can answer with an honest
// empty list instead of failing the panel.
func (s *Server) personaSemanticStore(p persona.Persona) *memstore.Store {
	if s.personaStore == nil {
		return nil
	}
	store, err := s.personaStore.SemanticStore(p, s.openEmbedder(), memstore.DefaultParams())
	if err != nil {
		debuglog.Logf("personas: open semantic store for %s: %v", p.ID, err)
		return nil
	}
	return store
}

// handlePersonaMemories lists (GET), edits (PUT), or forgets (DELETE) entries
// in a persona's memory store.
//
//	GET    /api/personas/memories?id=<personaID>
//	PUT    /api/personas/memories  {id, mid, content}
//	DELETE /api/personas/memories?id=<personaID>&mid=<memoryID>
//
// Response for GET: {memories: [...], memoryEnabled: true}. Entries are
// returned newest-first — a history the user reads top-down.
func (s *Server) handlePersonaMemories(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		s.personaMemoriesQuery(w, r, id)
	case http.MethodPut:
		s.personaMemoriesEdit(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// personaMemoriesQuery serves the query-parameter shape (GET and DELETE).
func (s *Server) personaMemoriesQuery(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	p, err := s.personaStore.Get(id)
	if err != nil {
		writeStatus(w, http.StatusNotFound, "persona not found")
		return
	}
	store := s.personaSemanticStore(p)
	if store == nil {
		writeStatus(w, http.StatusInternalServerError, "persona memory store unavailable")
		return
	}

	if r.Method == http.MethodDelete {
		mid, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("mid")), 10, 64)
		if err != nil {
			writeStatus(w, http.StatusBadRequest, "mid query parameter must be an integer")
			return
		}
		if err := store.Forget(mid); err != nil {
			writeStatus(w, http.StatusInternalServerError, "forget memory: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "forgotten"})
		return
	}

	traces, err := store.List(200)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]memorySummaryDTO, 0, len(traces))
	for _, t := range traces {
		out = append(out, memorySummaryDTO{
			ID:        t.ID,
			Content:   t.Content,
			Kind:      t.Kind,
			Scope:     t.Scope,
			CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"memories":      out,
		"memoryEnabled": true,
	})
}

// personaMemoryEdit is the PUT body: which persona, which memory, and the new
// text. Content is required — an empty edit is a delete, and deletes have
// their own method.
type personaMemoryEdit struct {
	ID      string `json:"id"`
	Mid     int64  `json:"mid"`
	Content string `json:"content"`
}

// personaMemoriesEdit rewrites one memory's content. Revise re-embeds the new
// text and logs a correction row, so recall improves with the edit instead of
// serving the stale vector.
func (s *Server) personaMemoriesEdit(w http.ResponseWriter, r *http.Request) {
	var req personaMemoryEdit
	if err := decodeBody(r, &req); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeStatus(w, http.StatusBadRequest, "content is required (use DELETE to remove a memory)")
		return
	}
	p, err := s.personaStore.Get(req.ID)
	if err != nil {
		writeStatus(w, http.StatusNotFound, "persona not found")
		return
	}
	store := s.personaSemanticStore(p)
	if store == nil {
		writeStatus(w, http.StatusInternalServerError, "persona memory store unavailable")
		return
	}
	if err := store.Revise(r.Context(), req.Mid, content, "web edit"); err != nil {
		writeStatus(w, http.StatusNotFound, "no memory with that id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revised"})
}

// personaMemoryCount is the badge figure the persona list carries. It counts
// the semantic store's active rows — the same source the memory panel lists —
// so the badge and the list can never disagree. A persona whose store cannot
// be opened reports 0 rather than failing the whole list: one broken bank
// should not blank the registry.
func (s *Server) personaMemoryCount(p persona.Persona) int {
	store := s.personaSemanticStore(p)
	if store == nil {
		return 0
	}
	traces, err := store.List(200)
	if err != nil {
		debuglog.Logf("personas: count memories for %s: %v", p.ID, err)
		return 0
	}
	return len(traces)
}
