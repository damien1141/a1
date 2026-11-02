package fetchtool

import (
        "encoding/json"
        "fmt"
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

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Headers: json.RawMessage(`{"X-Custom":"1"}`)})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "200 OK")
}

func TestRunFetch_InvalidHeadersJSON(t *testing.T) {
        // Legacy string form whose inner JSON is malformed: a JSON string
        // containing the literal text "{bad". parseHeadersInput extracts the
        // string, tries to re-parse it as a JSON object, and fails with
        // "invalid headers JSON".
        raw, _ := json.Marshal(fetchInput{URL: "http://example.com", Headers: json.RawMessage(`"{bad"`)})
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
        text := htmlToText(html, 0)
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
        text := htmlToText(html, 0)
        assert.Contains(t, text, "Hello World")
        assert.Contains(t, text, "<test>")
}

func TestHtmlToText_Truncation(t *testing.T) {
        html := `<p>` + strings.Repeat("a", 10000) + `</p>`
        text := htmlToText(html, 0)
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

// === parseOptions ===

func TestParseOptions_Defaults(t *testing.T) {
        opts, err := parseOptions(fetchInput{})
        require.NoError(t, err)
        assert.Equal(t, "GET", opts.method)
        assert.Empty(t, opts.headers)
        assert.Empty(t, opts.body)
        assert.False(t, opts.raw)
        assert.False(t, opts.links)
        assert.False(t, opts.metadata)
        assert.False(t, opts.outline)
        assert.Equal(t, "follow", opts.redirects)
        assert.Equal(t, 0, opts.maxChars)
}

func TestParseOptions_POST(t *testing.T) {
        opts, err := parseOptions(fetchInput{Method: "post"})
        require.NoError(t, err)
        assert.Equal(t, "POST", opts.method)
}

func TestParseOptions_Headers(t *testing.T) {
        opts, err := parseOptions(fetchInput{Headers: json.RawMessage(`{"X-Custom":"1","Accept":"text/html"}`)})
        require.NoError(t, err)
        assert.Equal(t, "1", opts.headers["X-Custom"])
        assert.Equal(t, "text/html", opts.headers["Accept"])
}

// === Metadata extraction ===

func TestExtractMetadata(t *testing.T) {
        html := `<html><head>
<title>Example Page</title>
<meta name="description" content="An example page for tests">
<meta property="og:title" content="OG Title Override">
<meta property="og:description" content="OG description">
<meta property="og:site_name" content="Example Site">
<meta property="og:type" content="article">
<meta property="article:published_time" content="2024-01-15T10:30:00Z">
<meta name="author" content="Jane Doe">
<link rel="canonical" href="https://example.com/canonical-page">
</head><body>content</body></html>`
        m := extractMetadata(html, "https://example.com/page")
        assert.Equal(t, "Example Page", m.Title)
        assert.Equal(t, "An example page for tests", m.Description)
        assert.Equal(t, "OG Title Override", m.OGTitle)
        assert.Equal(t, "OG description", m.OGDescription)
        assert.Equal(t, "Example Site", m.SiteName)
        assert.Equal(t, "article", m.PageType)
        assert.Equal(t, "2024-01-15T10:30:00Z", m.PublishedTime)
        assert.Equal(t, "Jane Doe", m.Author)
        assert.Equal(t, "https://example.com/canonical-page", m.Canonical)
}

func TestExtractMetadata_Empty(t *testing.T) {
        m := extractMetadata("", "https://example.com/")
        assert.Empty(t, m.Title)
        assert.Empty(t, m.Description)
        assert.Empty(t, m.OGTitle)
}

func TestExtractMetadata_RelativeCanonical(t *testing.T) {
        html := `<link rel="canonical" href="/canonical-page">`
        m := extractMetadata(html, "https://example.com/article")
        assert.Equal(t, "https://example.com/canonical-page", m.Canonical)
}

func TestExtractMetadata_TimeFallback(t *testing.T) {
        // No article:published_time, but <time datetime="…"> is present.
        html := `<time datetime="2024-06-01">June 1</time>`
        m := extractMetadata(html, "https://example.com/")
        assert.Equal(t, "2024-06-01", m.PublishedTime)
}

func TestRenderMetadata_OmitsEmpty(t *testing.T) {
        m := pageMetadata{Title: "Only Title"}
        out := renderMetadata(m)
        assert.Contains(t, out, "Title: Only Title")
        assert.NotContains(t, out, "Description:")
        assert.NotContains(t, out, "Author:")
}

// === Outline extraction ===

func TestExtractOutline(t *testing.T) {
        html := `<h1>Top</h1><h2>Section A</h2><h3>Sub A1</h3><h3>Sub A2</h3><h2>Section B</h2>`
        out := extractOutline(html)
        require.Len(t, out, 5)
        assert.Equal(t, 1, out[0].Level)
        assert.Equal(t, "Top", out[0].Text)
        assert.Equal(t, 2, out[1].Level)
        assert.Equal(t, "Section A", out[1].Text)
        assert.Equal(t, 3, out[2].Level)
        assert.Equal(t, "Sub A1", out[2].Text)
        assert.Equal(t, 3, out[3].Level)
        assert.Equal(t, "Sub A2", out[3].Text)
        assert.Equal(t, 2, out[4].Level)
        assert.Equal(t, "Section B", out[4].Text)
}

func TestExtractOutline_StripsNestedTags(t *testing.T) {
        html := `<h1>My <a href="/">Title</a> with <span>link</span></h1>`
        out := extractOutline(html)
        require.Len(t, out, 1)
        assert.Equal(t, "My Title with link", out[0].Text)
}

func TestExtractOutline_Empty(t *testing.T) {
        out := extractOutline("<html><body>no headings</body></html>")
        assert.Empty(t, out)
}

func TestExtractOutline_TruncatesLong(t *testing.T) {
        long := strings.Repeat("a", 250)
        html := `<h1>` + long + `</h1>`
        out := extractOutline(html)
        require.Len(t, out, 1)
        assert.Contains(t, out[0].Text, "...")
        assert.Less(t, len(out[0].Text), 250)
}

func TestRenderOutline_Indentation(t *testing.T) {
        headings := []headingEntry{
                {Level: 1, Text: "Top"},
                {Level: 2, Text: "Section"},
                {Level: 3, Text: "Sub"},
        }
        out := renderOutline(headings)
        assert.Contains(t, out, "- Top")
        assert.Contains(t, out, "  - Section")
        assert.Contains(t, out, "    - Sub")
}

// === Link extraction with anchor text ===

func TestExtractLinks_AnchorText(t *testing.T) {
        html := `<a href="https://example.com/">Home</a>
<a href="https://example.com/about">About Us</a>
<a href="https://api.example.com/v1">API Reference</a>`
        links := extractLinks(html, "https://example.com/")
        require.Len(t, links, 3)
        // Sorted: api.example.com first.
        assert.Equal(t, "api.example.com", links[0].Host)
        assert.Equal(t, "API Reference", links[0].Text)
        assert.Equal(t, "example.com", links[1].Host)
        assert.Equal(t, "/", links[1].Path)
        assert.Equal(t, "Home", links[1].Text)
        assert.Equal(t, "About Us", links[2].Text)
}

func TestExtractLinks_NonAnchorNoText(t *testing.T) {
        // <link rel="canonical" href="…"> has no anchor text.
        html := `<link rel="canonical" href="https://example.com/canon">
<a href="https://example.com/">Home</a>`
        links := extractLinks(html, "https://example.com/")
        require.Len(t, links, 2)
        // The <a> link carries text; the <link> doesn't.
        var canon, anchor linkEntry
        for _, l := range links {
                if l.Path == "/canon" {
                        canon = l
                }
                if l.Path == "/" {
                        anchor = l
                }
        }
        assert.Equal(t, "", canon.Text)
        assert.Equal(t, "Home", anchor.Text)
}

func TestExtractLinks_StripsNestedTagsInAnchor(t *testing.T) {
        html := `<a href="https://example.com/x"><b>Bold</b> Link</a>`
        links := extractLinks(html, "https://example.com/")
        require.Len(t, links, 1)
        assert.Equal(t, "Bold Link", links[0].Text)
}

func TestFormatLinks_WithText(t *testing.T) {
        links := []linkEntry{
                {Host: "example.com", Path: "/", Text: "Home"},
                {Host: "example.com", Path: "/about", Text: "About Us"},
                {Host: "api.example.com", Path: "/v1", Text: ""},
        }
        out := formatLinks(links)
        assert.Contains(t, out, "/ — Home")
        assert.Contains(t, out, "/about — About Us")
        // Non-anchor link without text just shows the path.
        assert.Contains(t, out, "  /v1\n")
        assert.NotContains(t, out, "/v1 —")
}

// === runFetch: metadata + outline flags ===

func TestRunFetch_Metadata(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><head><title>Test Page</title><meta name="description" content="A test"></head><body>x</body></html>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Metadata: true})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "## Metadata")
        assert.Contains(t, out.Content, "Title: Test Page")
        assert.Contains(t, out.Content, "Description: A test")
}

