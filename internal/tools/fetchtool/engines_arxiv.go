package fetchtool

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"
)

// arxivEngine searches the arXiv API
// (http://export.arxiv.org/api/query?search_query=...). Free, no key,
// no captcha. Returns an Atom XML feed with paper titles, summaries,
// authors, and PDF links. Best for "research on X", "state of the art
// in Y", "paper about Z" — the rare-but-important academic query.
//
// arXiv covers physics, math, CS, biology, finance, statistics, and
// EE — the STEM research corpus. Not a general web search engine,
// but when a coding agent needs to understand a research-level
// concept (transformers, residual networks, BLAS, etc.) arXiv is the
// authoritative source.
type arxivEngine struct{}

func newArxivEngine() *arxivEngine { return &arxivEngine{} }

func (arxivEngine) Name() string { return "arxiv" }

// academicSignalRe matches queries that look like academic / research
// requests. The signal words are unambiguous when they appear:
//   - "research on X"
//   - "state of the art in Y"
//   - "paper about Z"
//   - "arxiv" (explicit reference)
//   - "preprint", "literature review", "peer-reviewed"
var academicSignalRe = regexp.MustCompile(`(?i)\b(research|paper|papers|preprint|preprints|literature|study|studies|survey|state[- ]of[- ]the[- ]art|peer[- ]reviewed|citation|arxiv|arXiv|scholarly|academic|thesis|dissertation)\b`)

func (arxivEngine) Matches(query string) bool {
	return academicSignalRe.MatchString(query)
}

// arxivFeed is the Atom XML root element returned by
// http://export.arxiv.org/api/query?search_query=...
type arxivFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}

// arxivEntry is one paper in the arXiv response.
type arxivEntry struct {
	Title     string       `xml:"title"`
	Summary   string       `xml:"summary"`
	Updated   string       `xml:"updated"`   // "2024-01-15T00:00:00Z"
	Published string       `xml:"published"` // "2024-01-10T00:00:00Z"
	ID        string       `xml:"id"`        // arXiv URL: "http://arxiv.org/abs/2401.12345v1"
	Authors   []arxivAuthor `xml:"author"`
	Links     []arxivLink  `xml:"link"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

type arxivLink struct {
	HRef string `xml:"href,attr"`
	Type string `xml:"type,attr"`   // "text/html" or "application/pdf"
	Title string `xml:"title,attr"` // "pdf" for the PDF link
}

func (e *arxivEngine) Search(ctx context.Context, query string, opts searchOptions) (*SearchResponse, error) {
	maxResults := opts.maxResults
	if maxResults <= 0 {
		maxResults = searchDefaultMaxResults
	}
	// arXiv search uses the all: prefix for full-text search.
	endpoint := fmt.Sprintf("http://export.arxiv.org/api/query?search_query=all:%s&start=0&max_results=%d",
		url.QueryEscape(strings.TrimSpace(query)), maxResults)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	client := &http.Client{Timeout: 20 * time.Second} // arXiv can be slow
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arxiv: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("arxiv: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("arxiv: parse: %w", err)
	}

	results := make([]searchResult, 0, len(feed.Entries))
	sources := make([]tooldef.Source, 0, len(feed.Entries))
	for i, entry := range feed.Entries {
		// Build a readable author list: "Smith, J.; Lee, K.; Patel, R."
		authorNames := make([]string, 0, len(entry.Authors))
		for _, a := range entry.Authors {
			authorNames = append(authorNames, a.Name)
		}
		authors := strings.Join(authorNames, ", ")
		if len(authors) > 80 {
			authors = authors[:77] + "..."
		}
		// The arXiv entry ID is the abstract URL; we look for a PDF link.
		// Fall back to the ID itself (which links to the abstract page).
		pdfURL := entry.ID
		for _, link := range entry.Links {
			if link.Title == "pdf" || strings.HasSuffix(link.HRef, ".pdf") {
				pdfURL = link.HRef
				break
			}
		}
		// Snippet: published date + author list + summary (truncated).
		summary := collapseWhitespace(entry.Summary)
		summary = truncate(summary, 300)
		snippet := fmt.Sprintf("Published: %s · Authors: %s · %s",
			entry.Published[:10], authors, summary)
		host := "arxiv.org"
		rank := i + 1
		title := collapseWhitespace(entry.Title)
		results = append(results, searchResult{
			rank:    rank,
			url:     pdfURL,
			title:   title,
			snippet: snippet,
		})
		sources = append(sources, tooldef.Source{
			Rank:       rank,
			URL:        pdfURL,
			Title:      title,
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
