// Package webserver — slashapi.go
//
// This file wires the slash-command surface the TUI exposes (compact,
// context, branch, diff, code, snapshot, permission, settings, skills,
// extensions) to the same engine + project the chat API already drives.
//
// Each handler is small and self-contained: they read state from the
// server (mode, models, branches) or operate on a session's engine
// (compact, context, snapshot). Slash-command UI side effects that the
// TUI shows as overlays (diff, code, branch picker) are here too — the
// web renders them as modals, but the data fetch is identical.
package webserver

import (
        "context"
        "encoding/json"
        "net/http"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/agent"
        "github.com/damien1141/a1/internal/contextbreakdown"
        "github.com/damien1141/a1/internal/extension"
        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tools/snapshottool"
        "github.com/damien1141/a1/internal/util/diffreview"
        "github.com/damien1141/a1/internal/util/gitx"
)

// toast broadcasts a notification to every browser tab. sessionID=""
// means "broadcast to every session" — the browser renders it globally.
func (s *Server) toast(sessionID string, level toastLevel, msg string, duration time.Duration) {
        ms := int(duration / time.Millisecond)
        s.events().publish(sessionID, serverEvent{
                Kind:  eventToast,
                Toast: &toastPayload{Level: level, Message: msg, Duration: ms},
        })
}

// broadcastState pushes the current UI state to every connected browser.
// Call after any knob changes (model, mode, agents, memory, think).
func (s *Server) broadcastState() {
        st := s.stateView()
        s.events().publish("", serverEvent{Kind: eventState, State: &st})
}

// stateView builds the live UI state for the footer chrome.
// branchName is best-effort: a missing repo or detached HEAD yields "".
func (s *Server) stateView() statePayload {
        snap := s.stateStore().snapshot()
        st := statePayload{
                // Effective model: the manual knob when set, otherwise the
                // ACTIVE persona's Model — persona settings are the default
                // selection, so the chip names what a new session would
                // actually run instead of an empty "default" placeholder.
                Model:         s.effectiveModelName(),
                Think:         snap.Think,
                // Effective mode: the persona's explicit PermissionMode when
                // set, otherwise the surface's raw mode — the same layer
                // engines fold in applyPersonaSettings, so the pill and the
                // gate can never disagree.
                Mode:          s.effectivePermissionMode(),
                AgentsEnabled: snap.AgentsEnabled,
                // Effective memory: the active persona's explicit
                // memory_enabled when set, otherwise the global toggle —
                // the same value the engines gate on, so the chip and the
                // behavior can never disagree.
                MemoryEnabled: s.effectiveMemoryEnabled(snap.MemoryEnabled),
                Persona:       s.personaView(),
        }
        if s.proj != nil && s.proj.Config() != nil {
                cfg := s.proj.Config().Model()
                st.ContextWindow = cfg.ContextWindow
                if st.Model != "" {
                        for _, m := range s.proj.Config().AllModels() {
                                if m.Name == st.Model {
                                        st.ContextWindow = m.ContextWindow
                                        break
                                }
                        }
                }
        }
        // A live engine wins over the config/mirror values across the board:
        // its model NAME follows persona switches (ModelName returns the
        // persona override after SetPersona), its window is learned by
        // probing local llama.cpp/ollama servers and from provider overflow
        // errors, so the footer's chip + denominator track reality without
        // a refresh.
        s.mu.Lock()
        for _, le := range s.engines {
                if le != nil && le.engine != nil {
                        if n := le.engine.ModelName(); n != "" {
                                st.Model = n
                        }
                        w := le.engine.ContextWindow()
                        // Before the engine's window resolves (first turn),
                        // fall back to the ACTIVE model's configured window —
                        // that is the model the persona switch just selected,
                        // not the global default the mirror may still name.
                        if w <= 0 && s.proj != nil && s.proj.Config() != nil && st.Model != "" {
                                if m, ok := s.proj.Config().FindModel(st.Model); ok {
                                        w = m.ContextWindow
                                }
                        }
                        if w > 0 {
                                st.ContextWindow = w
                        }
                        st.ToolBytes, st.ToolCount = le.engine.ToolSchemaBytes()
                        st.ContextTokens = le.engine.ContextTokens()
                        break
                }
        }
        s.mu.Unlock()
        // Unknown models still get the assumed fallback so the footer chip
        // renders ("0% · …/32k") instead of hiding.
        if st.ContextWindow <= 0 {
                st.ContextWindow = llm.AssumedContextWindow
        }
        st.Branch = s.currentBranch()
        return st
}