func TestRunFetch_NoMetadataWhenDisabled(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><head><title>Test Page</title></head><body>x</body></html>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.NotContains(t, out.Content, "## Metadata")
}

func TestRunFetch_Outline(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><body><h1>Top</h1><h2>Section</h2><h3>Detail</h3></body></html>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Outline: true})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "## Outline")
        assert.Contains(t, out.Content, "- Top")
        assert.Contains(t, out.Content, "  - Section")
        assert.Contains(t, out.Content, "    - Detail")
}

func TestRunFetch_NoOutlineWhenDisabled(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><body><h1>Top</h1></body></html>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.NotContains(t, out.Content, "## Outline")
}

func TestRunFetch_LinksWithText(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<a href="/about">About Us</a><a href="/contact">Contact</a>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Links: true})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "## Links")
        assert.Contains(t, out.Content, "/about — About Us")
        assert.Contains(t, out.Content, "/contact — Contact")
}

// === Multi-URL mode ===

func TestRunFetch_MultiURL(t *testing.T) {
        server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("page one"))
        }))
        defer server1.Close()
        server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("page two"))
        }))
        defer server2.Close()

        raw, _ := json.Marshal(fetchInput{URLs: []string{server1.URL, server2.URL}})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "Fetched 2 URLs")
        assert.Contains(t, out.Content, "page one")
        assert.Contains(t, out.Content, "page two")
        // Per-URL separators.
        assert.Contains(t, out.Content, "---")
        // Summary reflects both ok.
        assert.Contains(t, out.Summary, "2 ok")
}

