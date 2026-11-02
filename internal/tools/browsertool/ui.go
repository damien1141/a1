package browsertool

import (
	"fmt"
	"strings"
	"time"
)

// The render half of the browser tool: what the transcript row shows, kept apart
// from what the model receives.
//
// Every action used to route its result through the same strings.Builder, so a
// screenshot printed a bare path and a content dump printed a page with no
// framing — and several actions overwrote Detail with a completion value, which
// erased the row's identity ("browser open <url>" became "closed"). Summary is
// the field for the outcome; Detail stays the identity for the whole run.

// Summary composes the one-line outcome for a resolved action row. verb names
// what was done; count is how much came back, when that is meaningful.
//
// A url target is reduced to its host: printing the whole path is what made
// these rows unreadable, and the full url is in the expanded body and in the
// model's Content anyway.
func Summary(verb, target string, count int, elapsed time.Duration) string {
	parts := []string{verb}
	if t := strings.TrimSpace(target); t != "" {
		parts = append(parts, displayTarget(t))
	}
	if count > 0 {
		parts = append(parts, countLabel(count))
	}
	if elapsed >= time.Second {
		parts = append(parts, fmt.Sprintf("%.1fs", elapsed.Seconds()))
	}
	return strings.Join(parts, " · ")
}

// displayTarget reduces a url to its host and passes anything else through
// untouched, so a selector or a query stays intact.
func displayTarget(target string) string {
	if !strings.Contains(target, "://") && !strings.HasPrefix(target, "//") {
		return target
	}
	return hostOf(target)
}

func countLabel(n int) string {
	if n == 1 {
		return "1 result"
	}
	return fmt.Sprintf("%d results", n)
}

// hostOf extracts a display host from a url so a row names the site rather than
// repeating the whole path.
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

// Running is the line shown while a browser action is in flight. Playwright
// waits for a load state, which for a heavy page is seconds of silence.
func Running(action string) string {
	a := strings.TrimSpace(action)
	if a == "" {
		return "Driving the browser…"
	}
	return a + "…"
}

// ScreenshotSummary names the file without printing its full path in the row:
// the path is in the expanded body, and the row is the glanceable part.
func ScreenshotSummary(path string) string {
	base := path
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		base = path[i+1:]
	}
	if base == "" {
		return "screenshot saved"
	}
	return "saved " + base
}

// ProfilesSummary reports how many profiles exist.
func ProfilesSummary(n int) string {
	if n == 1 {
		return "1 profile"
	}
	return fmt.Sprintf("%d profiles", n)
}

// HostOf exposes the url-to-host reduction for callers outside the package.
func HostOf(url string) string { return hostOf(url) }
