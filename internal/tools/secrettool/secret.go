package secrettool

import (
        "context"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "regexp"
        "strings"

        "github.com/damien1141/a1/internal/tools/tooldef"

        "github.com/damien1141/a1/internal/llm"
)

const (
        secretDefaultLimit = 100
)

var secretDescription = `Scan files for common secret patterns: API keys, tokens, passwords, private keys, and credentials.

Returns file headers plus line references with the matched secret type.
Use glob to limit file types. Results are capped; increase limit or refine if truncated.`

// SecretTool returns the secret scanner tool definition + handler.
func SecretTool() tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name:        "secret",
                        Description: secretDescription,
                        Params: &llm.FunctionParameters{
                                Type: "object",
                                Properties: llm.Object{
                                        "path": llm.Object{
                                                "type":        "string",
                                                "description": "Directory or file to scan. Example: ./src",
                                        },
                                        "glob": llm.Object{
                                                "type":        "string",
                                                "description": "File pattern filter. Example: *.go",
                                        },
                                        "limit": llm.Object{
                                                "type": "integer",
                                                "description": fmt.Sprintf(
                                                        "Maximum matches to return. Example: 50 (default: %d)",
                                                        secretDefaultLimit,
                                                ),
                                        },
                                },
                                Required: []string{},
                        },
                        Readable: true,
                },
                DetailFromArgs: func(input json.RawMessage) string {
                        var in secretInput
                        _ = json.Unmarshal(input, &in)
                        p := strings.TrimSpace(in.Path)
                        if p == "" {
                                p = "."
                        }
                        return fmt.Sprintf("secret scan %s", p)
                },
                Run: runSecret,
        }
}

type secretInput struct {
        Path  string `json:"path,omitempty"`
        Glob  string `json:"glob,omitempty"`
        Limit int    `json:"limit,omitempty"`
}