// currentBranch returns the cwd's git branch, "" when unavailable.
// Run inline; a missing repo or detached HEAD yields "" via gitx.
func (s *Server) currentBranch() string {
        ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
        defer cancel()
        dir, _ := os.Getwd()
        if dir == "" {
                return ""
        }
        branches, err := gitx.Branches(ctx, dir)
        if err != nil {
                return ""
        }
        for _, b := range branches {
                if b.Current {
                        return b.Name
                }
        }
        return ""
}

// publishToast is a tiny helper for slash handlers that need a toast without
// the duration verbosity.
func (s *Server) publishToast(sessionID, msg string, level toastLevel) {
        s.toast(sessionID, level, msg, 3*time.Second)
}

// ---- state endpoint ----

// handleGetState returns the live UI knobs. The browser fetches this on
// load and again whenever it sees a state event over SSE.
func (s *Server) handleGetState(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, s.stateView())
}

// ---- compact endpoint ----

// handleCompact triggers a forced context compaction on the named session.
// Refused while a turn is in flight: compaction mutates the session tree
// the engine is mid-turn building, which would race the engine's own
// session-tree mutation.
func (s *Server) handleCompact(w http.ResponseWriter, r *http.Request) {
        var body struct {
                SessionID string `json:"sessionId"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        id := strings.TrimSpace(body.SessionID)
        if id == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }
        s.mu.Lock()
        le, ok := s.engines[id]
        s.mu.Unlock()
        if !ok || le == nil || le.engine == nil {
                writeStatus(w, http.StatusNotFound, "session not found")
                return
        }
        // Try to acquire the per-session turn mutex non-blockingly. If the
        // session is mid-turn, refuse — same guard as CompactCommands.Run.
        if !le.turn.TryLock() {
                s.publishToast(id, "Cannot compact while a reply or command is running", toastWarning)
                writeStatus(w, http.StatusConflict, "session is busy")
                return
        }
        defer le.turn.Unlock()

        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        s.events().publish(id, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "compacting context…"}})
        ok2, err := le.engine.ForceCompact(ctx, func(ev session.Event, _ error) bool {
                s.projectEvent(id, le.engine.ContextWindow(), "", ev)
                return true
        })
        if err != nil {
                s.publishToast(id, "Compaction failed: "+err.Error(), toastError)
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        if !ok2 {
                s.publishToast(id, "Nothing to compact", toastWarning)
                writeJSON(w, http.StatusOK, map[string]string{"status": "noop"})
                return
        }
        s.publishToast(id, "Context compacted", toastSuccess)
        s.events().publish(id, serverEvent{Kind: eventStatus, Status: &statusPayload{Text: "compacted context"}})
        writeJSON(w, http.StatusOK, map[string]string{"status": "compacted"})
}

// ---- context endpoint ----

// handleContext returns the context-window breakdown for the named
// session. Renders as JSON so the browser draws the same table the TUI
// emits as text.
func (s *Server) handleContext(w http.ResponseWriter, r *http.Request) {
        id := strings.TrimSpace(r.URL.Query().Get("id"))
        if id == "" {
                writeStatus(w, http.StatusBadRequest, "session id is required")
                return
        }
        le, err := s.liveFor(id)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        snap, err := s.transcript(id)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        window := 0
        if cfg, ok := s.effectiveModelConfig(); ok {
                window = cfg.ContextWindow
        }
        toolBytes, toolCount := 0, 0
        if le != nil && le.engine != nil {
                toolBytes, toolCount = le.engine.ToolSchemaBytes()
                // The engine's live window wins over the config value: it
                // reflects probing (llama.cpp /props, ollama context_length)
                // and windows learned from provider overflow errors — the
                // same resolution the footer's state chip uses.
                if w := le.engine.ContextWindow(); w > 0 {
                        window = w
                }
        }
        if window <= 0 {
                window = llm.AssumedContextWindow
        }
        b := contextbreakdown.Build(snap, window, toolBytes, toolCount)
        resp := map[string]any{
                "contextWindow": b.ContextWindow,
                "total":         b.Total(),
                "usedPercent":   b.UsedPercent(),
                "categories":    b.Categories,
                "text":          b.Estimate(),
        }
        writeJSON(w, http.StatusOK, resp)
}

// ---- branches endpoint ----

// branchJSON is the wire shape for /api/branches.
type branchJSON struct {
        Name      string `json:"name"`
        Current   bool   `json:"current"`
        Remote    bool   `json:"remote"`
        Upstream  string `json:"upstream,omitempty"`
        Ahead     int    `json:"ahead,omitempty"`
        Behind    int    `json:"behind,omitempty"`
        Committed string `json:"committed,omitempty"`
        Subject   string `json:"subject,omitempty"`
}

// handleListBranches returns local + remote branches for the cwd, newest
// commit first.
func (s *Server) handleListBranches(w http.ResponseWriter, r *http.Request) {
        dir := strings.TrimSpace(r.URL.Query().Get("dir"))
        if dir == "" {
                dir, _ = os.Getwd()
        }
        ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
        defer cancel()
        branches, err := gitx.Branches(ctx, dir)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        recent, _ := gitx.Recent(ctx, dir)
        out := make([]branchJSON, 0, len(branches))
        for _, b := range branches {
                out = append(out, branchJSON{
                        Name:      b.Name,
                        Current:   b.Current,
                        Remote:    b.Remote,
                        Upstream:  b.Upstream,
                        Ahead:     b.Ahead,
                        Behind:    b.Behind,
                        Committed: b.Committed,
                        Subject:   b.Subject,
                })
        }
        // Derive the current branch from the branches slice we already fetched
        // (Branches returns exactly one entry with Current=true). The previous
        // code called s.currentBranch() which shells out to `git branch` a second
        // time — up to 5s each on a slow disk — for data we already had.
        current := ""
        for _, b := range branches {
                if b.Current {
                        current = b.Name
                        break
                }
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "branches": out,
                "recent":   recent,
                "current":  current,
                "dir":      dir,
        })
}

// handleSwitchBranch switches to (or creates) the named branch.
//
// {"dir":"...", "name":"feat/x", "create":true}
//
// dir is optional; the server's cwd is used when empty. create=true means
// branch off HEAD when name does not yet exist (matches the TUI's
// /branch <name> semantics).
func (s *Server) handleSwitchBranch(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Dir    string `json:"dir"`
                Name   string `json:"name"`
                Create bool   `json:"create"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        name := strings.TrimSpace(body.Name)
        if !gitx.ValidRef(name) {
                writeStatus(w, http.StatusBadRequest, "not a valid branch name: "+strconv.Quote(name))
                return
        }
        dir := strings.TrimSpace(body.Dir)
        if dir == "" {
                dir, _ = os.Getwd()
        }
        ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
        defer cancel()

        // Preflight: refuse when an unfinished merge/rebase is in progress.
        st, err := gitx.Preflight(ctx, dir)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        if st.Op != "" {
                writeStatus(w, http.StatusConflict, "finish or abort the in-progress "+st.Op+" first")
                return
        }

        branches, err := gitx.Branches(ctx, dir)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        exists := false
        for _, b := range branches {
                if b.Name == name {
                        exists = true
                        break
                }
        }
        if !exists && !body.Create {
                writeStatus(w, http.StatusNotFound, "branch not found: "+name)
                return
        }
        if !exists {
                if err := gitx.Create(ctx, dir, name); err != nil {
                        writeStatus(w, http.StatusInternalServerError, err.Error())
                        return
                }
                s.toast("", toastSuccess, "Created "+name, 3*time.Second)
        } else {
                if err := gitx.Switch(ctx, dir, name); err != nil {
                        writeStatus(w, http.StatusInternalServerError, err.Error())
                        return
                }
                s.toast("", toastSuccess, "Switched to "+name, 3*time.Second)
        }
        writeJSON(w, http.StatusOK, map[string]string{
                "status":  "ok",
                "branch":  name,
                "current": s.currentBranch(),
        })
        s.broadcastState()
}

// ---- diff endpoint ----

// handleDiff returns unified-diff text for the requested spec.
//
// ?what=staged  → git diff --staged
// ?what=HEAD    → git show -p HEAD
// ?what=        → git diff (working tree)
// ?dir=         → override cwd
//
// Returns JSON so the browser can render the diff alongside its label
// and an "empty" hint (mirrors diffreview.EmptyNote).
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
        dir := strings.TrimSpace(r.URL.Query().Get("dir"))
        if dir == "" {
                dir, _ = os.Getwd()
        }
        what := strings.TrimSpace(r.URL.Query().Get("what"))
        var spec []string
        switch strings.ToLower(what) {
        case "", "worktree", "working":
                spec = nil
        case "staged", "--staged", "--cached":
                spec = []string{"staged"}
        case "head":
                spec = []string{"HEAD"}
        default:
                // The user-supplied `what` is forwarded to `git diff` as a refspec.
                // Without a `--` separator, a value starting with `--` (e.g.
                // `?what=--output=/tmp/evil`) is interpreted by git as a flag,
                // allowing arbitrary file writes. Pass `--` first so anything
                // after is treated as a path/rev, then refuse values that still
                // look like flags just in case the revs pass through to a subcommand
                // that re-parses argv.
                if strings.HasPrefix(what, "-") {
                        writeStatus(w, http.StatusBadRequest, "diff: invalid refspec (starts with '-')")
                        return
                }
                spec = []string{"--", what}
        }
        ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
        defer cancel()
        text, err := diffreview.LoadGit(ctx, dir, spec)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        label := diffreview.LabelForSpec(spec)
        empty := ""
        if strings.TrimSpace(text) == "" {
                empty = diffreview.EmptyNote(spec)
        }
        writeJSON(w, http.StatusOK, map[string]string{
                "spec":  label,
                "diff":  text,
                "empty": empty,
        })
}

