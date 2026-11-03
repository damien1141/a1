package llm

import (
        "net/http"
        "regexp"
        "strings"
)

// Provider error text that means the prompt exceeded the model context window.
var overflowPatterns = []*regexp.Regexp{
        regexp.MustCompile(`(?i)prompt is too long`),
        regexp.MustCompile(`(?i)request_too_large`),
        regexp.MustCompile(`(?i)input is too long for requested model`),
        regexp.MustCompile(`(?i)exceeds the context window`),
        regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length`),
        regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
        regexp.MustCompile(`(?i)maximum prompt length is \d+`),
        regexp.MustCompile(`(?i)reduce the length of the messages`),
        regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
        regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length`),
        regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length`),
        regexp.MustCompile(`(?i)exceeds the limit of \d+`),
        regexp.MustCompile(`(?i)exceeds the available context size`),
        regexp.MustCompile(`(?i)greater than the context length`),
        regexp.MustCompile(`(?i)context window exceeds limit`),
        regexp.MustCompile(`(?i)exceeded model token limit`),
        regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
        regexp.MustCompile(`(?i)model_context_window_exceeded`),
        regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
        regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
        regexp.MustCompile(`(?i)exceeds (?:the )?context length`),
        regexp.MustCompile(`(?i)exceeds (?:the )?context size`),
        regexp.MustCompile(`(?i)exceeds (?:the )?context limit`),
        regexp.MustCompile(`(?i)token limit exceeded`),
        regexp.MustCompile(`(?i)too many tokens`),
        regexp.MustCompile(`(?i)(?:request body|request entity|payload) too large`),
}

// Errors that can match a broad overflow pattern but are not overflows
// (e.g. throttling that says "too many tokens").
var nonOverflowPatterns = []*regexp.Regexp{
        regexp.MustCompile(`(?i)^(Throttling error|Service unavailable):`),
        regexp.MustCompile(`(?i)rate limit`),
        regexp.MustCompile(`(?i)too many requests`),
        regexp.MustCompile(`(?i)please wait before trying again`),
}

// IsContextOverflow reports whether err is a provider context-window overflow.
// Nil and empty messages are false.
func IsContextOverflow(err error) bool {
        if err == nil {
                return false
        }
        msg := strings.TrimSpace(err.Error())
        if msg == "" {
                return false
        }
        for _, p := range nonOverflowPatterns {
                if p.MatchString(msg) {
                        return false
                }
        }
        for _, p := range overflowPatterns {
                if p.MatchString(msg) {
                        return true
                }
        }
        return false
}

// opaqueProviderErrorPatterns match error bodies that explain nothing —
// gateways and proxies that swallow the upstream failure reason and answer
// with a bare 400 ("Provider returned error"). Anchored full-matches only:
// an informative message that merely contains the word "provider" must not
// land here (those either match a real overflow pattern above or stay
// ordinary errors).
var opaqueProviderErrorPatterns = []*regexp.Regexp{
        regexp.MustCompile(`(?i)^provider returned (?:an )?error[.!]?$`),
        regexp.MustCompile(`(?i)^provider error[.!]?$`),
        regexp.MustCompile(`(?i)^upstream(?: provider)? error[.!]?$`),
        regexp.MustCompile(`(?i)^gateway error[.!]?$`),
        regexp.MustCompile(`(?i)^bad request[.!]?$`),
        regexp.MustCompile(`(?i)^invalid request[.!]?$`),
}

// IsSuspectedOverflow reports whether err is a provider refusal that could
// be a context overflow but cannot be proven one: a 413 (a too-large
// condition by definition, whatever the body says) or a 400 whose body
// carries no diagnostic information at all.
//
// These are the errors that brick long sessions behind an unhelpful
// gateway: every retry re-sends the same oversized history, fails with the
// same opaque text, and IsContextOverflow never fires because there is
// nothing to match. The remedy is the same force-compact-and-retry recovery
// a proven overflow gets — attempted as a guess, with the original error
// still surfacing when shrinking does not help. A suspected overflow is
// never ground truth for the persistent window: an unexplained 400 does not
// say what the limit is, and a transient gateway blip must not under-fold
// every later turn.
func IsSuspectedOverflow(err error) bool {
        if err == nil {
                return false
        }
        if IsContextOverflow(err) {
                return false
        }
        apiErr, ok := APIErrorOf(err)
        if !ok {
                return false
        }
        msg := strings.TrimSpace(apiErr.Message)
        if msg == "" {
                return false
        }
        for _, p := range nonOverflowPatterns {
                if p.MatchString(msg) {
                        return false
                }
        }
        if apiErr.Status == http.StatusRequestEntityTooLarge {
                // 413 means the request body exceeded a size limit. Whatever
                // the body says, shrinking the request is the only lever.
                return true
        }
        if apiErr.Status != http.StatusBadRequest {
                return false
        }
        for _, p := range opaqueProviderErrorPatterns {
                if p.MatchString(msg) {
                        return true
                }
        }
        return false
}