func TestRunFetch_MultiURLLimit(t *testing.T) {
        // 15 URLs over the cap of 10 — only the first 10 should be fetched.
        urls := make([]string, 15)
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("ok"))
        }))
        defer server.Close()
        for i := range urls {
                urls[i] = server.URL
        }

        raw, _ := json.Marshal(fetchInput{URLs: urls})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "Fetched 10 URLs")
        assert.Contains(t, out.Summary, "10 urls")
}

func TestRunFetch_MultiURLOneFails(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("good"))
        }))
        defer server.Close()

        // Mix one good URL with one unreachable one. Both should appear in the
        // output; the failed block carries an Error line, the good block carries
        // the body. The whole tool call does NOT fail.
        raw, _ := json.Marshal(fetchInput{URLs: []string{server.URL, "http://127.0.0.1:1/nonexistent"}})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "good")
        assert.Contains(t, out.Content, "Error:")
        assert.Contains(t, out.Summary, "1 ok")
        assert.Contains(t, out.Summary, "1 failed")
}

func TestRunFetch_MultiURLWithMetadata(t *testing.T) {
        server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><head><title>Page One</title></head><body>x</body></html>`))
        }))
        defer server1.Close()
        server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><head><title>Page Two</title></head><body>y</body></html>`))
        }))
        defer server2.Close()

        raw, _ := json.Marshal(fetchInput{URLs: []string{server1.URL, server2.URL}, Metadata: true})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "Title: Page One")
        assert.Contains(t, out.Content, "Title: Page Two")
}

// === DetailFromArgs covers multi-URL ===

func TestDetailFromArgs_MultiURL(t *testing.T) {
        tool := FetchTool()
        raw, _ := json.Marshal(fetchInput{URLs: []string{"https://a.com", "https://b.com"}})
        got := tool.DetailFromArgs(raw)
        assert.Equal(t, "2 urls", got)
}

func TestDetailFromArgs_SingleURL(t *testing.T) {
        tool := FetchTool()
        raw, _ := json.Marshal(fetchInput{URL: "https://example.com"})
        got := tool.DetailFromArgs(raw)
        assert.Equal(t, "https://example.com", got)
}

func TestDetailFromArgs_Empty(t *testing.T) {
        tool := FetchTool()
        raw, _ := json.Marshal(fetchInput{})
        got := tool.DetailFromArgs(raw)
        assert.Empty(t, got)
}

// === max_chars parameter ===

