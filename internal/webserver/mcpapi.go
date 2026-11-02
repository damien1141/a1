package webserver

import (
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/damien1141/a1/internal/mcp"
)

// mcpapi.go — the MCP server registry for the config editor: the same
// ~/.a1/mcp.json (+ project .a1/mcp.json) the `a1 mcp` CLI manages, over
// HTTP so the web settings page can list, add, and remove servers.
//
// Writes only touch the USER file (AddServer/RemoveServer semantics): a
// server defined by the project's checked-in .a1/mcp.json is shown with
// source "project" and cannot be edited here — it belongs to the repo, not
// the machine, and silently rewriting project config from a local GUI would
// surprise every other checkout.

// mcpServerDTO is one registry row.
type mcpServerDTO struct {
	Name string `json:"name"`
	// Source is where the server is defined: "user" (~/.a1/mcp.json) or
	// "project" (<root>/.a1/mcp.json; project wins on name clashes).
	Source string `json:"source"`
	// Editable marks servers this API can change (user file only).
	Editable bool              `json:"editable"`
	Config   mcp.ServerConfig  `json:"config"`
}

// handleListMCP serves the merged registry.
func (s *Server) handleListMCP(w http.ResponseWriter, _ *http.Request) {
	if !s.requireProject(w) {
		return
	}
	s.configMux.Lock()
	merged, err := mcp.Load(s.proj.MCPConfigFile())
	s.configMux.Unlock()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "load mcp config: "+err.Error())
		return
	}

	// Source attribution needs the user file's own keys; mcp.Load merges.
	userServers := readMCPServerNames(w)
	if userServers == nil {
		return
	}

	out := make([]mcpServerDTO, 0, len(merged))
	for name, cfg := range merged {
		_, inUser := userServers[name]
		source := "project"
		if inUser {
			source = "user"
		}
		out = append(out, mcpServerDTO{
			Name:     name,
			Source:   source,
			Editable: inUser,
			Config:   cfg,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	userPath, _ := mcp.UserConfigPath()
	writeJSON(w, http.StatusOK, map[string]any{
		"servers":     out,
		"userPath":    userPath,
		"projectPath": s.proj.MCPConfigFile(),
	})
}

// readMCPServerNames parses the user mcp.json's server names, writing the
// error response itself and returning nil on failure.
func readMCPServerNames(w http.ResponseWriter) map[string]struct{} {
	names := map[string]struct{}{}
	path, err := mcp.UserConfigPath()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "mcp user path: "+err.Error())
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return names
		}
		writeStatus(w, http.StatusInternalServerError, "read mcp config: "+err.Error())
		return nil
	}
	var doc struct {
		Servers map[string]json.RawMessage `json:"servers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		// A corrupt user file is reported: the editor must not silently
		// show a registry the CLI would fail to load too.
		writeStatus(w, http.StatusInternalServerError, "parse mcp config: "+err.Error())
		return nil
	}
	for name := range doc.Servers {
		names[name] = struct{}{}
	}
	return names
}

// mcpPutRequest is the PUT /api/mcp body: upsert one server by name.
type mcpPutRequest struct {
	Name   string            `json:"name"`
	Server mcp.ServerConfig `json:"server"`
}

// handlePutMCP upserts one server in the user config.
func (s *Server) handlePutMCP(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	var req mcpPutRequest
	if err := decodeBody(r, &req); err != nil {
		writeStatus(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeStatus(w, http.StatusBadRequest, "server name is required")
		return
	}
	if strings.ContainsAny(req.Name, "/\\\"\x00") || strings.HasPrefix(req.Name, ".") {
		writeStatus(w, http.StatusBadRequest, "server name must be a plain identifier")
		return
	}
	// Shape check per transport: stdio needs a command, http needs a URL.
	cfg := req.Server
	if cfg.IsHTTP() {
		if strings.TrimSpace(cfg.URL) == "" {
			writeStatus(w, http.StatusBadRequest, "http transport needs a url")
			return
		}
		cfg.Transport = "http"
	} else {
		if len(cfg.Command) == 0 {
			writeStatus(w, http.StatusBadRequest, "stdio transport needs a command")
			return
		}
		cfg.Transport = "stdio"
	}

	s.configMux.Lock()
	err := mcp.AddServer(req.Name, cfg)
	s.configMux.Unlock()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "save mcp config: "+err.Error())
		return
	}
	// Servers are picked up when engines are built; a note rides the toast
	// channel so the user knows a restart of running sessions applies it.
	s.events().publish("", serverEvent{
		Kind:  eventToast,
		Toast: &toastPayload{Level: toastSuccess, Message: "MCP server saved: " + req.Name + " — new sessions pick it up"},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved", "name": req.Name})
}

// handleDeleteMCP removes a server from the user config. Project-defined
// servers are refused with a pointer at the file that owns them.
func (s *Server) handleDeleteMCP(w http.ResponseWriter, r *http.Request) {
	if !s.requireProject(w) {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeStatus(w, http.StatusBadRequest, "server name is required")
		return
	}
	s.configMux.Lock()
	removed, err := mcp.RemoveServer(name)
	s.configMux.Unlock()
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "remove mcp server: "+err.Error())
		return
	}
	if !removed {
		writeStatus(w, http.StatusBadRequest, "not in the user config — project-defined servers are managed in <root>/.a1/mcp.json")
		return
	}
	s.events().publish("", serverEvent{
		Kind:  eventToast,
		Toast: &toastPayload{Level: toastSuccess, Message: "MCP server removed: " + name},
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}
