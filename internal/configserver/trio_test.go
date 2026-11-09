package configserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

// decodeErrBody pulls the {"error": ...} envelope writeConfigErr emits.
func decodeErrBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body), "body: %s", rec.Body.String())
	return body.Error
}

// localRequest builds a request with the loopback Host the origin checks need.
func localRequest(method, target string, body []byte) *http.Request {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, rdr)
	req.Host = "127.0.0.1:8080"
	return req
}

func TestUnknownRoute_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/nope", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestNonLoopbackHostRejected_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}

	// httptest's default Host is example.com — off-box, must be refused before
	// any handler logic runs.
	for _, target := range []string{"/api/config", "/api/stats"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		require.Equal(t, http.StatusForbidden, rec.Code, "GET %s", target)
		assert.Equal(t, "request origin is not allowed", decodeErrBody(t, rec))
	}

	// /api/models is POST-gated first; a non-loopback POST must also 403.
	req := httptest.NewRequest(http.MethodPost, "/api/models", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "request origin is not allowed", decodeErrBody(t, rec))
}

func TestMethodNotAllowed_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodDelete, "/api/config", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "method not allowed\n", rec.Body.String(), "plain http.Error body")

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodGet, "/api/models", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodPost, "/api/stats", nil))
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestConfigGetUnreadableFile_Negative(t *testing.T) {
	// Corrupt YAML must surface as a 500 JSON error, not a panic.
	path := writeTestConfig(t, "models: [unclosed")
	h := &Handler{ConfigPath: path}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodGet, "/api/config", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, decodeErrBody(t, rec), "yaml:", "the YAML error reaches the client")

	// A directory as the config path is a read error, too.
	h = &Handler{ConfigPath: t.TempDir()}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, localRequest(http.MethodGet, "/api/config", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, decodeErrBody(t, rec), "is a directory")
}