func TestHtmlToText_MaxCharsDefault(t *testing.T) {
        // maxChars=0 → default 8K. A 10K-char body is truncated.
        html := `<p>` + strings.Repeat("a", 10000) + `</p>`
        text := htmlToText(html, 0)
        assert.Contains(t, text, "[truncated]")
        assert.Less(t, len(text), 10000)
}

func TestHtmlToText_MaxCharsCustom(t *testing.T) {
        // maxChars=100 → truncate at 100 chars, then append "\n\n[truncated]".
        html := `<p>` + strings.Repeat("a", 1000) + `</p>`
        text := htmlToText(html, 100)
        assert.Contains(t, text, "[truncated]")
        // 100 chars of body + "\n\n[truncated]" marker = 113 total.
        assert.Equal(t, 113, len(text))
}

func TestHtmlToText_MaxCharsUnlimited(t *testing.T) {
        // maxChars=-1 → no truncation. A 5K-char body comes through whole.
        html := `<p>` + strings.Repeat("a", 5000) + `</p>`
        text := htmlToText(html, -1)
        assert.NotContains(t, text, "[truncated]")
        assert.Equal(t, 5000, len(text))
}

func TestRunFetch_MaxChars(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<p>` + strings.Repeat("a", 5000) + `</p>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, MaxChars: 200})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "[truncated]")
        assert.LessOrEqual(t, len(out.Content), 500) // 200 + headers + marker slack
}

func TestRunFetch_MaxCharsUnlimited(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<p>` + strings.Repeat("a", 5000) + `</p>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, MaxChars: -1})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.NotContains(t, out.Content, "[truncated]")
        assert.Contains(t, out.Content, strings.Repeat("a", 5000))
}

// === JSON pretty-print ===

func TestIsJSONContent(t *testing.T) {
        assert.True(t, isJSONContent("application/json"))
        assert.True(t, isJSONContent("application/vnd.api+json"))
        assert.True(t, isJSONContent("application/ld+json"))
        assert.True(t, isJSONContent("text/json"))
        assert.False(t, isJSONContent("text/html"))
        assert.False(t, isJSONContent("text/plain"))
}

func TestPrettyPrintJSON(t *testing.T) {
        // Compact JSON gets re-marshaled with 2-space indent.
        in := `{"a":1,"b":[2,3],"c":{"d":true}}`
        out := prettyPrintJSON(in, 0)
        assert.Contains(t, out, `{
  "a": 1,
  "b": [
    2,
    3
  ],
  "c": {
    "d": true
  }
}`)
}

func TestPrettyPrintJSON_InvalidFallsBack(t *testing.T) {
        // Not valid JSON — pretty-print returns the raw input.
        in := `not json at all`
        out := prettyPrintJSON(in, 0)
        assert.Equal(t, in, out)
}

func TestPrettyPrintJSON_Truncation(t *testing.T) {
        // Build a JSON that pretty-prints to something large.
        var sb strings.Builder
        sb.WriteString(`{"items":[`)
        for i := 0; i < 1000; i++ {
                if i > 0 {
                        sb.WriteString(",")
                }
                sb.WriteString(fmt.Sprintf(`"%d"`, i))
        }
        sb.WriteString(`]}`)

        out := prettyPrintJSON(sb.String(), 500)
        assert.Contains(t, out, "[truncated]")
        assert.Less(t, len(out), 1000)
}

func TestRunFetch_JSONPrettyPrint(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"name":"test","count":42,"items":[1,2,3]}`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        // The pretty-printed form has newlines between keys.
        assert.Contains(t, out.Content, "\"name\": \"test\"")
        assert.Contains(t, out.Content, "\"count\": 42")
        assert.Contains(t, out.Content, "\"items\": [")
        assert.Contains(t, out.Content, "  1,")
}

func TestRunFetch_JSONRawSkipsPrettyPrint(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{"a":1,"b":2}`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Raw: true})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        // raw=true returns the body as-is, so the compact JSON is preserved.
        assert.Contains(t, out.Content, `{"a":1,"b":2}`)
        assert.NotContains(t, out.Content, "\"a\": 1")
}

