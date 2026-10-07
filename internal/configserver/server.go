package configserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/damien1141/a1/internal/telemetry"
	"github.com/damien1141/a1/internal/util"
)

// ConfigDoc is the JSON/YAML representation of the config editor document.
type ConfigDoc struct {
	Path           string             `yaml:"-"                         json:"path,omitempty"`
	Models         []ModelDoc         `yaml:"models"                    json:"models"`
	SkillPath      *string            `yaml:"skill_path,omitempty"      json:"skillPath,omitempty"`
	Permissions    *PermDoc           `yaml:"permissions,omitempty"     json:"permissions,omitempty"`
	Agents         *AgentsDoc         `yaml:"agents,omitempty"          json:"agents,omitempty"`
	SemanticSearch *SemanticSearchDoc `yaml:"semantic_search,omitempty" json:"semanticSearch,omitempty"`
	Judge          *JudgeDoc          `yaml:"judge,omitempty"           json:"judge,omitempty"`
	Browser        *BrowserDoc        `yaml:"browser,omitempty"         json:"browser,omitempty"`
}

// ModelDoc describes a single model entry.
type ModelDoc struct {
	Name          string `yaml:"name"                     json:"name"`
	APIKey        string `yaml:"api_key"                  json:"apiKey"`
	BaseURL       string `yaml:"base_url"                 json:"baseUrl"`
	ContextWindow *int   `yaml:"context_window,omitempty" json:"contextWindow,omitempty"`
	ImageEnabled  *bool  `yaml:"image_enabled,omitempty"  json:"imageEnabled,omitempty"`
	API           string `yaml:"api,omitempty"           json:"api,omitempty"`
	ThinkEnabled  *bool  `yaml:"think_enabled,omitempty" json:"thinkEnabled,omitempty"`
	ThinkLevel    string `yaml:"think_level,omitempty"   json:"thinkLevel,omitempty"`
	Default       bool   `yaml:"default,omitempty"       json:"default,omitempty"`
}

// PermDoc describes permissions settings.
type PermDoc struct {
	Mode                *string  `yaml:"mode,omitempty"                  json:"mode,omitempty"`
	WorkspaceOnlyWrites *bool    `yaml:"workspace_only_writes,omitempty" json:"workspaceOnlyWrites,omitempty"`
	AskTimeoutSec       *int     `yaml:"ask_timeout_sec,omitempty"       json:"askTimeoutSec,omitempty"`
	DangerouslyAllowAll *bool    `yaml:"dangerously_allow_all,omitempty" json:"dangerouslyAllowAll,omitempty"`
	Bash                *BashDoc `yaml:"bash,omitempty"                  json:"bash,omitempty"`
}

// BashDoc describes bash permission settings.
type BashDoc struct {
	Default *string  `yaml:"default,omitempty" json:"default,omitempty"`
	Allow   []string `yaml:"allow"             json:"allow,omitempty"`
	Deny    []string `yaml:"deny"              json:"deny,omitempty"`
}

