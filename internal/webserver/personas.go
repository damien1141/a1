package webserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/persona"
	"github.com/damien1141/a1/internal/project"
	"github.com/damien1141/a1/internal/session/memory"
	"github.com/damien1141/a1/internal/weather"
)

// personaDTO is the JSON shape the PersonaManager UI reads / writes. It is
// a superset of the original PersonaManager contract ({id, name, tagline,
// emoji, hue, prompt, builtin, memoryCount}) plus this backend's extras
// (hasAvatar for uploaded profile pictures, isActive for the active marker,
// description as the legacy alias of tagline).
type personaDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Tagline     string `json:"tagline"`
	Description string `json:"description"` // legacy alias of tagline
	Emoji       string `json:"emoji"`
	Hue         int    `json:"hue"`
	Prompt      string `json:"prompt"`
	// OverrideSystemPrompt marks a persona whose prompt replaces the
	// default a1 system prompt entirely (the persona-menu toggle).
	OverrideSystemPrompt bool `json:"overrideSystemPrompt"`
	Builtin              bool `json:"builtin"`
	HasAvatar            bool `json:"hasAvatar"`
	IsActive             bool `json:"isActive"`
	// MemoryCount is filled by the caller that has the memory dir handy.
	MemoryCount int `json:"memoryCount"`
	// Per-persona runtime overrides (persona editor's settings section).
	// MemoryEnabled is a pointer because nil means "inherit the global
	// memory toggle"; the other two are "" = inherit.
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permissionMode,omitempty"`
	MemoryEnabled  *bool  `json:"memoryEnabled,omitempty"`
	// MemoryModel is the dedicated consolidation model (Bet 06),
	// "" = use Model.
	MemoryModel string `json:"memoryModel,omitempty"`
}

func toDTO(p persona.Persona, activeID string, memoryCount int) personaDTO {
	tagline := p.Tagline
	if tagline == "" {
		tagline = p.Description
	}
	return personaDTO{
		ID:                   p.ID,
		Name:                 p.Name,
		Tagline:              tagline,
		Description:          tagline,
		Emoji:                p.Emoji,
		Hue:                  p.Hue,
		Prompt:               p.SystemPrompt,
		OverrideSystemPrompt: p.OverrideSystemPrompt,
		Builtin:              p.Builtin || p.ID == persona.DefaultPersonaID,
		HasAvatar:            p.HasAvatar(),
		IsActive:             p.ID == activeID,
		MemoryCount:          memoryCount,
		Model:                p.Model,
		PermissionMode:       p.PermissionMode,
		MemoryEnabled:        p.MemoryEnabled,
		MemoryModel:          p.MemoryModel,
	}
}

// personaInput is the request body for create / update. Tagline and
// Description are aliases (the old UI sends tagline; older clients sent
// description); Prompt and SystemPrompt likewise.
type personaInput struct {
	Name         string `json:"name"`
	Tagline      string `json:"tagline"`
	Description  string `json:"description"`
	Emoji        string `json:"emoji"`
	Hue          int    `json:"hue"`
	Prompt       string `json:"prompt"`
	SystemPrompt string `json:"systemPrompt"`
	// OverrideSystemPrompt is a pointer so the partial-update contract
	// can distinguish "not sent" (keep stored) from an explicit false
	// (toggle off) — a bare bool could only ever turn it on.
	OverrideSystemPrompt *bool `json:"overrideSystemPrompt"`
	// Per-persona settings (model / permission mode / memory toggle).
	// Same pointer contract: nil = not sent = keep stored; non-nil sets
	// the field exactly (empty string clears an override).
	Model          *string `json:"model"`
	PermissionMode *string `json:"permissionMode"`
	MemoryEnabled  *bool   `json:"memoryEnabled"`
	// MemoryModel routes consolidation traffic to a dedicated model
	// (Bet 06 persona-editor toggle). Same pointer contract.
	MemoryModel *string `json:"memoryModel"`
}

func (in personaInput) toStoreInput() persona.Input {
	tagline := in.Tagline
	if tagline == "" {
		tagline = in.Description
	}
	prompt := in.Prompt
	if prompt == "" {
		prompt = in.SystemPrompt
	}
	return persona.Input{
		Name:                 in.Name,
		Tagline:              tagline,
		Emoji:                in.Emoji,
		Hue:                  in.Hue,
		Prompt:               prompt,
		SystemPrompt:         in.SystemPrompt,
		OverrideSystemPrompt: in.OverrideSystemPrompt,
	}
}

// personaListResponse is the GET /api/personas envelope. The old UI
// tolerates a malformed body by degrading to an empty palette, so the
// field names here are contract, not convention.
type personaListResponse struct {
	Personas      []personaDTO `json:"personas"`
	Active        string       `json:"active"`
	MemoryEnabled bool         `json:"memoryEnabled"`
}