func TestConfigPostMalformedJSON_Negative(t *testing.T) {
	h := &Handler{ConfigPath: writeTestConfig(t, "models:\n  - name: m\n    api_key: k\n")}

	req := localRequest(http.MethodPost, "/api/config", []byte("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeErrBody(t, rec), "bad request:")

	// Valid JSON with a wrong-typed field decodes just as badly.
	req = localRequest(http.MethodPost, "/api/config", []byte(`{"models": "not-an-array"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestConfigPostWrongContentType_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}
	req := localRequest(http.MethodPost, "/api/config", []byte(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code)
	assert.Equal(t, "content type must be application/json", decodeErrBody(t, rec))
}

func TestConfigPostOriginChecks_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}

	cases := []struct {
		name   string
		origin string
	}{
		{"missing origin", ""},
		{"foreign host", "http://evil.example.com"},
		{"origin with path", "http://127.0.0.1:8080/x"},
		{"scheme mismatch", "https://127.0.0.1:8080"},
		{"userinfo smuggled", "http://user@127.0.0.1:8080"},
	}
	for _, tc := range cases {
		req := localRequest(http.MethodPost, "/api/config", []byte(`{}`))
		req.Header.Set("Content-Type", "application/json")
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusForbidden, rec.Code, "%s: status", tc.name)
		assert.Equal(t, "request origin is not allowed", decodeErrBody(t, rec), "%s: body", tc.name)
	}
}

func TestConfigPostValidation_Negative(t *testing.T) {
	original := "models:\n  - name: m\n    api_key: k\n    default: true\n"
	path := writeTestConfig(t, original)

	cases := []struct {
		name    string
		payload map[string]any
		wantErr string
	}{
		{
			name:    "no models at all",
			payload: map[string]any{"models": []map[string]any{}},
			wantErr: "at least one model is required",
		},
		{
			name: "two defaults",
			payload: map[string]any{"models": []map[string]any{
				{"name": "a", "apiKey": "k1", "default": true},
				{"name": "b", "apiKey": "k2", "default": true},
			}},
			wantErr: "only one model may be marked default",
		},
		{
			name: "unnamed model",
			payload: map[string]any{"models": []map[string]any{
				{"name": "", "apiKey": "k"},
			}},
			wantErr: "model 1 has no name",
		},
		{
			name: "flagged default missing api key",
			payload: map[string]any{"models": []map[string]any{
				{"name": "d", "apiKey": "", "default": true},
			}},
			wantErr: `default model "d" is missing api_key`,
		},
		{
			name: "auto-picked default missing api key",
			payload: map[string]any{"models": []map[string]any{
				{"name": "first", "apiKey": ""},
			}},
			wantErr: `default model "first" is missing api_key`,
		},
	}
	for _, tc := range cases {
		rec := postConfig(t, &Handler{ConfigPath: path}, tc.payload)
		require.Equal(t, http.StatusBadRequest, rec.Code, "%s: status (body %s)", tc.name, rec.Body.String())
		assert.Contains(t, decodeErrBody(t, rec), tc.wantErr, "%s: error", tc.name)

		// A rejected save must leave the file untouched and never write a backup.
		data, err := os.ReadFile(path)
		require.NoError(t, err, "%s", tc.name)
		assert.Equal(t, original, string(data), "%s: config.yaml unchanged", tc.name)
		_, err = os.Stat(path + ".bak")
		assert.True(t, os.IsNotExist(err), "%s: no .bak on rejected save", tc.name)
	}
}

// postModels drives one /api/models call with the local-origin headers the
// endpoint requires.
func postModels(t *testing.T, h *Handler, payload string) *httptest.ResponseRecorder {
	t.Helper()
	req := localRequest(http.MethodPost, "/api/models", []byte(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:8080")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestModelsPostBadInput_Negative(t *testing.T) {
	h := &Handler{ConfigPath: "unused.yaml"}

	rec := postModels(t, h, "{not json")
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, decodeErrBody(t, rec), "bad request:")

	// Not a URL at all.
	rec = postModels(t, h, `{"baseUrl": "not a url"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "base URL must be an absolute HTTP(S) URL", decodeErrBody(t, rec))

	// A URL, but the wrong scheme.
	rec = postModels(t, h, `{"baseUrl": "ftp://example.com"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "base URL must use http or https", decodeErrBody(t, rec))
}

// TestModelsUpstreamFailures_Negative points /api/models at a local httptest
// upstream and pins how its failures map onto config-server status codes.
func TestModelsUpstreamFailures_Negative(t *testing.T) {
	cases := []struct {
		name     string
		upstream func(w http.ResponseWriter)
		wantCode int
		wantErr  string
	}{
		{
			name: "upstream 404",
			upstream: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("no such provider"))
			},
			wantCode: http.StatusBadGateway,
			wantErr:  "model list request failed: no such provider",
		},
		{
			name: "upstream 500 empty body falls back to status text",
			upstream: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantCode: http.StatusBadGateway,
			wantErr:  "model list request failed: 500 Internal Server Error",
		},
		{
			name: "rate limited",
			upstream: func(w http.ResponseWriter) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
			},
			wantCode: http.StatusBadGateway,
			wantErr:  "rate limit",
		},
		{
			name: "200 with malformed json",
			upstream: func(w http.ResponseWriter) {
				_, _ = w.Write([]byte("<<not json>>"))
			},
			wantCode: http.StatusBadGateway,
			wantErr:  "decode model list:",
		},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			tc.upstream(w)
		}))
		h := &Handler{ConfigPath: "unused.yaml"}

		rec := postModels(t, h, `{"baseUrl": `+jsonString(t, srv.URL)+`}`)
		require.Equal(t, tc.wantCode, rec.Code, "%s: status (body %s)", tc.name, rec.Body.String())
		assert.Contains(t, decodeErrBody(t, rec), tc.wantErr, "%s: error", tc.name)

		srv.Close()
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}
