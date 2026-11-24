package fetchtool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearxngEngine_Search_Positive: a well-formed SearXNG JSON body becomes
// ranked results + pre-built source cards, and the router-facing Engine name
// is stamped. Oracle: the JSON below is a hand-written literal of the shape
// SearXNG's API documents, so a decoder that silently drops fields fails here.
func TestSearxngEngine_Search_Positive(t *testing.T) {
	body := `{"results":[
		{"url":"https://example.com/a","title":"Alpha","content":"first hit","engine":"google"},
		{"url":"https://example.org/b","title":"Beta","content":"second hit","engine":"bing"},
		{"url":"","title":"no url — must be skipped","content":"","engine":"google"}
	]}`

	eng := newSearxngEngine("http://127.0.0.1:8888")
	require.NotNil(t, eng)
	resp := decodeSearxngForTest(t, eng, body, searchOptions{maxResults: 5})

	require.NotNil(t, resp)
	assert.Equal(t, "searxng", resp.Engine)
	// The empty-URL row is dropped, so two results survive and ranks stay dense.
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "https://example.com/a", resp.Results[0].url)
	assert.Equal(t, 1, resp.Results[0].rank)
	assert.Equal(t, 2, resp.Results[1].rank, "ranks must be dense after a skipped row")

	require.Len(t, resp.Sources, 2)
	assert.Equal(t, "example.com", resp.Sources[0].Host, "www. must be trimmed from the host pill")
	assert.NotEmpty(t, resp.Sources[0].FaviconURL)
}

// TestSearxngEngine_MaxResultsClamp_Negative: maxResults caps the slice, and a
// non-positive value means "everything the instance returned" rather than zero.
func TestSearxngEngine_MaxResultsClamp_Negative(t *testing.T) {
	body := `{"results":[
		{"url":"https://a.test/1","title":"1","content":"x"},
		{"url":"https://a.test/2","title":"2","content":"x"},
		{"url":"https://a.test/3","title":"3","content":"x"}
	]}`

	eng := newSearxngEngine("http://127.0.0.1:8888")
	require.NotNil(t, eng)

	capped := decodeSearxngForTest(t, eng, body, searchOptions{maxResults: 2})
	require.Len(t, capped.Results, 2, "maxResults must clamp")

	uncapped := decodeSearxngForTest(t, eng, body, searchOptions{maxResults: 0})
	require.Len(t, uncapped.Results, 3, "maxResults <= 0 means no cap")
}

// TestSearxngEngine_URLResolution_Differential pins the env-over-config rule
// against a hand-computed expectation: env wins, both are trimmed, and empty
// means "not configured" (a nil engine, so the router never registers it).
func TestSearxngEngine_URLResolution_Differential(t *testing.T) {
	cases := []struct {
		name string
		env  string
		cfg  string
		want string
	}{
		{"config only", "", "http://cfg.local:8080/", "http://cfg.local:8080"},
		{"env wins over config", "http://env.local:9999", "http://cfg.local:8080", "http://env.local:9999"},
		{"env trimmed of spaces and slash", "  http://env.local:9999/  ", "", "http://env.local:9999"},
		{"neither configured", "", "", ""},
		{"whitespace-only config is unset", "", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("A1_SEARXNG_URL", tc.env)
			// oracle: the expected string is hand-written above, not derived
			// by calling the function under test.
			assert.Equal(t, tc.want, searxngURL(tc.cfg))
			eng := newSearxngEngine(tc.cfg)
			if tc.want == "" {
				assert.Nil(t, eng, "an unconfigured instance must not register an engine")
			} else {
				require.NotNil(t, eng)
				assert.Equal(t, tc.want, eng.baseURL)
			}
		})
	}
}

// decodeSearxngForTest drives the engine's JSON decoding without a network
// round trip: the body is served through the same code path Search uses, with
// the HTTP layer bypassed. It keeps the pins meaningful on a sandbox where
// loopback dials are denied.
func decodeSearxngForTest(t *testing.T, eng *searxngEngine, body string, opts searchOptions) *SearchResponse {
	t.Helper()
	parsed, err := parseSearxngBody(body)
	require.NoError(t, err)
	resp := eng.responseFrom(parsed, "q", opts)
	return resp
}