// ---- code endpoint ----

// handleCode reads one file's contents for the in-browser code viewer.
//
// ?path=<rel-or-abs>&line=<n>
//
// Path confinement: the path is resolved against the cwd (or project root)
// and must stay inside it. Anything escaping returns 403. Symlinks pointing
// outside are also refused.
func (s *Server) handleCode(w http.ResponseWriter, r *http.Request) {
        rel := strings.TrimSpace(r.URL.Query().Get("path"))
        if rel == "" {
                writeStatus(w, http.StatusBadRequest, "path is required")
                return
        }
        cwd, _ := os.Getwd()
        root := cwd
        if s.proj != nil && s.proj.Root() != "" {
                root = s.proj.Root()
        }
        abs := rel
        if !filepath.IsAbs(abs) {
                abs = filepath.Join(root, rel)
        }
        abs = filepath.Clean(abs)
        rootClean := filepath.Clean(root)
        if abs != rootClean && !strings.HasPrefix(abs+string(os.PathSeparator), rootClean+string(os.PathSeparator)) {
                writeStatus(w, http.StatusForbidden, "path escapes project root")
                return
        }
        // Resolve symlinks; refuse if the resolved target is outside root.
        if resolved, err := filepath.EvalSymlinks(abs); err == nil && resolved != abs {
                if resolved != rootClean && !strings.HasPrefix(resolved+string(os.PathSeparator), rootClean+string(os.PathSeparator)) {
                        writeStatus(w, http.StatusForbidden, "path escapes project root")
                        return
                }
                abs = resolved
        }
        data, err := os.ReadFile(abs)
        if err != nil {
                writeStatus(w, http.StatusNotFound, err.Error())
                return
        }
        // Strip a leading root prefix so the browser shows a project-relative
        // path (matches what /code path:line accepts in the TUI).
        display := abs
        if strings.HasPrefix(display, rootClean+string(os.PathSeparator)) {
                display = display[len(rootClean)+1:]
        }
        line := 0
        if v := strings.TrimSpace(r.URL.Query().Get("line")); v != "" {
                if n, err := strconv.Atoi(v); err == nil && n > 0 {
                        line = n
                }
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "path":    display,
                "abs":     abs,
                "line":    line,
                "content": string(data),
                "size":    len(data),
        })
}

