package llm

import (
        "context"
        "encoding/json"
        "io"
        "net"
        "net/http"
        "net/url"
        "regexp"
        "strings"
        "time"
)

// AssumedContextWindow is the fallback used when a model's context window is
// unknown: no explicit context_window in config, no built-in preset, and the
// local probe came back empty. Folding must run on unknown-window models —
// before this fallback existed, window 0 disabled both compaction triggers
// and the only folding left was the post-overflow recovery path.
//
// 32K is the balance point: small enough that any real window ≥ 32K is
// folded safely (never overflows), large enough that an unknown big-window
// model is not re-summarized every few thousand tokens while the runtime
// waits for the first overflow error to teach it the true limit. See
// doc/context-folding-math.md §4 for the cost analysis.
const AssumedContextWindow = 32768

// contextWindowFloor is the smallest usable window: below this, keepRecentMin
// (2000) + the summary cap leave almost no room for new work. Halving during
// overflow recovery stops here.
const contextWindowFloor = 4096

// limitPatterns extracts the model's context-window limit from provider
// overflow error text. Each capture group is the LIMIT, never the request
// size — patterns that could match either are anchored on limit phrasing.
var limitPatterns = []*regexp.Regexp{
        regexp.MustCompile(`(?i)maximum context length is ([\d,]+)`),
        regexp.MustCompile(`(?i)maximum context length of ([\d,]+)`),
        regexp.MustCompile(`(?i)maximum prompt length is ([\d,]+)`),
        regexp.MustCompile(`(?i)exceeds the limit of ([\d,]+)`),
        regexp.MustCompile(`(?i)maximum number of tokens allowed \(?([\d,]+)\)?`),
        regexp.MustCompile(`(?i)prompt is too long: [\d,]+ > ([\d,]+)`),
        regexp.MustCompile(`(?i)tokens > ([\d,]+) maximum`),
        regexp.MustCompile(`(?i)greater than the context length \(?([\d,]+)\)?`),
        regexp.MustCompile(`(?i)model with ([\d,]+) maximum context length`),
        regexp.MustCompile(`(?i)exceeds context length \(?([\d,]+)\)?`),
}

// ParseContextLimit extracts the context-window limit stated in a provider
// overflow error, if any. The parsed limit is ground truth — the provider
// measured it — so callers use it to correct the runtime window even when a
// config value disagrees. ok is false for numberless errors (llama.cpp's
// "request exceeds the available context size") and for out-of-band numbers
// (a request-size capture or a nonsense value).
func ParseContextLimit(err error) (int, bool) {
        if err == nil {
                return 0, false
        }
        msg := strings.TrimSpace(err.Error())
        if msg == "" {
                return 0, false
        }
        for _, p := range limitPatterns {
                m := p.FindStringSubmatch(msg)
                if len(m) < 2 {
                        continue
                }
                n, parseErr := parseGroupedInt(m[1])
                if parseErr != nil {
                        continue
                }
                // Bounds: a real context window is at least a few hundred tokens and
                // at most ~100M. Outside that, the capture is a request size or noise.
                if n < 512 || n > 100_000_000 {
                        continue
                }
                return n, true
        }
        return 0, false
}

// parseGroupedInt parses a possibly comma-grouped integer ("1,048,576").
func parseGroupedInt(s string) (int, error) {
        s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
        if s == "" {
                return 0, errBadNumber
        }
        var n int
        for _, r := range s {
                if r < '0' || r > '9' {
                        return 0, errBadNumber
                }
                n = n*10 + int(r-'0')
                if n > 100_000_000 {
                        return 0, errBadNumber
                }
        }
        return n, nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const errBadNumber = simpleError("not a plain integer")

// probeBudget caps each probe request. The probe only targets local servers
// (llama.cpp / ollama), where a healthy response is sub-millisecond; the
// budget exists so a wedged local daemon cannot stall a turn's start.
const probeBudget = 1500 * time.Millisecond

const probeBodyLimit = 256 << 10

// ProbeContextWindow best-effort discovers the context window of a local
// OpenAI-compatible server: llama.cpp's GET /props (default_generation_settings.n_ctx
// or any n_ctx in the document) and ollama's POST /api/show (any
// *.context_length key). It returns 0 when the base URL is not local, the
// endpoint does not answer, or no number is found — callers fall back to
// AssumedContextWindow. Probing public hosts is deliberately refused: the
// shapes above are local-daemon APIs, and an outbound GET from an agent
// harness to an arbitrary public origin is not something to do silently.
func ProbeContextWindow(ctx context.Context, httpClient *http.Client, baseURL, model string) int {
        if httpClient == nil {
                return 0
        }
        origin, ok := localOrigin(baseURL)
        if !ok {
                return 0
        }
        if n := probeLlamaProps(ctx, httpClient, origin); n > 0 {
                return n
        }
        if n := probeOllamaShow(ctx, httpClient, origin, model); n > 0 {
                return n
        }
        return 0
}

// localOrigin returns scheme://host when baseURL points at a loopback or
// private-network host, else ok=false.
func localOrigin(baseURL string) (string, bool) {
        u, err := url.Parse(strings.TrimSpace(baseURL))
        if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
                return "", false
        }
        host := u.Hostname()
        if !isLocalHost(host) {
                return "", false
        }
        return u.Scheme + "://" + u.Host, true
}