// modelInfo is one entry of GET /api/models: the catalog the persona
// editor's model dropdown offers, plus the context window the footer
// needs to re-scale its counter when the active model changes.
type modelInfo struct {
	Name          string `json:"name"`
	ContextWindow int    `json:"contextWindow"`
	Default       bool   `json:"default"`
}

// handleListModels serves GET /api/models — the project's configured model
// catalog with each entry's context window. Read-only: the config API's
// POST /api/models (model testing) is a different concern on purpose.
func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	cfg := s.proj.Config()
	if cfg == nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []modelInfo{}})
		return
	}
	out := make([]modelInfo, 0, len(cfg.Models))
	for _, m := range cfg.AllModels() {
		out = append(out, modelInfo{
			Name:          m.Name,
			ContextWindow: m.ContextWindow,
			Default:       m.Name == cfg.DefaultModel,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}

// store returns the persona store, or nil when the server was built
// without a project (tests).
func (s *Server) personaStoreOrEmpty() *persona.Store { return s.personaStore }

// personaMemoryBank opens the persona's memory bank THROUGH the persona
// store — the single source of truth for where persona banks live (the
// store's memory dir). The agent's remember mirror writes to the exact same
// handle, so what the manager counts and lists can never drift from what
// the agent saves again.
func (s *Server) personaMemoryBank(p persona.Persona) (*memory.Bank, error) {
	return s.personaStore.MemoryBank(p)
}

// handleListPersonas returns the registry envelope: every persona (with
// memory counts), the active id, and the memory-enabled flag. Persona
// memory is always scoped per-profile in this backend, so memoryEnabled is
// reported as true — the manager's "agent memory is off" warning only
// applies to the old opt-in flag, which has no equivalent here.
func (s *Server) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeJSON(
			w,
			http.StatusOK,
			personaListResponse{Personas: []personaDTO{}, Active: persona.DefaultPersonaID, MemoryEnabled: true},
		)
		return
	}
	personas, err := s.personaStore.List()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	active, _ := s.personaStore.Active()
	out := make([]personaDTO, 0, len(personas))
	for _, p := range personas {
		// MemoryCount is the non-trace figure: the executor logs every
		// tool run into the bank, and "N memories" that mostly counts
		// bash invocations reads as broken. See memory.IsTrace.
		count := 0
		if bank, err := s.personaMemoryBank(p); err == nil {
			count = bank.MemoryCount()
		}
		out = append(out, toDTO(p, active, count))
	}
	writeJSON(w, http.StatusOK, personaListResponse{
		Personas:      out,
		Active:        active,
		MemoryEnabled: true,
	})
}

// handleGetActivePersona returns the currently active persona as a bare
// DTO (kept from this backend's earlier contract; the manager uses the
// list envelope, but the chat page's lighter polling may use this).
func (s *Server) handleGetActivePersona(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	p, err := s.personaStore.ActivePersona()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}
	active, _ := s.personaStore.Active()
	writeJSON(w, http.StatusOK, toDTO(p, active, 0))
}

// handleCreatePersona creates a new persona and activates it. Body:
// {name, tagline?, emoji?, hue?, prompt?}. Response: {persona: {...}} —
// the envelope the manager's save() flow expects.
func (s *Server) handleCreatePersona(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	var in personaInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeStatus(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	p, err := s.personaStore.CreateInput(in.toStoreInput())
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, persona.ErrNameRequired) {
			status = http.StatusBadRequest
		}
		writeStatus(w, status, err.Error())
		return
	}
	active, _ := s.personaStore.Active()
	writeJSON(w, http.StatusCreated, map[string]personaDTO{"persona": toDTO(p, active, 0)})
}