// ---- snapshot endpoint ----

// handleSnapshot runs the explicit snapshot tool. Body mirrors the TUI's
// /snapshot create|rollback|list|delete [branch] slash command.
//
// {"action":"create|rollback|list|delete", "branch":"feat/x"}
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Action string `json:"action"`
                Branch string `json:"branch"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        body.Action = strings.TrimSpace(body.Action)
        if body.Action == "" {
                writeStatus(w, http.StatusBadRequest, "usage: action=create|rollback|list|delete [branch]")
                return
        }
        // Validate the action up front so a typo (e.g. "creat") returns 400
        // instead of dispatching into the snapshot tool, which would surface the
        // same error as a 500 (looks like a server bug rather than a user typo).
        switch body.Action {
        case "create", "rollback", "list", "delete":
                // ok
        default:
                writeStatus(w, http.StatusBadRequest,
                        "unknown action: "+body.Action+" (want create|rollback|list|delete)")
                return
        }
        raw, _ := json.Marshal(body)
        ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
        defer cancel()
        res, err := snapshottool.SnapshotTool().Run(ctx, raw)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        writeJSON(w, http.StatusOK, map[string]string{
                "action":  body.Action,
                "content": res.Content,
                "output":  res.Output,
                "summary": res.Summary,
        })
}

// ---- permission info endpoint ----

// handlePermissionInfo reports the APPA trajectory state for the active
// engine on the named session. Empty session id returns the global mode
// and policy summary only.
func (s *Server) handlePermissionInfo(w http.ResponseWriter, r *http.Request) {
        id := strings.TrimSpace(r.URL.Query().Get("id"))
        resp := map[string]any{
                "mode":   string(s.modeStore().mode()),
                "label":  modeLabel[s.modeStore().mode()],
                "summary": modeSummary[s.modeStore().mode()],
        }
        if id != "" {
                s.mu.Lock()
                le, ok := s.engines[id]
                s.mu.Unlock()
                if ok && le != nil && le.engine != nil {
                        gate := le.engine.Gate()
                        if sg, ok := gate.(*permission.StaticGate); ok {
                                tr := sg.Trajectory()
                                if tr != nil {
                                        resp["trajectory"] = tr.String()
                                        resp["effects"] = tr.Effects.Len()
                                        resp["rulings"] = tr.Rulings.Len()
                                        resp["acceptedNarrowing"] = len(tr.AcceptedNarrowing)
                                }
                        }
                }
        }
        writeJSON(w, http.StatusOK, resp)
}

// ---- settings: model / think / agents / memory ----

// handleGetSettings returns the live values + the catalog (model names,
// think levels, permission modes). One call lets the browser render the
// whole settings panel without round-trips.
func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
        thinkLevels := make([]string, 0, len(llm.AllThinkModes()))
        for _, m := range llm.AllThinkModes() {
                thinkLevels = append(thinkLevels, string(m))
        }
        // The active mode is the EFFECTIVE one (persona override folded
        // in), so the panel highlights what new sessions actually run —
        // not the pre-persona base the raw store still holds.
        cur := permission.Mode(s.effectivePermissionMode())
        modes := make([]modeEntry, 0, len(webPermissionModes))
        for _, m := range webPermissionModes {
                modes = append(modes, modeEntry{
                        Mode:    string(m),
                        Label:   modeLabel[m],
                        Summary: modeSummary[m],
                        Active:  m == cur,
                })
        }
        // The state block must carry the EFFECTIVE memory value (persona
        // override folded in), not the raw global knob: the toggle writes
        // the persona when it owns the setting, so the raw snapshot made
        // the toggle re-render OFF right after a flip — while the footer
        // badge and the engines ran ON. effectiveMemoryEnabled is the same
        // resolver stateView() uses, so the panel, the footer, and the
        // engine can never disagree about memory. The model carries the
        // same effective layer (knob → persona → "" default sentinel) for
        // the same reason: the panel must highlight what the persona
        // actually runs.
        snap := s.stateStore().snapshot()
        writeJSON(w, http.StatusOK, map[string]any{
                "state": map[string]any{
                        "model":         s.effectiveModelName(),
                        "think":         snap.Think,
                        "agentsEnabled": snap.AgentsEnabled,
                        "memoryEnabled": s.effectiveMemoryEnabled(snap.MemoryEnabled),
                        "personaId":     snap.PersonaID,
                },
                "models":      modelNames(s.proj),
                "thinkLevels": thinkLevels,
                "modes":       modes,
                "themes":      themeNames(),
        })
}

// handleSetModel swaps the active model. WHERE the write lands depends on
// the active persona, mirroring the memory toggle's ownership pattern: a
// persona carrying an explicit Model owns the setting (the picker edits
// the persona, persisted to the store), while a persona without one
// inherits the per-run global knob. Either way the resolved model is
// re-applied to every live engine so the next turn uses it.
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Name string `json:"name"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        name := strings.TrimSpace(body.Name)
        if name != "" {
                valid := false
                for _, n := range modelNames(s.proj) {
                        if n == name {
                                valid = true
                                break
                        }
                }
                if !valid {
                        writeStatus(w, http.StatusBadRequest, "unknown model: "+name)
                        return
                }
        }
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil && p.Model != "" {
                        // The active persona owns its model: persist the pick
                        // there instead of the per-run knob, so it survives
                        // restarts and applies to every surface.
                        if _, err := s.personaStore.SetSettings(p.ID, &name, nil, nil); err != nil {
                                writeStatus(w, http.StatusInternalServerError, err.Error())
                                return
                        }
                        s.applyPersonaToEngines()
                        s.broadcastState()
                        s.toast("", toastSuccess, "Model: "+defaultIfEmpty(name, "project default"), 2*time.Second)
                        writeJSON(w, http.StatusOK, s.stateView())
                        return
                }
        }
        s.stateStore().setModel(name)
        s.applyModelToEngines()
        s.broadcastState()
        s.toast("", toastSuccess, "Model: "+defaultIfEmpty(name, "project default"), 2*time.Second)
        writeJSON(w, http.StatusOK, s.stateView())
}