func isLocalHost(host string) bool {
        if strings.EqualFold(host, "localhost") {
                return true
        }
        ip := net.ParseIP(host)
        if ip == nil {
                return false
        }
        return ip.IsLoopback() || ip.IsPrivate()
}

// probeLlamaProps reads llama-server's /props and returns the largest n_ctx
// found (per-slot contexts share the -c value; max is the safe read).
func probeLlamaProps(ctx context.Context, httpClient *http.Client, origin string) int {
        pctx, cancel := context.WithTimeout(ctx, probeBudget)
        defer cancel()
        req, err := http.NewRequestWithContext(pctx, http.MethodGet, origin+"/props", http.NoBody)
        if err != nil {
                return 0
        }
        req.Header.Set("Accept", "application/json")
        resp, err := httpClient.Do(req)
        if err != nil {
                return 0
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return 0
        }
        body, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
        if err != nil {
                return 0
        }
        var doc any
        if json.Unmarshal(body, &doc) != nil {
                return 0
        }
        return maxNumberForKey(doc, "n_ctx")
}

// probeOllamaShow asks ollama's /api/show for the model card and returns the
// largest *.context_length found (ollama reports it as "<arch>.context_length"
// or "context_length" under model_info depending on version).
func probeOllamaShow(ctx context.Context, httpClient *http.Client, origin, model string) int {
        if strings.TrimSpace(model) == "" {
                return 0
        }
        pctx, cancel := context.WithTimeout(ctx, probeBudget)
        defer cancel()
        req, err := http.NewRequestWithContext(
                pctx,
                http.MethodPost,
                origin+"/api/show",
                strings.NewReader(`{"model":`+mustJSONString(model)+`}`),
        )
        if err != nil {
                return 0
        }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Accept", "application/json")
        resp, err := httpClient.Do(req)
        if err != nil {
                return 0
        }
        defer resp.Body.Close()
        if resp.StatusCode != http.StatusOK {
                return 0
        }
        body, err := io.ReadAll(io.LimitReader(resp.Body, probeBodyLimit))
        if err != nil {
                return 0
        }
        var doc any
        if json.Unmarshal(body, &doc) != nil {
                return 0
        }
        n := maxNumberForSuffix(doc, ".context_length")
        if m := maxNumberForKey(doc, "context_length"); m > n {
                n = m
        }
        return n
}

func mustJSONString(s string) string {
        b, _ := json.Marshal(s)
        return string(b)
}

// maxNumberForKey walks a decoded JSON document and returns the largest
// number stored under an exactly-matching key (missing → 0).
func maxNumberForKey(doc any, key string) int {
        best := 0
        walkJSON(doc, func(k string, v float64) {
                if k == key && int(v) > best {
                        best = int(v)
                }
        })
        return best
}

// maxNumberForSuffix walks a decoded JSON document and returns the largest
// number stored under a key ending with suffix (missing → 0).
func maxNumberForSuffix(doc any, suffix string) int {
        best := 0
        walkJSON(doc, func(k string, v float64) {
                if strings.HasSuffix(k, suffix) && int(v) > best {
                        best = int(v)
                }
        })
        return best
}

// walkJSON visits every numeric leaf with its key.
func walkJSON(v any, visit func(key string, val float64)) {
        switch t := v.(type) {
        case map[string]any:
                for k, vv := range t {
                        switch n := vv.(type) {
                        case float64:
                                visit(k, n)
                        default:
                                walkJSON(vv, visit)
                        }
                }
        case []any:
                for _, vv := range t {
                        walkJSON(vv, visit)
                }
        }
}

// ContextWindowFloor exposes the halving floor for overflow recovery; see
// doc/context-folding-math.md §5. The floor is where post-compaction context
// (keepRecentMin + summary cap = 2000 + 0.8·2048 = 3638) still fits.
func ContextWindowFloor() int { return contextWindowFloor }
