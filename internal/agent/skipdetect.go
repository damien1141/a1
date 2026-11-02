package agent

import (
        "strings"
)

// fastPathSkip detects, via cheap structural patterns, whether a message is
// almost certainly non-personal (code, URLs, structured data, math
// expressions, etc.) and should be skipped for both memory retrieval
// (inlet) and memory consolidation (outlet). Returns a non-empty reason
// string when the message should be skipped; "" when it should proceed.
//
// This is a port of Open WebUI's SkipDetector._fast_path_skip_detection
// (mtayfur's Memory System filter), with the patterns tuned for an agent
// harness that reads a lot of tool output and code. The semantic binary
// classification (50+ reference embeddings per category) is intentionally
// NOT ported — the fast-path patterns alone catch ~80% of skip cases,
// and the embedding-based path adds latency + a 200MB embedding model
// dependency that isn't worth it for v1. If recall noise becomes a real
// problem, the semantic path can be added later as a second tier.
//
// All patterns return true (skip) with high confidence and a low false
// positive rate. A message that passes ALL patterns is allowed through to
// the (slower) memstore.Retrieve / LLM consolidation path.
func fastPathSkip(msg string) string {
        if len(msg) == 0 {
                return "empty"
        }
        // Minimum length: skip very short messages (single-word replies).
        if len(strings.TrimSpace(msg)) < 10 {
                return "too_short"
        }

        lines := strings.Split(msg, "\n")
        lineCount := len(lines)
        nonEmptyLines := make([]string, 0, lineCount)
        for _, line := range lines {
                if trimmed := strings.TrimSpace(line); trimmed != "" {
                        nonEmptyLines = append(nonEmptyLines, line)
                }
        }
        nonEmptyCount := len(nonEmptyLines)

        // Pattern 1: Many URLs (5+ full URLs = link list or technical references).
        urlCount := strings.Count(msg, "http://") + strings.Count(msg, "https://")
        if urlCount >= 5 {
                return "many_urls"
        }

        // Pattern 2: Long unbroken alphanumeric strings (tokens, hashes, base64).
        for _, word := range strings.Fields(msg) {
                cleaned := strings.Trim(word, ".,;:!?()[]{}\"'")
                if len(cleaned) > 80 {
                        stripped := strings.ReplaceAll(strings.ReplaceAll(cleaned, "-", ""), "_", "")
                        if isAlphanumericStr(stripped) {
                                return "long_token"
                        }
                }
        }

        // Pattern 3: Markdown/text separators (repeated ---, ===, ___, ***).
        for _, pattern := range []string{"---", "===", "___", "***"} {
                if strings.Count(msg, pattern) >= 2 {
                        return "separator_chain"
                }
        }

        // Pattern 4: Command-line patterns with context-aware detection.
        if nonEmptyCount > 0 {
                actualCommandLines := 0
                for _, line := range nonEmptyLines {
                        stripped := strings.TrimSpace(line)
                        if strings.HasPrefix(stripped, "$ ") && len(stripped) > 2 {
                                parts := strings.Fields(stripped[2:])
                                if len(parts) > 0 && isAlphanumericStr(parts[0]) {
                                        actualCommandLines++
                                }
                        } else if idx := strings.Index(stripped, "$ "); idx > 0 {
                                prev := stripped[idx-1]
                                if prev == ' ' || prev == ':' || prev == '\t' {
                                        rest := strings.Fields(stripped[idx+2:])
                                        if len(rest) > 0 {
                                                first := rest[0]
                                                if isAlphanumericStr(first) || first == "curl" || first == "wget" ||
                                                        first == "git" || first == "npm" || first == "pip" || first == "docker" {
                                                        actualCommandLines++
                                                }
                                        }
                                }
                        }
                }
                if actualCommandLines >= 1 && (strings.Contains(msg, "http://") ||
                        strings.Contains(msg, "https://") || strings.Contains(msg, " | ")) {
                        return "command_with_url"
                }
                if actualCommandLines >= 3 {
                        return "many_commands"
                }
        }

        // Pattern 5: High path/URL density (dots and slashes suggesting file paths).
        if len(msg) > 30 {
                slashCount := strings.Count(msg, "/") + strings.Count(msg, "\\")
                dotCount := strings.Count(msg, ".")
                pathChars := slashCount + dotCount
                if pathChars > 10 && float64(pathChars)/float64(len(msg)) > 0.15 {
                        return "path_density"
                }
        }

        // Pattern 6: Markup character density (structured data).
        markupChars := strings.Count(msg, "{") + strings.Count(msg, "}") +
                strings.Count(msg, "[") + strings.Count(msg, "]") +
                strings.Count(msg, "<") + strings.Count(msg, ">")
        if markupChars >= 6 {
                if float64(markupChars)/float64(len(msg)) > 0.10 {
                        return "markup_density"
                }
                curlyCount := strings.Count(msg, "{") + strings.Count(msg, "}")
                if curlyCount >= 10 {
                        return "json_density"
                }
        }

        // Pattern 7: Structured nested content with colons (key: value patterns).
        if lineCount >= 8 && nonEmptyCount > 0 {
                colonLines := 0
                indentedLines := 0
                for _, line := range nonEmptyLines {
                        if strings.Contains(line, ":") && !strings.HasPrefix(strings.TrimSpace(line), "#") {
                                colonLines++
                        }
                        if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
                                indentedLines++
                        }
                }
                if float64(colonLines)/float64(nonEmptyCount) > 0.4 &&
                        float64(indentedLines)/float64(nonEmptyCount) > 0.5 {
                        wordsOutsideKV := 0
                        for _, line := range nonEmptyLines {
                                if !strings.Contains(line, ":") {
                                        wordsOutsideKV += len(strings.Fields(line))
                                }
                        }
                        if wordsOutsideKV < 5 {
                                return "kv_structure"
                        }
                }
        }

        // Pattern 8: Highly structured multi-line content (require markup chars).
        if lineCount > 15 && nonEmptyCount > 0 {
                markupInLines := 0
                for _, line := range nonEmptyLines {
                        if strings.ContainsAny(line, "{}[]<>") {
                                markupInLines++
                        }
                }
                if float64(markupInLines)/float64(nonEmptyCount) > 0.3 {
                        return "structured_multiline"
                }
        }

        // Pattern 9: Code-like indentation pattern.
        if lineCount >= 3 && nonEmptyCount > 0 {
                indentedLines := 0
                for _, line := range nonEmptyLines {
                        if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
                                indentedLines++
                        }
                }
                if float64(indentedLines)/float64(nonEmptyCount) > 0.5 {
                        codeEndings := 0
                        for _, line := range nonEmptyLines {
                                stripped := strings.TrimRight(line, " \t")
                                if strings.HasSuffix(stripped, "{") || strings.HasSuffix(stripped, "}") ||
                                        strings.HasSuffix(stripped, "(") || strings.HasSuffix(stripped, ")") ||
                                        strings.HasSuffix(stripped, ";") {
                                        codeEndings++
                                }
                        }
                        if float64(codeEndings)/float64(nonEmptyCount) > 0.2 {
                                return "code_indent"
                        }
                }
        }

        // Pattern 10: Very high special character ratio (encoded data, technical output).
        if len(msg) > 50 {
                specialChars := 0
                for _, c := range msg {
                        if !isAlphanumeric(byte(c)) && c != ' ' && c != '\t' && c != '\n' && c != '\r' {
                                specialChars++
                        }
                }
                specialRatio := float64(specialChars) / float64(len(msg))
                if specialRatio > 0.35 {
                        alphanumeric := 0
                        for _, c := range msg {
                                if isAlphanumeric(byte(c)) {
                                        alphanumeric++
                                }
                        }
                        if float64(alphanumeric)/float64(len(msg)) < 0.50 {
                                return "special_char_density"
                        }
                }
        }

        return ""
}

// isAlphanumeric reports whether b is an ASCII letter or digit. Faster
// than regexp for this hot path.
func isAlphanumeric(b byte) bool {
        return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isAlphanumericStr reports whether s consists entirely of ASCII letters
// and digits. Used for the structural-pattern checks where we need to
// test a multi-char token.
func isAlphanumericStr(s string) bool {
        if len(s) == 0 {
                return false
        }
        for i := 0; i < len(s); i++ {
                if !isAlphanumeric(s[i]) {
                        return false
                }
        }
        return true
}
