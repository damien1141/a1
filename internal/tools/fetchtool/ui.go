package fetchtool

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// This file is the render half of the fetch tool: what the transcript row shows,
// kept apart from what the model receives.
//
// Claude Code's WebSearchTool/UI.tsx exports a per-tool render module with a
// summary, a running line, and a completion line. a1's fetch tool instead built
// its own display chrome into the text it returned to the model — a
// "Fetched GET url" header, a 60-char "=" rule, and a "Status:" / "Content-Type:"
// preamble — so the transcript showed all of it and no summary at all. Splitting
// the two is the whole point of this file.

// Summary composes the one-line outcome for a resolved fetch row.
//
// The status line moves here from the header chrome: the model still gets it in
// Content where it is genuinely useful for reasoning, and the user gets the short
// version instead of a page of headers.
func Summary(url string, statusCode, bytes int, elapsed time.Duration) string {
	host := hostOf(url)
	size := "0 B"
	if bytes > 0 {
		size = formatBytes(bytes)
	}
	dur := "0ms"
	if elapsed >= time.Second {
		dur = fmt.Sprintf("%.1fs", elapsed.Seconds())
	} else {
		dur = fmt.Sprintf("%dms", elapsed.Milliseconds())
	}
	return fmt.Sprintf("%s · %s · %s", statusText(statusCode), size, dur) + hostSuffix(host)
}

func statusText(code int) string {
	if code <= 0 {
		return "no response"
	}
	if text := http.StatusText(code); text != "" {
		return fmt.Sprintf("%d %s", code, text)
	}
	return fmt.Sprintf("%d", code)
}

func hostSuffix(host string) string {
	if host == "" {
		return ""
	}
	return " · " + host
}

// HostOf extracts the host from a url, tolerating anything that is not a url so
// a malformed argument cannot crash a render path.
func hostOf(url string) string {
	u := strings.TrimSpace(url)
	for _, scheme := range []string{"https://", "http://", "//"} {
		if rest, ok := strings.CutPrefix(u, scheme); ok {
			u = rest
			break
		}
	}
	if i := strings.IndexAny(u, "/?#"); i >= 0 {
		u = u[:i]
	}
	if i := strings.LastIndex(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 {
		u = u[:i]
	}
	return u
}

// Running is the line shown while the request is in flight. A fetch can take
// seconds, and an unchanged row for that whole time reads as a hang.
func Running(url string) string {
	host := hostOf(url)
	if host == "" {
		return "Fetching…"
	}
	return "Fetching " + host + "…"
}

func formatBytes(n int) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// HostOf exposes the url-to-host reduction for tests and for callers that want
// to name a site without repeating the whole path.
func HostOf(url string) string { return hostOf(url) }
