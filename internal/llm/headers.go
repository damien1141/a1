package llm

import (
	"net/http"
	"strings"
)

// ApplyExtraHeaders stamps a model's configured Headers onto an outgoing
// provider request. Every client calls it after setting its own defaults,
// so a configured header can override a built-in (a gateway that wants its
// own Authorization flavor or Anthropic-Version) while the common case —
// extra routing keys like X-Provider — simply adds. Blank header names are
// dropped; values go out verbatim (an empty value is a legal header).
func ApplyExtraHeaders(req *http.Request, headers map[string]string) {
	if req == nil || len(headers) == 0 {
		return
	}
	for name, value := range headers {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		req.Header.Set(key, value)
	}
}