// handleSetThink sets the reasoning effort level and re-applies to engines.
func (s *Server) handleSetThink(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Level string `json:"level"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        level := llm.ThinkMode(strings.TrimSpace(body.Level))
        if level == "" {
                level = llm.Default
        }
        valid := false
        for _, m := range llm.AllThinkModes() {
                if m == level {
                        valid = true
                        break
                }
        }
        if !valid {
                writeStatus(w, http.StatusBadRequest, "unknown think level: "+string(level))
                return
        }
        s.stateStore().setThink(level)
        s.applyModelToEngines()
        s.broadcastState()
        label := string(level)
        if level == llm.Default {
                label = "default"
        }
        s.toast("", toastSuccess, "Thinking: "+label, 2*time.Second)
        writeJSON(w, http.StatusOK, s.stateView())
}

// handleSetAgents toggles sub-agent tool registration. Re-applies the model
// to engines so the system prompt picks up the new agent-tool set.
func (s *Server) handleSetAgents(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Enabled bool `json:"enabled"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        s.stateStore().setAgents(body.Enabled)
        s.applyModelToEngines()
        s.broadcastState()
        msg := "Sub-agents: off"
        if body.Enabled {
                msg = "Sub-agents: on"
        }
        s.toast("", toastSuccess, msg, 2*time.Second)
        writeJSON(w, http.StatusOK, s.stateView())
}

