package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

const (
	fetchDefaultTimeout = 30 * time.Second
	fetchMaxBytes       = 1 << 20 // 1 MiB
)

var fetchDescription = `Fetch web content via HTTP GET/POST.

Returns response body, status code, and selected headers. HTML responses are
automatically converted to readable text to keep the output token-friendly.
Enforces a timeout and a maximum response size to keep the agent safe.

With links=true, also returns a deduplicated list of every link found on the
page, grouped by host (subdomains first) and path. This lets the agent see the
site's structure — what subdomains exist and what paths are reachable — so it
can navigate without guessing.`

// FetchTool returns the web fetch tool definition + handler.
func FetchTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "fetch",
			Description: fetchDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"url": llm.Object{
						"type":        "string",
						"description": "URL to fetch. Example: https://example.com",
					},
					"method": llm.Object{
						"type":        "string",
						"description": "HTTP method: GET or POST. Example: GET",
					},
					"headers": llm.Object{
						"type":        "string",
						"description": "JSON object of extra request headers. Example: {\"Accept\": \"text/plain\"}",
					},
					"body": llm.Object{
						"type":        "string",
						"description": "Request body for POST. Example: {\"q\": \"hello\"}",
					},
					"raw": llm.Object{
						"type":        "boolean",
						"description": "Return raw response body without HTML-to-text conversion. Example: false",
					},
					"links": llm.Object{
						"type":        "boolean",
						"description": "Also return every link found on the page, grouped by host (subdomains first) and path, so the agent can see the site structure and know where to navigate. Example: true",
					},
				},
				Required: []string{"url"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in fetchInput
			_ = json.Unmarshal(input, &in)
			u := strings.TrimSpace(in.URL)
			if u == "" {
				return "fetch"
			}
			return fmt.Sprintf("fetch %s", u)
		},
		Run: runFetch,
	}
}

type fetchInput struct {
	URL     string `json:"url"`
	Method  string `json:"method,omitempty"`
	Headers string `json:"headers,omitempty"`
	Body    string `json:"body,omitempty"`
	Raw     bool   `json:"raw,omitempty"`
	Links   bool   `json:"links,omitempty"`
}

func runFetch(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in fetchInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse fetch arguments: %w", err)
	}

	url := strings.TrimSpace(in.URL)
	if url == "" {
		return tooldef.Result{}, fmt.Errorf("url is required")
	}

	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		return tooldef.Result{}, fmt.Errorf("unsupported method %q: use GET or POST", method)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(strings.TrimSpace(in.Body)))
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "a1-fetch/1.0")
	req.Header.Set("Accept", "*/*")

	if h := strings.TrimSpace(in.Headers); h != "" {
		var extra map[string]string
		if err := json.Unmarshal([]byte(h), &extra); err != nil {
			return tooldef.Result{}, fmt.Errorf("invalid headers JSON: %w", err)
		}
		for k, v := range extra {
			req.Header.Set(k, v)
		}
	}

	client := &http.Client{Timeout: fetchDefaultTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	limited := &io.LimitedReader{R: resp.Body, N: fetchMaxBytes}
	body, err := io.ReadAll(limited)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("read response: %w", err)
	}
	if limited.N <= 0 {
		return tooldef.Result{}, fmt.Errorf("response exceeded %d bytes", fetchMaxBytes)
	}

	contentType := resp.Header.Get("Content-Type")
	bodyStr := string(body)
	// Extract links from the raw HTML before htmlToText strips the tags.
	var links []linkEntry
	if in.Links && isHTMLContent(contentType) {
		links = extractLinks(bodyStr, url)
	}
	if !in.Raw && isHTMLContent(contentType) {
		bodyStr = htmlToText(bodyStr)
	} else if !isTextContent(contentType) && !isHTMLContent(contentType) {
		bodyStr = fmt.Sprintf("[binary %s %d bytes]", resp.Header.Get("Content-Type"), len(body))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Fetched %s %s\n", method, url))
	sb.WriteString(strings.Repeat("=", 60))
	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("Status: %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode)))
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		sb.WriteString(fmt.Sprintf("Content-Type: %s\n", ct))
	}
	sb.WriteString("\n")
	sb.WriteString(bodyStr)
	if len(body) >= fetchMaxBytes {
		sb.WriteString("\n\n[truncated]")
	}
	sb.WriteString("\n")

	detail := fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	result := tooldef.Result{Content: sb.String(), Detail: detail, Output: sb.String()}

	// When links=true, append the site structure so the agent can see what
	// subdomains and paths exist and navigate without guessing.
	if len(links) > 0 {
		result.Content += "\n\n## Links\n\n" + formatLinks(links)
		result.Output = result.Content
	}

	return result, nil
}

