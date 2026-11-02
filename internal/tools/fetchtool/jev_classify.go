package fetchtool

import (
        "context"
        "time"

        "github.com/damien1141/a1/internal/debuglog"
        "github.com/damien1141/a1/internal/llm/jev"
)

// JEV engine classification: the decision model picks the search engine.
//
// The router's Matches() predicates are hand-tuned regexes with a deliberate
// priority order (see newSearchRouter). They are fast and keyless, but they
// read keywords, not meaning: "is bun production ready" names a runtime and
// lands on the package registry instead of the forums. A JEV model — one
// state, one Choice question, no text generation — reads the query.
//
// Discipline (the user's rule: jev is optional, never load-bearing alone):
// the classifier is nil unless a decision model is configured (the jev:
// section of config.yaml — api or ollama — or the TYPESAFE_API_KEY
// environment fallback); every error, timeout, unknown choice, or empty
// answer falls through to the untouched predicate order; and the
// JEV-chosen engine still has to produce results or the router proceeds
// exactly as before.

// jevClassifyTimeout bounds one classify call against the hosted API.
// Search is on the tool hot path; a wedged endpoint must not add
// unbounded latency to every fetch. The Ollama provider raises this
// ceiling (a local model can take seconds), and jev.timeout_sec overrides
// both — see jev.EffectiveTimeout.
const jevClassifyTimeout = 3 * time.Second

// jevAsker is the seam over the jev backends the tests fake.
type jevAsker = jev.Asker

// jevClassifier asks the decision model which engine should handle a query.
type jevClassifier struct {
        asker   jevAsker
        timeout time.Duration
}

// newJevClassifier returns nil unless a decision model is configured
// (jev: section in config.yaml — api or ollama — or the TYPESAFE_API_KEY
// fallback). Nil means "predicates only" — the router treats it as absent.
// The asker resolves its backend per call, so a config edit between
// searches applies to the next one.
func newJevClassifier() *jevClassifier {
        if !jev.Configured() {
                return nil
        }
        return &jevClassifier{asker: jev.NewConfiguredAsker(), timeout: jev.EffectiveTimeout(jevClassifyTimeout)}
}

// jevEngineCriteria maps every router-selectable engine to its Choice
// criteria. The merged web engine is the general option; the BetterWright
// browser fallback is deliberately absent — it is an emergency path, not a
// routing choice.
var jevEngineCriteria = map[string]any{
        "frankfurter":   "Currency conversion between named ISO codes (e.g. USD to EUR, '100 dollars in yen'). Never anything else.",
        "mdn":           "Web-platform reference: CSS properties, HTML elements/attributes, DOM APIs, SVG, browser behavior, JavaScript language semantics as documented for the browser.",
        "npm":           "JavaScript/TypeScript package discovery — finding, choosing, or comparing libraries from the npm registry.",
        "crates":        "Rust crate discovery — finding, choosing, or comparing libraries from crates.io.",
        "github":        "Finding repositories, projects, or source code on GitHub; library discovery for ecosystems without their own registry engine above.",
        "reddit":        "Community opinion, experience reports, and discussions — 'what do people think', recommendations, pros/cons from practitioners. Explicit reddit asks.",
        "hackernews":    "Practitioner discussion and tech-industry commentary; launch/news threads; 'what does HN think'.",
        "stackexchange": "Programming Q&A: concrete technical problems with right answers — errors, APIs, algorithms, configuration, language usage.",
        "arxiv":         "Academic papers and research preprints.",
        "wikipedia":     "Encyclopedic facts: concepts, people, history, science, definitions — general-knowledge lookup.",
        "merged":        "General web search plus community threads — anything not clearly served by a specific engine above.",
}

// classify asks one Choice question and returns the chosen engine name.
// ok is false on error, timeout, a missing/mistyped answer, or a choice
// outside jevEngineCriteria — every such case falls back to the predicates.
func (c *jevClassifier) classify(ctx context.Context, query string) (string, bool) {
        if c == nil || c.asker == nil || query == "" {
                return "", false
        }
        qCtx, cancel := context.WithTimeout(ctx, c.timeout)
        defer cancel()
        resp, err := c.asker.SystemOneWithOptions(qCtx, query, jev.Questions{
                "engine": jev.Choice{
                        Instructions: "Pick the single best source to answer this search query. Prefer the most authoritative specific source over a general web search; prefer community sources when the query asks for opinions or experience; prefer reference docs over forums for 'what is / how does X work' web-platform questions.",
                        Criteria:     jevEngineCriteria,
                },
        }, jev.Options{})
        if err != nil {
                debuglog.Logf("fetch: jev classify: %v", err)
                return "", false
        }
        ans, ok := resp.Answers["engine"]
        if !ok || ans.Type != jev.AnswerChoice {
                return "", false
        }
        if _, known := jevEngineCriteria[ans.Choice]; !known {
                debuglog.Logf("fetch: jev classify picked unknown engine %q", ans.Choice)
                return "", false
        }
        return ans.Choice, true
}
