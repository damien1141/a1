package fetchtool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// stackExchangeEngine searches the free Stack Exchange API
// (https://api.stackexchange.com/2.3/search). No API key required for
// low-volume use (the API is rate-limited per IP, generous for casual
// agent use). Returns JSON with question titles, scores, tags, and
// links — exactly what a coding agent needs to find canonical
// answers.
//
// The Matches() predicate looks for programming signals: language
// names, error/exception terms, file extensions, common code
// concepts (function, class, method, api, sql, regex, docker, etc.).
// When these fire, Stack Exchange is the single best engine for a
// coding agent — better than general web search by a wide margin.
type stackExchangeEngine struct{}

func newStackExchangeEngine() *stackExchangeEngine { return &stackExchangeEngine{} }

func (stackExchangeEngine) Name() string { return "stackexchange" }

// programmingSignalRe matches queries that contain programming
// signals: language names, error terms, file extensions, code-y
// concepts. The regex is case-insensitive and word-boundary-aware so
// "go" matches as a language but not as a substring of "goodbye".
//
// We deliberately err on the side of matching — false positives just
// mean we try Stack Exchange first and fall back if it returns
// nothing. False negatives are worse: a "how do I parse JSON in go"
// query that doesn't match would route to Wikipedia/Playwright and
// give a worse answer.
var programmingSignalRe = regexp.MustCompile(`(?i)\b(go|golang|python|py|rust|rs|javascript|js|typescript|ts|java|kotlin|swift|ruby|rb|php|perl|lua|c\b|c\+\+|cpp|c#|csharp|shell|bash|zsh|powershell|sql|html|css|scss|sass|react|vue|angular|svelte|node|deno|bun|next\.js|nuxt|astro|docker|kubernetes|k8s|git|regex|error|exception|traceback|stacktrace|nullpointer|segfault|compile|build|linker|function|class|method|interface|struct|enum|generics|async|await|promise|future|goroutine|channel|mutex|trait|impl|module|package|import|require|export|callback|closure|lambda|iterator|generator|yield|decorator|annotation|middleware|serializer|deserializer|json|yaml|toml|xml|protobuf|grpc|http|rest|graphql|websocket|socket|tcp|udp|cors|csrf|jwt|oauth|openid|api|sdk|cli|tui|gui|crd|controller|reconcile|operator|helm|terraform|ansible|puppet|chef|vault|consul|nomad|redis|postgres|mysql|sqlite|mongodb|cassandra|kafka|rabbitmq|elasticsearch|grafana|prometheus|jaeger|zipkin|linux|windows|macos|bsd|freebsd|openbsd|netbsd|solaris)\b`)

// fileExtensionRe matches common code file extensions: .py, .go, .rs,
// .ts, .js, .tsx, .jsx, .java, .rb, .php, .c, .cpp, .h, .hpp, .cs,
// .swift, .kt, .sh, .sql, .yaml, .yml, .json, .toml, .xml. The
// leading dot makes it unambiguous — "main.go" is obviously a code
// path, "go" alone is a language name.
var fileExtensionRe = regexp.MustCompile(`(?i)\.(py|go|rs|ts|tsx|js|jsx|java|rb|php|c|cpp|h|hpp|cs|swift|kt|sh|sql|ya?ml|json|toml|xml|html|css|scss|sass|vue|svelte|astro)\b`)

func (stackExchangeEngine) Matches(query string) bool {
	return programmingSignalRe.MatchString(query) || fileExtensionRe.MatchString(query)
}

// stackExchangeSearchResponse is the JSON shape of
// https://api.stackexchange.com/2.3/search?site=stackoverflow&...
type stackExchangeSearchResponse struct {
	Items []struct {
		Title      string   `json:"title"`
		Link       string   `json:"link"`
		Score      int      `json:"score"`
		Tags       []string `json:"tags"`
		IsAnswered bool     `json:"is_answered"`
		AnswerCount int    `json:"answer_count"`
	} `json:"items"`
}

func (e *stackExchangeEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// Stack Exchange's `intitle` parameter matches questions whose
	// title contains the exact search term. For multi-word queries we
	// also pass `q` for body+tag matching. Both are URL-encoded.
	endpoint := fmt.Sprintf("https://api.stackexchange.com/2.3/search?order=desc&sort=votes&site=stackoverflow&pagesize=%d&q=%s",
		maxResults, url.QueryEscape(strings.TrimSpace(query)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stackexchange: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stackexchange: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var sr stackExchangeSearchResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("stackexchange: parse: %w", err)
	}

	results := make([]searchResult, 0, len(sr.Items))
	sources := make([]tooldef.Source, 0, len(sr.Items))
	for i, item := range sr.Items {
		// Snippet: tags + score + answer count, formatted compactly.
		tags := strings.Join(item.Tags, ", ")
		if len(tags) > 80 {
			tags = tags[:77] + "..."
		}
		snippet := fmt.Sprintf("Score: %d · Answers: %d · Tags: %s",
			item.Score, item.AnswerCount, tags)
		host := hostOf(item.Link)
		host = strings.TrimPrefix(host, "www.")
		rank := i + 1
		results = append(results, searchResult{
			rank:    rank,
			url:     item.Link,
			title:   item.Title,
			snippet: snippet,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        item.Link,
			Title:      item.Title,
			Host:       host,
			FaviconURL: formatFaviconURL(host),
			Snippet:    snippet,
		})
	}

	return &SearchResponse{
		Engine:  e.Name(),
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}
