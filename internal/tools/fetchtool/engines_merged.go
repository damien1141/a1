package fetchtool

import (
	"context"
	"strings"
	"sync"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// mergedEngine runs several engines in parallel and interleaves their
// results into one response — the "more sources per query" fallback. The
// primary engine (DuckDuckGo Lite, a full web index) contributes two
// results per merge round; each secondary (Hacker News, Reddit — the
// community view) contributes one, so the merged list stays web-dominant
// while the source sidebar gains hosts a single engine never surfaces.
//
// Primary contract: when the PRIMARY engine errors (DDG's captcha wall,
// a network failure), the merge errors with it — a response built from
// community results alone must not mask a dead web index, because the
// router's next fallback is the BetterWright browser engine, whose whole
// job is surviving that captcha. Secondaries erroring is fine; they just
// drop out of the merge.
//
// Page fetching happens exactly once, on the merged survivors: children
// run with fetchPages disabled and the merged engine performs the
// parallel page fetch itself, so pages are never fetched for results the
// interleave then drops.
type mergedEngine struct {
	primary SearchEngine
	others  []SearchEngine
}

// newMergedWebEngine builds the default general-web fallback:
// DuckDuckGo Lite as primary, Hacker News and Reddit as community
// secondaries. Reddit refuses datacenter IPs — that error is swallowed
// and the merge proceeds with what answered.
func newMergedWebEngine() *mergedEngine {
	return &mergedEngine{
		primary: newDDGLiteEngine(),
		others: []SearchEngine{
			newHackerNewsEngine(),
			newRedditEngine(),
		},
	}
}

func (m *mergedEngine) Name() string { return "web" }

// Matches returns true: the merged engine is a fallback the router calls
// directly, never a domain match. (SearchEngine implementations that
// return false are skipped by the router's Matches() loop; this one is
// only ever in the fallbacks list, where the router calls Search
// unconditionally — Matches is satisfied for interface completeness.)
func (m *mergedEngine) Matches(_ string) bool { return true }

// mergedOutcome captures one child engine's parallel result.
type mergedOutcome struct {
	eng  SearchEngine
	resp *SearchResponse
	err  error
}

func (m *mergedEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	all := make([]SearchEngine, 0, 1+len(m.others))
	all = append(all, m.primary)
	all = append(all, m.others...)

	// Children never fetch pages — the merge picks the survivors first.
	childOpts := opts
	childOpts.fetchPages = false

	outs := make([]mergedOutcome, len(all))
	var wg sync.WaitGroup
	for i, eng := range all {
		wg.Add(1)
		go func(i int, eng SearchEngine) {
			defer wg.Done()
			resp, err := eng.Search(ctx, query, childOpts)
			outs[i] = mergedOutcome{eng: eng, resp: resp, err: err}
		}(i, eng)
	}
	wg.Wait()

	// Primary must succeed — see the type comment for why its error
	// propagates instead of being merged around.
	if outs[0].err != nil {
		return nil, outs[0].err
	}

	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}

	// Weighted interleave with URL dedupe. cursors[i] is how many of
	// engine i's results have been consumed; round r takes two from the
	// primary and one from each secondary until maxResults is reached or
	// every engine is exhausted.
	cursors := make([]int, len(outs))
	results := make([]searchResult, 0, maxResults)
	sources := make([]tooldef.Source, 0, maxResults)
	contributors := make([]string, 0, len(outs))
	contributed := make([]bool, len(outs))
	seen := make(map[string]bool)

	take := func(i, n int) {
		if outs[i].resp == nil {
			return
		}
		res := outs[i].resp.Results
		for k := 0; k < n && cursors[i] < len(res) && len(results) < maxResults; k++ {
			r := res[cursors[i]]
			idx := cursors[i]
			cursors[i]++
			if r.url == "" || seen[r.url] {
				continue
			}
			seen[r.url] = true
			rank := len(results) + 1
			r.rank = rank
			results = append(results, r)
			if idx < len(outs[i].resp.Sources) {
				s := outs[i].resp.Sources[idx]
				s.Rank = rank
				sources = append(sources, s)
			} else {
				sources = append(sources, sourceFromResult(r, rank))
			}
			if !contributed[i] {
				contributed[i] = true
				contributors = append(contributors, outs[i].eng.Name())
			}
		}
	}

	exhausted := func() bool {
		for i := range outs {
			if outs[i].resp != nil && cursors[i] < len(outs[i].resp.Results) {
				return false
			}
		}
		return true
	}
	for len(results) < maxResults && !exhausted() {
		take(0, 2) // primary weight: two per round
		for i := 1; i < len(outs); i++ {
			take(i, 1)
		}
	}

	if len(results) == 0 {
		// Nothing merged (primary empty, secondaries empty or blocked).
		// Return an empty response — the router treats it as "this
		// fallback produced nothing" and tries the next one.
		return &SearchResponse{Query: query}, nil
	}

	// Fetch pages for the merged survivors only, once.
	if opts.fetchPages && len(sources) > 0 {
		fetchResultPages(ctx, sources, opts.maxCharsPerPage)
	}

	engine := strings.Join(contributors, "+")
	if engine == "" {
		engine = m.Name()
	}
	return &SearchResponse{
		Engine:  engine,
		Query:   query,
		Results: results,
		Sources: sources,
	}, nil
}

// sourceFromResult builds a UI source card from a bare searchResult —
// used when a merged child returned Results without aligned Sources.
// Mirrors the fallback path of SearchResponse.toResult.
func sourceFromResult(res searchResult, rank int) tooldef.Source {
	host := hostOf(res.url)
	host = strings.TrimPrefix(host, "www.")
	return tooldef.Source{
		Rank:       rank,
		URL:        res.url,
		Title:      truncate(res.title, 200),
		Host:       host,
		FaviconURL: formatFaviconURL(host),
		Snippet:    truncate(res.snippet, 400),
	}
}