func TestRunFetch_JSONInvalidFallsBackToRaw(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json")
                _, _ = w.Write([]byte(`{this is not valid json`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        // Invalid JSON falls back to the raw body, not an error.
        assert.Contains(t, out.Content, `{this is not valid json`)
}

// === <pre>/<code> preservation ===

func TestHtmlToText_PrePreservation(t *testing.T) {
        // Indentation and newlines inside <pre> must survive the
        // collapse-whitespace step. Without preservation, the leading spaces
        // would be stripped and the newlines flattened to a single line.
        html := `<p>intro</p><pre>line 1
  line 2
    line 3</pre><p>outro</p>`
        text := htmlToText(html, 0)
        assert.Contains(t, text, "```")
        assert.Contains(t, text, "line 1\n  line 2\n    line 3")
        assert.Contains(t, text, "intro")
        assert.Contains(t, text, "outro")
}

func TestHtmlToText_PreStripsNestedTags(t *testing.T) {
        // Tags inside <pre> are stripped, but whitespace is preserved.
        html := `<pre>func main() {
  <b>x := 1</b>
  return x
}</pre>`
        text := htmlToText(html, 0)
        assert.Contains(t, text, "```")
        assert.Contains(t, text, "func main() {")
        assert.Contains(t, text, "  x := 1")
        assert.Contains(t, text, "  return x")
        assert.NotContains(t, text, "<b>")
}

func TestHtmlToText_PreWithAttributes(t *testing.T) {
        html := `<pre class="highlight">code</pre>`
        text := htmlToText(html, 0)
        assert.Contains(t, text, "```")
        assert.Contains(t, text, "code")
}

func TestHtmlToText_MultiplePre(t *testing.T) {
        html := `<pre>first</pre><p>between</p><pre>second</pre>`
        text := htmlToText(html, 0)
        assert.Contains(t, text, "first")
        assert.Contains(t, text, "second")
        assert.Contains(t, text, "between")
}

func TestRunFetch_PrePreservation(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/html")
                _, _ = w.Write([]byte(`<html><body>
<p>Intro paragraph.</p>
<pre>def hello():
    print("hi")
    return 42</pre>
<p>Outro paragraph.</p>
</body></html>`))
        }))
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "```")
        assert.Contains(t, out.Content, "def hello():")
        assert.Contains(t, out.Content, `    print("hi")`)
        assert.Contains(t, out.Content, "    return 42")
        assert.Contains(t, out.Content, "Intro paragraph")
        assert.Contains(t, out.Content, "Outro paragraph")
}

// === Redirects ===

func TestParseOptions_RedirectsDefault(t *testing.T) {
        opts, err := parseOptions(fetchInput{})
        require.NoError(t, err)
        assert.Equal(t, "follow", opts.redirects)
}

func TestParseOptions_RedirectsManual(t *testing.T) {
        opts, err := parseOptions(fetchInput{Redirects: "manual"})
        require.NoError(t, err)
        assert.Equal(t, "manual", opts.redirects)
}

func TestParseOptions_RedirectsNone(t *testing.T) {
        opts, err := parseOptions(fetchInput{Redirects: "none"})
        require.NoError(t, err)
        assert.Equal(t, "none", opts.redirects)
}

func TestParseOptions_RedirectsInvalid(t *testing.T) {
        _, err := parseOptions(fetchInput{Redirects: "always"})
        require.Error(t, err)
        assert.Contains(t, err.Error(), "invalid redirects")
}

func TestRunFetch_RedirectFollow(t *testing.T) {
        mux := http.NewServeMux()
        mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                http.Redirect(w, r, "/dest", http.StatusMovedPermanently)
        })
        mux.HandleFunc("/dest", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("destination content"))
        })
        server := httptest.NewServer(mux)
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Redirects: "follow"})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "destination content")
        // After following the redirect, the final URL is /dest; the agent sees
        // it labeled so it knows a redirect happened.
        assert.Contains(t, out.Content, "Final URL:")
        assert.Contains(t, out.Content, "/dest")
}

func TestRunFetch_RedirectManual(t *testing.T) {
        mux := http.NewServeMux()
        mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                http.Redirect(w, r, "/dest", http.StatusMovedPermanently)
        })
        server := httptest.NewServer(mux)
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Redirects: "manual"})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        // Manual mode: the 3xx response is returned, not followed.
        assert.Contains(t, out.Content, "301 Moved Permanently")
        assert.Contains(t, out.Content, "Location:")
        assert.Contains(t, out.Content, "/dest")
        // The body of a redirect response is typically empty or a short
        // HTML hint — we don't follow, so "destination" is NOT in the output.
        assert.NotContains(t, out.Content, "destination content")
}