// handleSetMemory toggles agent memory. WHERE the toggle writes depends on
// the active persona: a persona carrying an explicit memory_enabled owns
// the setting (the toggle edits the persona, persisted to the store, so it
// both always works and always reflects reality), while a persona without
// one (or no persona store at all) inherits the global settings knob.
// Either way the engines are re-bound against the resulting effective
// state, so remember/recall tools and the auto inlet/outlet follow
// immediately.
func (s *Server) handleSetMemory(w http.ResponseWriter, r *http.Request) {
        var body struct {
                Enabled bool `json:"enabled"`
        }
        if err := decodeBody(r, &body); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        if s.personaStore != nil {
                if p, err := s.personaStore.ActivePersona(); err == nil && p.MemoryEnabled != nil {
                        // The active persona owns its memory setting: persist
                        // the toggle there instead of the global knob.
                        if _, err := s.personaStore.SetSettings(p.ID, nil, nil, &body.Enabled); err != nil {
                                writeStatus(w, http.StatusInternalServerError, err.Error())
                                return
                        }
                        s.applyPersonaToEngines()
                        s.broadcastState()
                        msg := "Agent memory: off"
                        if body.Enabled {
                                msg = "Agent memory: on"
                        }
                        s.toast("", toastSuccess, msg, 2*time.Second)
                        writeJSON(w, http.StatusOK, s.stateView())
                        return
                }
        }
        s.stateStore().setMemory(body.Enabled)
        s.applyPersonaToEngines()
        s.broadcastState()
        msg := "Agent memory: off"
        if body.Enabled {
                msg = "Agent memory: on"
        }
        s.toast("", toastSuccess, msg, 2*time.Second)
        writeJSON(w, http.StatusOK, s.stateView())
}

