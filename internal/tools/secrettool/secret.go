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

Returns file headers plus line anchors with the matched secret type.
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
					"type":        "integer",
					"description": fmt.Sprintf("Maximum matches to return. Example: 50 (default: %d)", secretDefaultLimit),
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

var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"API key", regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?([A-Za-z0-9_\-]{20,})['"]?`)},
	{"Bearer token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_\-\.]{20,}`)},
	{"Private key", regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?PRIVATE\s+KEY-----`)},
	{"AWS access key", regexp.MustCompile(`(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`)},
	{"AWS secret", regexp.MustCompile(`(?i)aws(.{0,20})?secret(.{0,20})?[:=]\s*['"]?([A-Za-z0-9/+=]{40})['"]?`)},
	{"GitHub token", regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)[A-Za-z0-9_]{36}`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]+`)},
	{"Password", regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*['"]?([^\s'"]{8,})['"]?`)},
	{"Generic secret", regexp.MustCompile(`(?i)(secret|token|key)\s*[:=]\s*['"]?([A-Za-z0-9_\-]{16,})['"]?`)},
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

	matches, err := scanForSecrets(searchPath, glob, limit)
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

func scanForSecrets(root, glob string, limit int) ([]string, error) {
	var matches []string
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

		fileTag := computeFileHash(text)
		rel := path
		if cwd, err := os.Getwd(); err == nil {
			if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
				rel = r
			}
		}
		if fileTag != "" {
			matches = append(matches, formatFileHeader(rel, fileTag))
		}

		for i, line := range lines {
			for _, pattern := range secretPatterns {
				if pattern.re.MatchString(line) {
					count++
					if count > limit {
						matches = append(matches, fmt.Sprintf("... (%d secrets limit reached; use limit=%d for more)", limit, limit*2))
						return filepath.SkipDir
					}
					lineText := strings.TrimRight(line, "\r")
					h := computeLineHash(lineText)
					ref := fmt.Sprintf("%d#%s", i+1, h)
					matches = append(matches, fmt.Sprintf("%s:>>%s|[%s] %s", rel, ref, pattern.name, lineText))
				}
			}
		}

		return nil
	})

	return matches, err
}

func computeFileHash(text string) string {
	return ""
}

func computeLineHash(line string) string {
	return ""
}

func formatFileHeader(path, tag string) string {
	return fmt.Sprintf("@file %s#%s", path, tag)
}