func TestRunFetch_RedirectNone(t *testing.T) {
        mux := http.NewServeMux()
        mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                http.Redirect(w, r, "/dest", http.StatusMovedPermanently)
        })
        server := httptest.NewServer(mux)
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{URL: server.URL, Redirects: "none"})
        _, err := runFetch(t.Context(), raw)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "redirect blocked")
        assert.Contains(t, err.Error(), "/dest")
}

func TestRunFetch_RedirectNoneInMultiURL(t *testing.T) {
        // In multi-URL mode, redirect=none on a redirecting URL surfaces a
        // per-URL error block (not a top-level failure).
        mux := http.NewServeMux()
        mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
                http.Redirect(w, r, "/dest", http.StatusMovedPermanently)
        })
        mux.HandleFunc("/dest", func(w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "text/plain")
                _, _ = w.Write([]byte("ok"))
        })
        server := httptest.NewServer(mux)
        defer server.Close()

        raw, _ := json.Marshal(fetchInput{
                URLs:      []string{server.URL + "/dest", server.URL},
                Redirects: "none",
        })
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err) // multi-URL never returns top-level error
        assert.Contains(t, out.Content, "ok")
        assert.Contains(t, out.Content, "redirect blocked")
        assert.Contains(t, out.Summary, "1 ok")
        assert.Contains(t, out.Summary, "1 failed")
}

// === headers as object (preferred) or string (legacy) ===

func TestParseHeadersInput_ObjectForm(t *testing.T) {
        m, err := parseHeadersInput(json.RawMessage(`{"Accept":"text/html","X-Custom":"1"}`))
        require.NoError(t, err)
        assert.Equal(t, "text/html", m["Accept"])
        assert.Equal(t, "1", m["X-Custom"])
}

func TestParseHeadersInput_LegacyStringForm(t *testing.T) {
        // The legacy form is a JSON-encoded string containing a JSON object.
        // Useful for callers that already had headers as a string.
        m, err := parseHeadersInput(json.RawMessage(`"{\"Accept\":\"text/html\"}"`))
        require.NoError(t, err)
        assert.Equal(t, "text/html", m["Accept"])
}

func TestParseHeadersInput_Empty(t *testing.T) {
        m, err := parseHeadersInput(nil)
        require.NoError(t, err)
        assert.Empty(t, m)
}

func TestParseHeadersInput_Null(t *testing.T) {
        m, err := parseHeadersInput(json.RawMessage(`null`))
        require.NoError(t, err)
        assert.Empty(t, m)
}

func TestParseHeadersInput_EmptyString(t *testing.T) {
        m, err := parseHeadersInput(json.RawMessage(`""`))
        require.NoError(t, err)
        assert.Empty(t, m)
}

func TestParseHeadersInput_InvalidObject(t *testing.T) {
        // Not a valid object, not a string.
        _, err := parseHeadersInput(json.RawMessage(`[1,2,3]`))
        require.Error(t, err)
        assert.Contains(t, err.Error(), "invalid headers JSON")
}

func TestParseHeadersInput_InvalidLegacyString(t *testing.T) {
        // A JSON string containing malformed inner JSON.
        _, err := parseHeadersInput(json.RawMessage(`"{bad"`))
        require.Error(t, err)
        assert.Contains(t, err.Error(), "invalid headers JSON")
}

func TestRunFetch_HeadersAsObject(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.Header.Get("X-Custom") == "1" {
                        w.WriteHeader(http.StatusOK)
                } else {
                        w.WriteHeader(http.StatusBadRequest)
                }
                _, _ = w.Write([]byte("done"))
        }))
        defer server.Close()

        // Pass headers as a JSON object (the preferred form the schema advertises).
        raw, _ := json.Marshal(fetchInput{URL: server.URL, Headers: json.RawMessage(`{"X-Custom":"1"}`)})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "200 OK")
}

func TestRunFetch_HeadersAsLegacyString(t *testing.T) {
        server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
                if r.Header.Get("X-Custom") == "1" {
                        w.WriteHeader(http.StatusOK)
                } else {
                        w.WriteHeader(http.StatusBadRequest)
                }
                _, _ = w.Write([]byte("done"))
        }))
        defer server.Close()

        // Pass headers as a JSON-encoded string (the legacy form, kept for
        // backward compatibility with old callers).
        raw, _ := json.Marshal(fetchInput{URL: server.URL, Headers: json.RawMessage(`"{\"X-Custom\":\"1\"}"`)})
        out, err := runFetch(t.Context(), raw)
        require.NoError(t, err)
        assert.Contains(t, out.Content, "200 OK")
}