// handleUpdatePersona mutates an existing persona. Accepts both the old
// UI's path-param form (PUT /api/personas/{id}) and this backend's legacy
// body-id form (PUT /api/personas with {id}). Partial input: zero fields
// keep the stored value. The default persona's name is locked.
func (s *Server) handleUpdatePersona(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	// Body decoded twice: personaInput for the profile fields, and a tiny
	// struct that picks up the legacy body-carried `id` when the path
	// didn't provide one (PUT /api/personas with {id, ...}).
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var in personaInput
	if err := json.Unmarshal(body, &in); err != nil {
		writeStatus(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	id := r.PathValue("id")
	if id == "" {
		var legacy struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(body, &legacy)
		id = legacy.ID
	}
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	input := in.toStoreInput()
	p, err := s.personaStore.UpdateProfile(id, &input)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, persona.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, persona.ErrNameRequired):
			status = http.StatusBadRequest
		}
		writeStatus(w, status, err.Error())
		return
	}
	// Settings (model / permission mode / memory toggle) ride the same
	// endpoint: the editor sends all three explicitly, older clients
	// send none and the store keeps them untouched. The permission mode
	// is validated here so a typo'd mode is a 400, not a silently dead
	// override that appliesPersonaSettings later no-ops on.
	if in.MemoryModel != nil {
		p, err = s.personaStore.SetMemoryModel(id, *in.MemoryModel)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, persona.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeStatus(w, status, err.Error())
			return
		}
	}
	if in.Model != nil || in.PermissionMode != nil || in.MemoryEnabled != nil {
		if in.PermissionMode != nil && *in.PermissionMode != "" {
			if _, ok := modeSummary[permission.Mode(*in.PermissionMode)]; !ok {
				writeStatus(w, http.StatusBadRequest,
					"unknown permission mode "+*in.PermissionMode)
				return
			}
		}
		p, err = s.personaStore.SetSettings(id, in.Model, in.PermissionMode, in.MemoryEnabled)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, persona.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeStatus(w, status, err.Error())
			return
		}
	}
	// Editing the ACTIVE persona can change what the next turn looks
	// like — its prompt, or the replace-default-system-prompt toggle —
	// so push the (re-read) profile to every live engine: the session's
	// persona injection is re-armed and the LLM client's system prompt
	// is rebuilt. Without this, a toggle flip only took effect on the
	// next brand-new chat.
	if active, _ := s.personaStore.Active(); active == p.ID {
		s.applyPersonaToEngines()
		s.broadcastState()
	}
	active, _ := s.personaStore.Active()
	writeJSON(w, http.StatusOK, map[string]personaDTO{"persona": toDTO(p, active, 0)})
}

// handleDeletePersona deletes a persona. Accepts both DELETE
// /api/personas/{id} and DELETE /api/personas?id=. The default persona
// cannot be deleted; deleting the active persona reverts to default.
func (s *Server) handleDeletePersona(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		id = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := s.personaStore.Delete(id); err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, persona.ErrNotFound):
			status = http.StatusNotFound
		case errors.Is(err, persona.ErrCannotDeleteDefault):
			status = http.StatusBadRequest
		}
		writeStatus(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// setActivePersonaRequest is the body for POST /api/personas/active.
type setActivePersonaRequest struct {
	ID string `json:"id"`
}

// handleSetActivePersona switches the active persona and rebinds every
// live engine's session so the next turn reflects the new personality AND
// the new memory bank (Session.MemoryBank re-evaluates per call).
// Response: {active, persona} — the manager's switchTo() flow expects the
// persona back so it can toast "Now speaking as <name>".
func (s *Server) handleSetActivePersona(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	var req setActivePersonaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeStatus(w, http.StatusBadRequest, "bad request: "+err.Error())
		return
	}
	if req.ID == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := s.personaStore.SetActive(req.ID); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, persona.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeStatus(w, status, err.Error())
		return
	}
	// Propagate the switch to every live engine's session so the next turn
	// reflects the new personality and memory bank, mirror it into the
	// UI-knob state, and broadcast so every tab's footer persona chip
	// and the Ctrl+K palette follow the switch live.
	s.applyPersonaToEngines()
	s.broadcastState()
	p, err := s.personaStore.Get(req.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"active": req.ID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":  req.ID,
		"persona": toDTO(p, req.ID, 0),
	})
}

// memorySummaryDTO is one row in the manager's memory-bank preview.
// ID is the entry's index within the bank (the handle DeleteAt takes).
type memorySummaryDTO struct {
	ID      int    `json:"id"`
	Content string `json:"content"`
	Kind    string `json:"kind"`
	// Scope and Thread exist in the old UI's type but this backend's banks
	// don't track them; they're omitted so the UI's optional rendering
	// simply skips them.
	CreatedAt string `json:"createdAt"`
}

