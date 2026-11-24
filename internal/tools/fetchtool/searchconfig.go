package fetchtool

import (
	"os"
	"strings"

	"github.com/damien1141/a1/internal/project"
)

// SearchConfig is the fetchtool's view of the `search:` config section. It is
// read through a function so a config save reaches the next search without a
// restart, and so tests can pin the value without touching the real config.
type SearchConfig struct {
	// SearxngURL is a self-hosted SearXNG base URL, e.g.
	// "http://127.0.0.1:8888". Empty means the engine is not registered.
	SearxngURL string
}

// searxngURLFromConfig resolves the SearXNG URL: the A1_SEARXNG_URL env var
// wins over config.yaml (a per-invocation override that needs no file edit),
// matching how A1_FILES / A1_MCP / A1_BROWSER_HEADLESS already behave.
//
// Exported in spirit via the env var; the lookup is deliberately forgiving —
// an unloaded project, a nil config, or a nil section all mean "not
// configured", never an error. A search tool that failed to boot because the
// config was mid-write would be worse than one that falls back to the
// scrapers.
func searxngURLFromConfig() string {
	if v := strings.TrimSpace(os.Getenv("A1_SEARXNG_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	proj := project.GetDefaultProject()
	if proj == nil || proj.Config() == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(proj.Config().Search.SearxngURL), "/")
}