// applyModelToEngines re-applies the live model + think level to every
// running engine. Engines created later pick the values up in liveFor.
// The resolution folds the ACTIVE persona's Model in when the manual knob
// is untouched, so a persona model edit reaches live engines too.
func (s *Server) applyModelToEngines() {
        cfg, ok := s.effectiveModelConfig()
        if !ok {
                return
        }
        s.mu.Lock()
        engines := make([]*agent.Engine, 0, len(s.engines))
        for _, le := range s.engines {
                if le != nil && le.engine != nil {
                        engines = append(engines, le.engine)
                }
        }
        s.mu.Unlock()
        for _, e := range engines {
                e.SetModel(cfg)
        }
}

// ---- skills endpoint ----

// skillJSON is the wire shape for one discovered skill.
type skillJSON struct {
        Name        string `json:"name"`
        Description string `json:"description,omitempty"`
        Path        string `json:"path"`
}

// handleListSkills scans the configured skill path for SKILL.md files.
// Mirrors the TUI's SkillsCommands picker source.
func (s *Server) handleListSkills(w http.ResponseWriter, _ *http.Request) {
        dir := s.skillDir()
        if dir == "" {
                writeJSON(w, http.StatusOK, map[string]any{"skills": []skillJSON{}})
                return
        }
        entries, err := os.ReadDir(dir)
        if err != nil {
                writeJSON(w, http.StatusOK, map[string]any{"skills": []skillJSON{}, "error": err.Error()})
                return
        }
        out := make([]skillJSON, 0, len(entries))
        for _, e := range entries {
                if !e.IsDir() {
                        continue
                }
                skillFile := filepath.Join(dir, e.Name(), "SKILL.md")
                data, err := os.ReadFile(skillFile)
                if err != nil {
                        continue
                }
                name, desc := parseSkillFrontmatter(string(data))
                if name == "" {
                        name = e.Name()
                }
                out = append(out, skillJSON{
                        Name:        name,
                        Description: desc,
                        Path:        filepath.Join(dir, e.Name()),
                })
        }
        writeJSON(w, http.StatusOK, map[string]any{"skills": out, "dir": dir})
}

