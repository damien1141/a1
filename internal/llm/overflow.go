package llm

import (
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