// secretPatterns is ordered from MOST specific to LEAST specific. The scanner
// emits only the first pattern that matches a given line, so specific
// patterns (e.g. AWS access key, GitHub token) win over Generic secret when
// both would fire on the same input. The previous loop appended every
// matching pattern for every line — a line like `api_key = "sk-..."`
// matched both "API key" and "Generic secret" and got two entries.
var secretPatterns = []struct {
        name string
        re   *regexp.Regexp
        // ok is an optional post-filter applied to the captured value (the
        // LAST non-empty submatch of the regex, or the whole match if there
        // are no submatches). Returning false suppresses the match. Used to
        // reject obvious false positives — pure-hex hashes for Generic
        // secret, the literal word "password" as the captured value for
        // Password, etc.
        ok func(match string) bool
}{
        {"Private key", regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----`), nil},
        {"AWS access key", regexp.MustCompile(`(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`), nil},
        {"GitHub token", regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)[A-Za-z0-9_]{36}`), nil},
        {"Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]+`), nil},
        {"Bearer token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.]{20,}`), nil},
        {"AWS secret", regexp.MustCompile(`(?i)aws(.{0,20})?secret(.{0,20})?[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`), nil},
        {"API key", regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?([A-Za-z0-9_\-]{20,})['"]?`), nil},
        {
                name: "Password",
                re:   regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*['"]?([^\s'"]{8,})['"]?`),
                ok: func(match string) bool {
                        // Reject when the captured value is the keyword itself in
                        // disguise: `password = "password"`, `pwd: "p@ssw0rd"` —
                        // actually `p@ssw0rd` is plausible, leave that alone. We
                        // only reject the bare keyword forms.
                        lower := strings.ToLower(match)
                        switch lower {
                        case "password", "passwd", "pwd", "changeme", "secret", "token", "key", "test", "example", "placeholder", "redacted":
                                return false
                        }
                        return true
                },
        },
        {
                name: "Generic secret",
                re:   regexp.MustCompile(`(?i)(secret|token|key)\s*[:=]\s*['"]?([A-Za-z0-9_\-]{16,})['"]?`),
                ok: func(match string) bool {
                        // Pure-hex values of hash lengths (MD5/SHA-1/SHA-256/
                        // SHA-512) are almost always checksums, not secrets.
                        if isHexHash(match) {
                                return false
                        }
                        return true
                },
        },
}

// isHexHash reports whether s is a pure hex string of a common hash length.
// Used to suppress Generic-secret matches on MD5 (32), SHA-1 (40), SHA-256
// (64), and SHA-512 (128) checksums that frequently appear in lockfiles,
// content-addressed stores, and integrity checks.
func isHexHash(s string) bool {
        if len(s) != 32 && len(s) != 40 && len(s) != 64 && len(s) != 128 {
                return false
        }
        for i := 0; i < len(s); i++ {
                c := s[i]
                if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
                        return false
                }
        }
        return true
}

func runSecret(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
        var in secretInput
        if err := json.Unmarshal(input, &in); err != nil {
                return tooldef.Result{}, fmt.Errorf("failed to parse secret arguments: %w", err)
        }

        searchRel := strings.TrimSpace(in.Path)
        if searchRel == "" {
                searchRel = "."
        }
        searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
        if err != nil {
                return tooldef.Result{}, err
        }

        if _, err := os.Stat(searchPath); err != nil {
                return tooldef.Result{}, fmt.Errorf("path not found: %s. Check the path and try again", searchPath)
        }

        limit := in.Limit
        if limit <= 0 {
                limit = secretDefaultLimit
        }

        glob := strings.TrimSpace(in.Glob)

        matches, err := scanForSecrets(ctx, searchPath, glob, limit)
        if err != nil {
                return tooldef.Result{}, err
        }

        if len(matches) == 0 {
                return tooldef.Result{Content: "No secrets detected", Detail: "0 secrets", Output: "No secrets detected"}, nil
        }

        content := strings.Join(matches, "\n")
        detail := fmt.Sprintf("%d potential secrets found", len(matches))
        return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

type secretFileResult struct {
        rel     string
        matches []string
}

func scanForSecrets(ctx context.Context, root, glob string, limit int) ([]string, error) {
        var files []secretFileResult
        count := 0

        err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return nil
                }
                if info == nil || info.IsDir() {
                        if info != nil && info.IsDir() {
                                base := filepath.Base(path)
                                if base == ".git" || base == "node_modules" || base == "vendor" || base == ".venv" || base == "venv" {
                                        return filepath.SkipDir
                                }
                        }
                        return nil
                }

                if glob != "" {
                        matched, err := filepath.Match(glob, filepath.Base(path))
                        if err != nil || !matched {
                                return nil
                        }
                }

                b, err := os.ReadFile(path)
                if err != nil {
                        return nil
                }
                text := strings.ReplaceAll(string(b), "\r\n", "\n")
                lines := strings.Split(text, "\n")

                rel := path
                if cwd, err := os.Getwd(); err == nil {
                        if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
                                rel = r
                        }
                }

                var matches []string
                for i, line := range lines {
                        // Stop at the first matching pattern for this line. Patterns
                        // are ordered most-specific-first, so e.g. an AWS access key
                        // beats Generic secret. The old loop appended a separate
                        // entry for every pattern that matched — a single line
                        // containing `api_key = "sk-..."` was double-counted (API
                        // key + Generic secret) and inflated the headline number.
                        for _, pattern := range secretPatterns {
                                m := pattern.re.FindStringSubmatch(line)
                                if m == nil {
                                        continue
                                }
                                // Pick the captured value: the last non-empty submatch,
                                // falling back to the full match. Patterns without
                                // submatches use the full match — none of those have an
                                // ok filter today, but the path is still well-defined.
                                val := m[0]
                                for j := len(m) - 1; j >= 1; j-- {
                                        if m[j] != "" {
                                                val = m[j]
                                                break
                                        }
                                }
                                if pattern.ok != nil && !pattern.ok(val) {
                                        continue
                                }
                                count++
                                if count > limit {
                                        return fmt.Errorf(
                                                "... (%d secrets limit reached; use limit=%d for more)",
                                                limit, limit*2,
                                        )
                                }
                                lineText := strings.TrimRight(line, "\r")
                                matches = append(matches, fmt.Sprintf("%s:>>%4d|[%s] %s", rel, i+1, pattern.name, lineText))
                                break
                        }
                }

                if len(matches) > 0 {
                        files = append(files, secretFileResult{rel: rel, matches: matches})
                }

                return nil
        })

        var out []string
        for _, f := range files {
                out = append(out, "@file "+f.rel)
                out = append(out, f.matches...)
        }
        if err != nil {
                out = append(out, err.Error())
        }
        return out, nil
}