// skillDir resolves the skill directory: the project's configured skill
// path, falling back to the user-level ~/.a1/skills.
func (s *Server) skillDir() string {
        if s.proj != nil && s.proj.Config() != nil && s.proj.Config().SkillPath != "" {
                return s.proj.Config().SkillPath
        }
        home, err := os.UserHomeDir()
        if err != nil {
                return ""
        }
        return filepath.Join(home, ".a1", "skills")
}

// parseSkillFrontmatter extracts the name and description from a
// SKILL.md file's YAML frontmatter. Returns ("", "") when no frontmatter
// is found.
func parseSkillFrontmatter(content string) (name, desc string) {
        if !strings.HasPrefix(content, "---") {
                return "", ""
        }
        end := strings.Index(content[3:], "---")
        if end < 0 {
                return "", ""
        }
        block := content[3 : end+3]
        for _, line := range strings.Split(block, "\n") {
                line = strings.TrimSpace(line)
                if strings.HasPrefix(line, "name:") {
                        name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
                        name = strings.Trim(name, "\"'")
                }
                if strings.HasPrefix(line, "description:") {
                        desc = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
                        desc = strings.Trim(desc, "\"'")
                }
        }
        return name, desc
}

// ---- extensions endpoint ----

// extJSON is the wire shape for one discovered extension.
type extJSON struct {
        ID          string `json:"id"`
        Name        string `json:"name"`
        Version     string `json:"version,omitempty"`
        Description string `json:"description,omitempty"`
        Path        string `json:"path"`
        Source      string `json:"source,omitempty"`
}

// handleListExtensions returns the discovered extensions and any warnings.
// The web surface does not have a controller holding an extension runner,
// so this scans the filesystem directly (matching extension.Discover()).
func (s *Server) handleListExtensions(w http.ResponseWriter, _ *http.Request) {
        userDir, projectDir := s.extensionDirs()
        discovered, warns, err := extension.Discover(userDir, projectDir)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, err.Error())
                return
        }
        out := make([]extJSON, 0, len(discovered))
        for _, d := range discovered {
                out = append(out, extJSON{
                        ID:          d.ID,
                        Name:        d.Manifest.Name,
                        Version:     d.Manifest.Version,
                        Description: d.Manifest.Description,
                        Path:        d.Path,
                        Source:      d.Source,
                })
        }
        type warnJSON struct {
                Path    string `json:"path,omitempty"`
                Message string `json:"message"`
        }
        warnsOut := make([]warnJSON, 0, len(warns))
        for _, w := range warns {
                warnsOut = append(warnsOut, warnJSON{
                        Path:    w.Path,
                        Message: w.Message,
                })
        }
        writeJSON(w, http.StatusOK, map[string]any{
                "extensions": out,
                "warnings":   warnsOut,
        })
}

// extensionDirs returns the (userDir, projectDir) the Discover scan uses.
// Both may be empty.
func (s *Server) extensionDirs() (userDir, projectDir string) {
        home, err := os.UserHomeDir()
        if err == nil {
                userDir = filepath.Join(home, ".a1", "extensions")
        }
        if s.proj != nil && s.proj.Root() != "" {
                projectDir = filepath.Join(s.proj.Root(), ".a1", "extensions")
        }
        return userDir, projectDir
}

// ---- helpers ----

// themeNames returns the available TUI themes for the settings panel. The
// web renders themes via CSS, so the names here are advisory: the browser
// applies one of a small set of pre-shipped palettes.
func themeNames() []string {
        return []string{"paper", "darcula", "ink"}
}

// defaultIfEmpty returns fallback when s is "".
func defaultIfEmpty(s, fallback string) string {
        if s == "" {
                return fallback
        }
        return s
}

// runBashCommand was a placeholder for a future "!cmd" composer prefix that
// would shell out from the input bar (mirroring the TUI's BashRunner). It was
// never wired to a handler — the InputBar's shell-mode badge is purely visual,
// and `!cmd` text is posted to /api/prompt unchanged. Removed to stop the
// "kept for the upcoming" comment from accumulating indefinitely; revisit
// when the composer path is real.