func isTextContent(ct string) bool {
	return strings.Contains(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "xml") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "css")
}

func isHTMLContent(ct string) bool {
	return strings.Contains(ct, "html")
}

// htmlToText strips HTML tags and returns readable text. It preserves line breaks
// around block-level elements and collapses whitespace to keep the output
// token-friendly.
func htmlToText(html string) string {
	// Replace block-level tags with newlines to preserve structure.
	blockTags := regexp.MustCompile(
		`(?i)</(p|div|section|article|header|footer|main|nav|aside|ul|ol|li|table|tr|td|th|h[1-6]|blockquote|pre|code|br|hr)[^>]*>\s*`,
	)
	text := blockTags.ReplaceAllString(html, "\n")

	// Remove all remaining HTML tags.
	tagRe := regexp.MustCompile(`(?i)<[^>]+>`)
	text = tagRe.ReplaceAllString(text, "")

	// Decode common HTML entities.
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&quot;", "\"")
	text = strings.ReplaceAll(text, "&#39;", "'")

	// Collapse whitespace: trim each line and remove blank lines.
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			cleaned = append(cleaned, line)
		}
	}
	text = strings.Join(cleaned, "\n")

	// Limit output to ~8k characters to stay token-friendly.
	const maxTextLen = 1 << 13 // 8k
	if len(text) > maxTextLen {
		text = text[:maxTextLen] + "\n\n[truncated]"
	}
	return text
}

// linkRe matches href / src attributes. It is intentionally loose: any URL
// -looking value is captured, and the caller resolves relative URLs against
// the page base.
var linkRe = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*"([^"]+)"`)

// linkEntry is one resolved link, grouped by host for the agent's navigation.
type linkEntry struct {
	Host string
	Path string
}

// extractLinks finds every href/src in the raw HTML, resolves them against
// the page URL, and returns the unique set grouped by host.
func extractLinks(html, baseURL string) []linkEntry {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	var out []linkEntry
	for _, m := range linkRe.FindAllStringSubmatch(html, -1) {
		raw := strings.TrimSpace(m[1])
		if raw == "" {
			continue
		}
		// Skip non-navigable schemes: mailto, tel, javascript:.
		if strings.Contains(raw, ":") && !strings.HasPrefix(raw, "http") && !strings.HasPrefix(raw, "//") {
			continue
		}
		u, err := base.Parse(raw)
		if err != nil || u.Scheme == "" {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		key := u.Host + u.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, linkEntry{Host: u.Host, Path: u.Path})
	}
	// Sort by host so the output is scannable: subdomains group together.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// formatLinks renders the link map as a compact, scannable block: one host
// header followed by its paths. Subdomains sort naturally under the parent
// host, so the agent sees the site's structure at a glance.
func formatLinks(links []linkEntry) string {
	var sb strings.Builder
	var cur string
	count := 0
	for _, l := range links {
		if l.Host != cur {
			cur = l.Host
			sb.WriteString(fmt.Sprintf("\n%s\n", cur))
			count++
		}
		p := l.Path
		if p == "" {
			p = "/"
		}
		sb.WriteString(fmt.Sprintf("  %s\n", p))
	}
	sb.WriteString(fmt.Sprintf("\n(%d links across %d hosts)", len(links), count))
	return sb.String()
}
