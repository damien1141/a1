package fetchtool

import (
	"context"
	"fmt"
	"testing"

	"github.com/damien1141/a1/internal/bw"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeBWDaemon swaps the daemon for a fixed envelope and restores it after.
func fakeBWDaemon(t *testing.T, out string, runErr error) *[]string {
	t.Helper()
	var calls []string
	origRun, origResolve := bw.RunCommand, bw.Resolve
	bw.RunCommand = func(_ context.Context, _ string, args []string, _ []string) ([]byte, error) {
		js := ""
		for i, a := range args {
			if a == "-c" && i+1 < len(args) {
				js = args[i+1]
			}
		}
		calls = append(calls, js)
		if runErr != nil {
			return nil, runErr
		}
		return []byte(out), nil
	}
	bw.Resolve = func() (string, error) { return "/usr/bin/fake-betterwright", nil }
	t.Cleanup(func() { bw.RunCommand, bw.Resolve = origRun, origResolve })
	return &calls
}

func TestBWSearchEngineParsesResults(t *testing.T) {
	fakeBWDaemon(t, `{"ok":true,"result":[
		{"url":"https://example.com/a","title":"First Hit","snippet":"about a"},
		{"url":"https://example.com/b","title":"Second Hit","snippet":"about b"}
	]}`, nil)
	e := newBWSearchEngine()
	resp, err := e.Search(t.Context(), "test query", searchOptions{maxResults: 5})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, 1, resp.Results[0].rank)
	assert.Equal(t, "https://example.com/a", resp.Results[0].url)
	assert.Equal(t, "example.com", resp.Results[0].host)
	require.Len(t, resp.Sources, 2)
	assert.Equal(t, "First Hit", resp.Sources[0].Title)
	assert.NotEmpty(t, resp.Sources[0].FaviconURL)
}

func TestBWSearchEngineEmptyResultsError(t *testing.T) {
	fakeBWDaemon(t, `{"ok":true,"result":[]}`, nil)
	e := newBWSearchEngine()
	_, err := e.Search(t.Context(), "query", searchOptions{maxResults: 5})
	require.Error(t, err, "zero parsed results must error so the router falls through")
}

// stubEngine is a scripted SearchEngine for router-ordering tests.
type stubEngine struct {
	name    string
	matches bool
	results int
	err     error
}

func (s *stubEngine) Name() string        { return s.name }
func (s *stubEngine) Matches(string) bool { return s.matches }
func (s *stubEngine) Search(context.Context, string, searchOptions) (*SearchResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &SearchResponse{Results: make([]searchResult, s.results)}, nil
}

func TestRouterFallsThroughFallbacksInOrder(t *testing.T) {
	dead := &stubEngine{name: "dead", err: fmt.Errorf("captcha")}
	live := &stubEngine{name: "live", results: 2}
	r := &searchRouter{fallbacks: []SearchEngine{dead, live}}
	resp, err := r.Search(t.Context(), "anything", searchOptions{})
	require.NoError(t, err)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "live", resp.Engine, "a failing fallback falls through to the next")
}

func TestRouterWithCatchAllEngineRunsLast(t *testing.T) {
	injected := &stubEngine{name: "injected", results: 1}
	dead := &stubEngine{name: "dead", err: fmt.Errorf("nope")}
	r := newSearchRouter().WithCatchAllEngine(dead).WithCatchAllEngine(injected)
	// The injected engines must sit AFTER the built-in fallbacks: the
	// first fallback is the merged web engine (a live network engine —
	// asserting on order rather than executing keeps this test hermetic
	// on machines where DDG answers any query).
	require.NotEmpty(t, r.fallbacks)
	last := r.fallbacks[len(r.fallbacks)-1]
	assert.Equal(t, "injected", last.Name(), "WithCatchAllEngine appends at the end")
	if len(r.fallbacks) >= 2 {
		assert.Equal(t, "dead", r.fallbacks[len(r.fallbacks)-2].Name())
	}
	// The first built-in fallback is the merged web engine.
	_, isMerged := r.fallbacks[0].(*mergedEngine)
	assert.True(t, isMerged, "first fallback should be the merged web engine")
}