// AgentsDoc describes agent settings.
type AgentsDoc struct {
	Enabled *bool            `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Models  *AgentsModelsDoc `yaml:"models,omitempty"  json:"models,omitempty"`
}

// AgentsModelsDoc describes model assignments for agent roles.
type AgentsModelsDoc struct {
	Explore string `yaml:"explore" json:"explore,omitempty"`
	Review  string `yaml:"review"  json:"review,omitempty"`
	Worker  string `yaml:"worker"  json:"worker,omitempty"`
}

// SemanticSearchDoc describes semantic search settings.
type SemanticSearchDoc struct {
	Enabled        bool   `yaml:"enabled"         json:"enabled"`
	OllamaBaseURL  string `yaml:"ollama_base_url" json:"ollamaBaseUrl"`
	EmbeddingModel string `yaml:"embedding_model" json:"embeddingModel"`
}

// JudgeDoc describes judge settings.
type JudgeDoc struct {
	Model         string `yaml:"model"           json:"model"`
	OllamaBaseURL string `yaml:"ollama_base_url" json:"ollamaBaseUrl"`
}

// BrowserDoc describes browser settings.
type BrowserDoc struct {
	Profiles []string `yaml:"profiles" json:"profiles"`
}

// ModelListRequest is the request body for /api/models.
type ModelListRequest struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	API     string `json:"api"`
}

type modelListItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

type modelListResponse struct {
	Data   []modelListItem `json:"data"`
	Models []modelListItem `json:"models"`
}

const (
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	KiloGatewayBaseURL      = "https://api.kilo.ai/api/gateway"
	anthropicAPIVersion     = "2023-06-01"
	modelListRequestLimit   = 15 * time.Second
	modelListBodyLimit      = int64(4 << 20)
)

// Handler bundles the config file path with shared config-server helpers.
type Handler struct {
	ConfigPath string
	configHTML []byte
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (r.URL.Path == "/api/config" || r.URL.Path == "/api/models" || r.URL.Path == "/api/stats") &&
		!isLoopbackHost(r.Host) {
		writeConfigErr(w, http.StatusForbidden, errors.New("request origin is not allowed"))
		return
	}

	switch r.URL.Path {
	case "/":
		html, err := h.loadHTML()
		if err != nil {
			writeConfigErr(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(html)
	case "/api/config":
		h.handleConfig(w, r)
	case "/api/models":
		h.handleModels(w, r)
	case "/api/stats":
		h.handleStats(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) loadHTML() ([]byte, error) {
	if len(h.configHTML) > 0 {
		return h.configHTML, nil
	}
	path, err := locateConfigHTML()
	if err != nil {
		return nil, err
	}
	html, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config html: %w", err)
	}
	h.configHTML = html
	return html, nil
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		doc, err := readConfigDoc(h.ConfigPath)
		if err != nil {
			writeConfigErr(w, http.StatusInternalServerError, err)
			return
		}
		doc.Path = h.ConfigPath
		writeConfigJSON(w, doc)
	case http.MethodPost:
		if status, err := validateLocalJSONRequest(r); err != nil {
			writeConfigErr(w, status, err)
			return
		}
		var doc ConfigDoc
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			writeConfigErr(w, http.StatusBadRequest, fmt.Errorf("bad request: %w", err))
			return
		}
		if err := validateConfigDoc(&doc); err != nil {
			writeConfigErr(w, http.StatusBadRequest, err)
			return
		}
		if err := writeConfigDoc(h.ConfigPath, &doc); err != nil {
			writeConfigErr(w, http.StatusInternalServerError, err)
			return
		}
		writeConfigJSON(w, map[string]string{"status": "saved"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleModels fetches model IDs through the local config server so the page
// does not need cross-origin access to a provider API.
func (*Handler) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if status, err := validateLocalJSONRequest(r); err != nil {
		writeConfigErr(w, status, err)
		return
	}

	var input ModelListRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeConfigErr(w, http.StatusBadRequest, fmt.Errorf("bad request: %w", err))
		return
	}

	baseURL := strings.TrimSpace(input.BaseURL)
	apiKey := strings.TrimSpace(input.APIKey)
	anthropic := isAnthropicAPI(input.API, baseURL, input.Model)
	if baseURL == "" {
		if anthropic {
			baseURL = "https://api.anthropic.com"
		} else {
			baseURL = defaultOpenAIBaseURL
		}
	}
	endpoint, err := modelListEndpoint(baseURL, anthropic)
	if err != nil {
		writeConfigErr(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), modelListRequestLimit)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		writeConfigErr(w, http.StatusBadRequest, fmt.Errorf("build model list request: %w", err))
		return
	}
	request.Header.Set("Accept", "application/json")
	if anthropic {
		request.Header.Set("X-Api-Key", apiKey)
		request.Header.Set("Anthropic-Version", anthropicAPIVersion)
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}

	response, err := modelListHTTPClient().Do(request)
	if err != nil {
		writeConfigErr(w, http.StatusBadGateway, fmt.Errorf("fetch model list: %w", err))
		return
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, modelListBodyLimit+1))
	if err != nil {
		writeConfigErr(w, http.StatusBadGateway, fmt.Errorf("read model list: %w", err))
		return
	}
	if int64(len(body)) > modelListBodyLimit {
		writeConfigErr(w, http.StatusBadGateway, errors.New("model list response is too large"))
		return
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(body))
		if len(message) > 500 {
			message = message[:500]
		}
		if message == "" {
			message = response.Status
		}
		writeConfigErr(w, http.StatusBadGateway, fmt.Errorf("model list request failed: %s", message))
		return
	}

	var payload modelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		writeConfigErr(w, http.StatusBadGateway, fmt.Errorf("decode model list: %w", err))
		return
	}
	models := collectModelIDs(append(payload.Data, payload.Models...))
	sort.Strings(models)
	writeConfigJSON(w, struct {
		Models []string `json:"models"`
	}{Models: models})
}

func (*Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !isLoopbackHost(r.Host) {
		writeConfigErr(w, http.StatusForbidden, errors.New("request origin is not allowed"))
		return
	}

	rec := telemetry.GlobalRecorder()
	snapshot := telemetry.Snapshot{}
	if rec != nil {
		snapshot = rec.Snapshot()
	}
	writeConfigJSON(w, snapshot)
}

func modelListHTTPClient() *http.Client {
	client := *util.DefaultHTTPClient()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		origin := via[0].URL
		if !strings.EqualFold(req.URL.Scheme, origin.Scheme) ||
			!strings.EqualFold(req.URL.Host, origin.Host) {
			return errors.New("model list redirect changed origin")
		}
		return nil
	}
	return &client
}

func isAnthropicAPI(api, baseURL, model string) bool {
	switch strings.TrimSpace(api) {
	case "Anthropic":
		return true
	case "OpenAI", "OpenAIResponses", "Gemini":
		return false
	}
	return strings.Contains(strings.ToLower(baseURL), "anthropic") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude")
}

func modelListEndpoint(baseURL string, anthropic bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("base URL must be an absolute HTTP(S) URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("base URL must use http or https")
	}

	path := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(path, "/models") {
		if anthropic && !strings.HasSuffix(path, "/v1") {
			path += "/v1"
		}
		path += "/models"
	}
	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func collectModelIDs(items []modelListItem) []string {
	seen := make(map[string]struct{}, len(items))
	models := make([]string, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = strings.TrimSpace(item.Name)
		}
		if id == "" {
			id = strings.TrimSpace(item.DisplayName)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	return models
}

func validateLocalJSONRequest(r *http.Request) (int, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return http.StatusUnsupportedMediaType, errors.New("content type must be application/json")
	}
	origin := r.Header.Get("Origin")
	originURL, err := url.Parse(origin)
	if err != nil || origin == "" || originURL.Scheme == "" || originURL.Host == "" ||
		originURL.User != nil || originURL.Path != "" || originURL.RawQuery != "" || originURL.Fragment != "" {
		return http.StatusForbidden, errors.New("request origin is not allowed")
	}

	expectedScheme := "http"
	if r.TLS != nil {
		expectedScheme = "https"
	}
	if !strings.EqualFold(originURL.Scheme, expectedScheme) || !strings.EqualFold(originURL.Host, r.Host) {
		return http.StatusForbidden, errors.New("request origin is not allowed")
	}
	return 0, nil
}

func isLoopbackHost(rawHost string) bool {
	u, err := url.Parse("http://" + rawHost)
	if err != nil || u.Host != rawHost || u.User != nil || u.Path != "" {
		return false
	}
	hostname := u.Hostname()
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func readConfigDoc(path string) (*ConfigDoc, error) {
	doc := &ConfigDoc{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func validateConfigDoc(doc *ConfigDoc) error {
	if len(doc.Models) == 0 {
		return errors.New("at least one model is required")
	}
	hasDefault := false
	for i := range doc.Models {
		m := &doc.Models[i]
		if m.Name == "" {
			return fmt.Errorf("model %d has no name", i+1)
		}
		if !m.Default {
			continue
		}
		if hasDefault {
			return errors.New("only one model may be marked default")
		}
		hasDefault = true
		if m.APIKey == "" {
			return fmt.Errorf("default model %q is missing api_key", m.Name)
		}
	}
	if !hasDefault {
		doc.Models[0].Default = true
		if doc.Models[0].APIKey == "" {
			return fmt.Errorf("default model %q is missing api_key", doc.Models[0].Name)
		}
	}
	return nil
}

func writeConfigDoc(path string, doc *ConfigDoc) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(path); err == nil {
		//nolint:gosec // G306: config backup stays user-readable
		if err := os.WriteFile(path+".bak", cur, 0o644); err != nil {
			return fmt.Errorf("backup config: %w", err)
		}
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // G306: config.yaml is meant to be user-readable
}

func writeConfigJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeConfigErr(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// Start launches the config editor webserver and returns the listener address
// plus a stop function. The caller owns the goroutine lifetime.
func Start(ctx context.Context, configPath string) (string, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	htmlPath, err := locateConfigHTML()
	if err != nil {
		return "", nil, err
	}
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		return "", nil, fmt.Errorf("read config html: %w", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	addr := ln.Addr().(*net.TCPAddr)
	pageURL := fmt.Sprintf("http://127.0.0.1:%d/", addr.Port)

	srv := &http.Server{
		Handler:           &Handler{ConfigPath: configPath, configHTML: html},
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	stopped := make(chan struct{})
	stop := func() {
		_ = srv.Close()
		<-stopped
	}

	go func() {
		defer close(stopped)
		select {
		case err := <-errc:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Fprintf(os.Stderr, "config server error: %v\n", err)
			}
		case <-ctx.Done():
			_ = srv.Shutdown(context.Background())
		}
	}()

	return pageURL, stop, nil
}

func locateConfigHTML() (string, error) {
	start, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	dir := start
	for {
		for _, rel := range []string{"config.html", filepath.Join("internal", "configserver", "config.html")} {
			candidate := filepath.Join(dir, rel)
			if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("config.html not found from %s", start)
}

// OpenBrowser best-effort opens url in the default browser.
func OpenBrowser(ctx context.Context, url string) {
	if ctx == nil {
		ctx = context.Background()
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}
