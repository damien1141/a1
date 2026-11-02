package fetchtool

import (
        "context"
        "errors"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm/jev"
)

// fakeFetchJevAsker scripts the classifier's System One call.
type fakeFetchJevAsker struct {
        resp  jev.Response
        err   error
        calls int
}

func (f *fakeFetchJevAsker) SystemOneWithOptions(
        _ context.Context, _ any, _ jev.Questions, _ jev.Options,
) (jev.Response, error) {
        f.calls++
        return f.resp, f.err
}

func choiceAnswer(choice string, confidence float64) jev.Answer {
        return jev.Answer{
                Type:         jev.AnswerChoice,
                Choice:       choice,
                Confidence:   confidence,
                Probabilities: map[string]float64{choice: confidence},
        }
}

// stubRouterEngine is a deterministic engine for router dispatch tests.
type stubRouterEngine struct {
        name    string
        matches bool
        empty   bool
        err     error
        ran     int
}

func (s *stubRouterEngine) Name() string { return s.name }

func (s *stubRouterEngine) Matches(string) bool { return s.matches }

func (s *stubRouterEngine) Search(_ context.Context, query string, _ searchOptions) (*SearchResponse, error) {
        s.ran++
        if s.err != nil {
                return nil, s.err
        }
        if s.empty {
                return &SearchResponse{}, nil
        }
        return &SearchResponse{
                Engine:  s.name,
                Results: []searchResult{{rank: 1, url: "https://example.com/" + s.name, title: query}},
        }, nil
}

// TestJevClassifierChoice: a known choice comes back; unknown choices,
// errors, and mistyped answers fall back.
func TestJevClassifierChoice(t *testing.T) {
        t.Run("known choice", func(t *testing.T) {
                asker := &fakeFetchJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                        "engine": choiceAnswer("wikipedia", 0.9),
                }}}
                c := &jevClassifier{asker: asker, timeout: jevClassifyTimeout}
                name, ok := c.classify(t.Context(), "what is a monad")
                require.True(t, ok)
                assert.Equal(t, "wikipedia", name)
                assert.Equal(t, 1, asker.calls)
        })

        t.Run("unknown choice rejected", func(t *testing.T) {
                asker := &fakeFetchJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                        "engine": choiceAnswer("altavista", 0.9),
                }}}
                c := &jevClassifier{asker: asker, timeout: jevClassifyTimeout}
                _, ok := c.classify(t.Context(), "anything")
                assert.False(t, ok, "an unregistered engine name must fall back to the predicates")
        })

        t.Run("error falls back", func(t *testing.T) {
                c := &jevClassifier{
                        asker:   &fakeFetchJevAsker{err: errors.New("typesafe API error (503)")},
                        timeout: jevClassifyTimeout,
                }
                _, ok := c.classify(t.Context(), "anything")
                assert.False(t, ok)
        })

        t.Run("missing answer falls back", func(t *testing.T) {
                c := &jevClassifier{asker: &fakeFetchJevAsker{}, timeout: jevClassifyTimeout}
                _, ok := c.classify(t.Context(), "anything")
                assert.False(t, ok)
        })

        t.Run("nil classifier and empty query are safe", func(t *testing.T) {
                var c *jevClassifier
                _, ok := c.classify(t.Context(), "q")
                assert.False(t, ok)
                cc := &jevClassifier{asker: &fakeFetchJevAsker{}, timeout: jevClassifyTimeout}
                _, ok = cc.classify(t.Context(), "")
                assert.False(t, ok)
        })
}

// TestRouterJevFirstDibs: the decision model's pick dispatches before the
// predicate order. Stub engines carry real criteria names — the classifier
// validates choices against the registered engine set.
func TestRouterJevFirstDibs(t *testing.T) {
        se := &stubRouterEngine{name: "stackexchange", matches: true}
        reddit := &stubRouterEngine{name: "reddit"}
        r := &searchRouter{
                engines: []SearchEngine{se, reddit},
                jev: &jevClassifier{
                        asker: &fakeFetchJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                                "engine": choiceAnswer("reddit", 0.9),
                        }}},
                        timeout: jevClassifyTimeout,
                },
        }
        resp, err := r.Search(t.Context(), "is bun production ready", searchOptions{})
        require.NoError(t, err)
        require.NotNil(t, resp)
        assert.Equal(t, "reddit", resp.Engine)
        assert.Equal(t, 1, reddit.ran)
        assert.Equal(t, 0, se.ran, "the predicate engine must not run when jev's pick produced results")
}

// TestRouterJevPickCanReachFallbackEngines: "merged" is a valid choice —
// the general web engine runs directly.
func TestRouterJevPickCanReachFallbackEngines(t *testing.T) {
        merged := &stubRouterEngine{name: "merged"}
        se := &stubRouterEngine{name: "stackexchange", matches: true}
        r := &searchRouter{
                engines:   []SearchEngine{se},
                fallbacks: []SearchEngine{merged},
                jev: &jevClassifier{
                        asker: &fakeFetchJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                                "engine": choiceAnswer("merged", 0.9),
                        }}},
                        timeout: jevClassifyTimeout,
                },
        }
        resp, err := r.Search(t.Context(), "best pizza in new york", searchOptions{})
        require.NoError(t, err)
        assert.Equal(t, "merged", resp.Engine)
        assert.Equal(t, 1, merged.ran)
        assert.Equal(t, 0, se.ran)
}

// TestRouterJevEmptyPickFallsToPredicates: a jev pick that produces
// nothing hands control back to the predicate order.
func TestRouterJevEmptyPickFallsToPredicates(t *testing.T) {
        se := &stubRouterEngine{name: "stackexchange", matches: true}
        reddit := &stubRouterEngine{name: "reddit", empty: true}
        r := &searchRouter{
                engines: []SearchEngine{se, reddit},
                jev: &jevClassifier{
                        asker: &fakeFetchJevAsker{resp: jev.Response{Answers: map[string]jev.Answer{
                                "engine": choiceAnswer("reddit", 0.9),
                        }}},
                        timeout: jevClassifyTimeout,
                },
        }
        resp, err := r.Search(t.Context(), "anything", searchOptions{})
        require.NoError(t, err)
        assert.Equal(t, "stackexchange", resp.Engine)
        assert.Equal(t, 1, reddit.ran)
        assert.Equal(t, 1, se.ran)
}

// TestRouterJevUnconfiguredIsPredicatesOnly: a nil classifier (no
// TYPESAFE_API_KEY) leaves the router byte-for-byte on the predicate path.
func TestRouterJevUnconfiguredIsPredicatesOnly(t *testing.T) {
        se := &stubRouterEngine{name: "stackexchange", matches: true}
        r := &searchRouter{engines: []SearchEngine{se}}
        resp, err := r.Search(t.Context(), "anything", searchOptions{})
        require.NoError(t, err)
        assert.Equal(t, "stackexchange", resp.Engine)
        assert.Equal(t, 1, se.ran)
}

// TestNewJevClassifierNilWithoutKey: the whole jev surface is opt-in.
func TestNewJevClassifierNilWithoutKey(t *testing.T) {
        t.Setenv("TYPESAFE_API_KEY", "")
        assert.Nil(t, newJevClassifier())
}
