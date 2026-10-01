package fetchtool

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchTool_Definition(t *testing.T) {
	tool := FetchTool()
	assert.Equal(t, "fetch", tool.Definition.Name)
	assert.Contains(t, tool.Definition.Description, "HTTP")
	assert.True(t, tool.Definition.Readable)
	assert.NotNil(t, tool.Run)
}

func TestRunFetch_RequiresURL(t *testing.T) {
	raw, _ := json.Marshal(fetchInput{})
	out, err := runFetch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url is required")
	assert.Empty(t, out.Content)
}

func TestRunFetch_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "hello world")
	assert.Contains(t, out.Content, "200 OK")
}

func TestRunFetch_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte("posted"))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL, Method: "POST", Body: `{"x":1}`})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "posted")
}

func TestRunFetch_InvalidMethod(t *testing.T) {
	raw, _ := json.Marshal(fetchInput{URL: "http://example.com", Method: "DELETE"})
	out, err := runFetch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported method")
	assert.Empty(t, out.Content)
}

func TestRunFetch_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") == "1" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL, Headers: `{"X-Custom":"1"}`})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "200 OK")
}

func TestRunFetch_InvalidHeadersJSON(t *testing.T) {
	raw, _ := json.Marshal(fetchInput{URL: "http://example.com", Headers: `{bad`})
	out, err := runFetch(t.Context(), raw)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid headers JSON")
	assert.Empty(t, out.Content)
}

func TestIsTextContent(t *testing.T) {
	assert.True(t, isTextContent("text/html"))
	assert.True(t, isTextContent("application/json"))
	assert.False(t, isTextContent("image/png"))
}

func TestRunFetch_HTMLToText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><h1>Hello</h1><p>World</p></body></html>`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "Hello")
	assert.Contains(t, out.Content, "World")
	assert.NotContains(t, out.Content, "<html>")
	assert.NotContains(t, out.Content, "<body>")
	assert.NotContains(t, out.Content, "<h1>")
	assert.NotContains(t, out.Content, "<p>")
}

func TestRunFetch_HTMLRaw(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><h1>Hello</h1></body></html>`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL, Raw: true})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "<html>")
	assert.Contains(t, out.Content, "<h1>")
	assert.Contains(t, out.Content, "Hello")
}

func TestHtmlToText(t *testing.T) {
	html := `<html><body><h1>Title</h1><p>Para 1</p><p>Para 2</p></body></html>`
	text := htmlToText(html)
	assert.Contains(t, text, "Title")
	assert.Contains(t, text, "Para 1")
	assert.Contains(t, text, "Para 2")
	assert.NotContains(t, text, "<html>")
	assert.NotContains(t, text, "<body>")
	assert.NotContains(t, text, "<h1>")
	assert.NotContains(t, text, "<p>")
}

func TestHtmlToText_Entities(t *testing.T) {
	html := `<p>Hello&nbsp;World&amp;&lt;test&gt;</p>`
	text := htmlToText(html)
	assert.Contains(t, text, "Hello World")
	assert.Contains(t, text, "<test>")
}

func TestHtmlToText_Truncation(t *testing.T) {
	html := `<p>` + strings.Repeat("a", 10000) + `</p>`
	text := htmlToText(html)
	assert.Contains(t, text, "[truncated]")
	assert.Less(t, len(text), 10000)
}

func TestRunFetch_Links(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body>
  <a href="https://example.com/">home</a>
  <a href="https://example.com/about">about</a>
  <a href="/docs">docs (relative)</a>
  <a href="https://api.example.com/v1">api</a>
  <a href="https://cdn.example.com/x">cdn</a>
  <a href="mailto:foo@example.com">mailto</a>
  <a href="javascript:void(0)">js</a>
  <a href="https://example.com/about">dup</a>
</body></html>`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL, Links: true})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.Contains(t, out.Content, "## Links")
	assert.Contains(t, out.Content, "example.com")
	assert.Contains(t, out.Content, "/about")
	assert.Contains(t, out.Content, "/docs")
	assert.Contains(t, out.Content, "api.example.com")
	assert.Contains(t, out.Content, "cdn.example.com")
	assert.NotContains(t, out.Content, "mailto:")
	assert.NotContains(t, out.Content, "javascript:")
}

func TestRunFetch_NoLinksWhenDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><a href="https://example.com/">x</a></body></html>`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(fetchInput{URL: server.URL})
	out, err := runFetch(t.Context(), raw)
	require.NoError(t, err)
	assert.NotContains(t, out.Content, "## Links")
}

func TestExtractLinks(t *testing.T) {
	html := `<a href="https://example.com/">home</a>
<a href="https://example.com/about">about</a>
<a href="/docs">docs</a>
<a href="https://api.example.com/v1">api</a>
<a href="mailto:foo@example.com">mail</a>`
	links := extractLinks(html, "https://example.com/")
	require.Len(t, links, 4)
	// Sorted by host: api.example.com first, then example.com.
	assert.Equal(t, "api.example.com", links[0].Host)
	assert.Equal(t, "/v1", links[0].Path)
	assert.Equal(t, "example.com", links[1].Host)
}

func TestFormatLinks(t *testing.T) {
	links := []linkEntry{
		{Host: "api.example.com", Path: "/v1"},
		{Host: "example.com", Path: "/"},
		{Host: "example.com", Path: "/about"},
	}
	out := formatLinks(links)
	assert.Contains(t, out, "api.example.com")
	assert.Contains(t, out, "example.com")
	assert.Contains(t, out, "/v1")
	assert.Contains(t, out, "/about")
	assert.Contains(t, out, "3 links across 2 hosts")
}