// handlePersonaMemories lists (GET) or forgets (DELETE) entries in a
// persona's memory bank.
//
//	GET    /api/personas/memories?id=<personaID>
//	DELETE /api/personas/memories?id=<personaID>&mid=<index>
//
// Response for GET: {memories: [...], memoryEnabled: true}. The entries
// are returned oldest-first (the manager renders them top-down as a
// history).
func (s *Server) handlePersonaMemories(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id query parameter is required")
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	p, err := s.personaStore.Get(id)
	if err != nil {
		writeStatus(w, http.StatusNotFound, "persona not found")
		return
	}
	bank, err := s.personaMemoryBank(p)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, err.Error())
		return
	}

	switch r.Method {
	case http.MethodGet:
		// Filtered list: tool-run traces are bookkeeping, not memories,
		// so they are excluded here and from the count in
		// handleListPersonas. ID stays the RAW bank index — DeleteAt
		// takes that, so a filtered position would drop the wrong line.
		indexed, err := bank.ListMemoriesIndexed()
		if err != nil {
			writeStatus(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]memorySummaryDTO, 0, len(indexed))
		for _, ie := range indexed {
			out = append(out, memorySummaryDTO{
				ID:        ie.Index,
				Content:   ie.Entry.Content,
				Kind:      string(ie.Entry.Type),
				CreatedAt: ie.Entry.Timestamp.Format("2006-01-02T15:04:05Z"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"memories":      out,
			"memoryEnabled": true,
		})
	case http.MethodDelete:
		midStr := strings.TrimSpace(r.URL.Query().Get("mid"))
		mid, err := strconv.Atoi(midStr)
		if err != nil {
			writeStatus(w, http.StatusBadRequest, "mid query parameter must be an integer")
			return
		}
		if err := bank.DeleteAt(mid); err != nil {
			writeStatus(w, http.StatusNotFound, "no memory with that id")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "forgotten"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// fmtSscan helpers removed — strconv.Atoi is the obvious way to parse mid.

// handlePersonaAvatar accepts a multipart upload with the avatar image. The
// persona id is taken from the ?id= query (or the multipart `id` field).
// The avatar file is written to ~/.a1/personas/<id>/avatar.<ext>. When the
// persona has an uploaded avatar, the manager's Avatar renders the image
// instead of the emoji-on-gradient fallback.
//
// The avatar body is capped at 10 MiB to match the rest of the project's
// image-pipeline ceiling; anything larger is rejected with 413.
const maxAvatarBytes = 10 << 20 // 10 MiB

func (s *Server) handlePersonaAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		writeStatus(w, http.StatusBadRequest, "parse multipart: "+err.Error())
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		id = strings.TrimSpace(r.URL.Query().Get("id"))
	}
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "id is required")
		return
	}
	file, header, err := r.FormFile("avatar")
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "avatar file is required")
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "read avatar: "+err.Error())
		return
	}
	if int64(len(body)) > maxAvatarBytes {
		writeStatus(w, http.StatusRequestEntityTooLarge, "avatar exceeds 10 MiB cap")
		return
	}
	// Detect MIME from the first 512 bytes (sniffing like the rest of the
	// image pipeline). header.Header.Get("Content-Type") is the browser's
	// guess, which is unreliable.
	mimeType := sniffMIME(body)
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
	}
	p, err := s.personaStore.SetAvatar(id, mimeType, body)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, persona.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeStatus(w, status, err.Error())
		return
	}
	active, _ := s.personaStore.Active()
	writeJSON(w, http.StatusOK, map[string]personaDTO{"persona": toDTO(p, active, 0)})
}

// handleServePersonaAvatar serves the persona's avatar bytes (or 404 when
// the persona has no avatar). The MIME type is sniffed from the file so
// the browser renders inline.
func (s *Server) handleServePersonaAvatar(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	if s.personaStore == nil {
		writeStatus(w, http.StatusInternalServerError, "persona store not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeStatus(w, http.StatusBadRequest, "persona id is required")
		return
	}
	p, err := s.personaStore.Get(id)
	if err != nil {
		writeStatus(w, http.StatusNotFound, "persona not found")
		return
	}
	if !p.HasAvatar() {
		writeStatus(w, http.StatusNotFound, "persona has no avatar")
		return
	}
	path := s.personaStore.AvatarPath(p)
	w.Header().Set("Content-Type", sniffMIMEForFile(path))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}

// sniffMIME returns the MIME type from the first 512 bytes (matching the
// TUI's image.Load pipeline).
func sniffMIME(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	if len(body) > 512 {
		body = body[:512]
	}
	return http.DetectContentType(body)
}

// sniffMIMEForFile reads the first 512 bytes from disk and sniffs the MIME.
// Falls back to "application/octet-stream" on read error.
func sniffMIMEForFile(path string) string {
	f, err := os.Open(path) //nolint:gosec // G304: path is from the trusted persona store
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	return http.DetectContentType(buf[:n])
}

// weatherConfigFromProject reads the weather block from the project config
// and translates it to a weather.Config. Returns the zero Config when the
// project is unset or the weather block is absent (default-off).
func weatherConfigFromProject(proj *project.Project) weather.Config {
	if proj == nil || proj.Config() == nil {
		return weather.Config{}
	}
	cfg := proj.Config()
	if cfg.Weather == nil {
		return weather.Config{}
	}
	return weather.Config{
		Enabled:   cfg.Weather.Enabled,
		Latitude:  cfg.Weather.Latitude,
		Longitude: cfg.Weather.Longitude,
		Name:      cfg.Weather.Name,
	}
}

// RefreshWeather re-reads the weather block from the project config and
// updates the live weather service. Called after the config editor saves
// so toggling weather.enabled in the UI takes effect without a restart.
func (s *Server) RefreshWeather() {
	if s == nil || s.weatherSvc == nil {
		return
	}
	s.weatherSvc.UpdateConfig(weatherConfigFromProject(s.proj))
}
