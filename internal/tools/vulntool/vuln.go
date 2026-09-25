package vulntool

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
	vulnDefaultLimit = 100
)

var vulnDescription = `Scan Go files for common security vulnerability patterns.

Checks for SQL injection, command injection, XSS, insecure crypto,
hardcoded secrets, and unsafe HTTP handling. Returns findings with
file:line references and remediation guidance.`

// VulnTool returns the basic vulnerability scanner tool definition + handler.
func VulnTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "vuln",
			Description: vulnDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or package path. Example: ./internal/tools",
					},
					"glob": llm.Object{
						"type":        "string",
						"description": "File pattern filter. Example: *.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 50 (default: %d)", vulnDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in vulnInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("vuln %s", p)
		},
		Run: runVuln,
	}
}

type vulnInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type vulnHit struct {
	file     string
	line     int
	pattern  string
	message  string
	severity string
}

func runVuln(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in vulnInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse vuln arguments: %w", err)
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
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}

	glob := strings.TrimSpace(in.Glob)
	if glob == "" {
		glob = "*.go"
	}

	limit := in.Limit
	if limit <= 0 {
		limit = vulnDefaultLimit
	}

	hits, err := scanVulnerabilities(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hits) == 0 {
		return tooldef.Result{Content: "No vulnerability patterns detected", Detail: "0 hits", Output: "No vulnerability patterns detected"}, nil
	}

	content := renderVulnResults(ctx, hits)
	detail := fmt.Sprintf("%d potential vulnerabilities", len(hits))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func scanVulnerabilities(root, glob string, limit int) ([]vulnHit, error) {
	goFiles, err := collectGoFiles(root, glob)
	if err != nil {
		return nil, err
	}

	var hits []vulnHit
	for _, file := range goFiles {
		fileHits, err := scanFileForVulns(file)
		if err != nil {
			continue
		}
		hits = append(hits, fileHits...)
		if len(hits) >= limit {
			break
		}
	}

	if len(hits) > limit {
		hits = hits[:limit]
	}

	return hits, nil
}

func collectGoFiles(root, glob string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			if info != nil && info.IsDir() {
				if shouldSkipDir(filepath.Base(path)) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		matched, err := filepath.Match(glob, filepath.Base(path))
		if err != nil || !matched {
			return nil
		}

		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

var vulnPatterns = []struct {
	pattern  *regexp.Regexp
	message  string
	severity string
}{
	{regexp.MustCompile(`(?i)fmt\.Sprintf\(".*%s.*",\s*\w+\)`), "potential SQL injection via fmt.Sprintf with user input", "high"},
	{regexp.MustCompile(`(?i)exec\.Command\(".*",\s*.*input`), "potential command injection via exec.Command with user input", "high"},
	{regexp.MustCompile(`(?i)template\.HTML\("`), "potential XSS via template.HTML with unescaped content", "high"},
	{regexp.MustCompile(`(?i)md5\.New\(\)|md5\.Sum`), "use of weak hash function MD5", "medium"},
	{regexp.MustCompile(`(?i)sha1\.New\(\)|sha1\.Sum`), "use of weak hash function SHA1", "medium"},
	{regexp.MustCompile(`(?i)crypto/des`), "use of weak encryption DES", "high"},
	{regexp.MustCompile(`(?i)crypto/rc4`), "use of weak encryption RC4", "high"},
	{regexp.MustCompile(`(?i)password.*=.*["'][^"']{8,}["']`), "hardcoded password in source code", "high"},
	{regexp.MustCompile(`(?i)api[_-]?key.*=.*["'][A-Za-z0-9_\-]{20,}["']`), "hardcoded API key in source code", "high"},
	{regexp.MustCompile(`(?i)http\.Get\(|http\.Post\(`), "HTTP request without timeout; consider context.WithTimeout", "medium"},
	{regexp.MustCompile(`(?i)tls\.Config\{[^}]*InsecureSkipVerify.*true`), "InsecureSkipVerify=true disables TLS certificate validation", "high"},
}

func scanFileForVulns(path string) ([]vulnHit, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(b)
	lines := strings.Split(text, "\n")

	var hits []vulnHit
	for _, pattern := range vulnPatterns {
		for i, line := range lines {
			if pattern.pattern.MatchString(line) {
				hits = append(hits, vulnHit{
					file:     path,
					line:     i + 1,
					pattern:  pattern.pattern.String(),
					message:  pattern.message,
					severity: pattern.severity,
				})
			}
		}
	}

	return hits, nil
}

func renderVulnResults(ctx context.Context, hits []vulnHit) string {
	var sb strings.Builder
	for _, h := range hits {
		rel := tooldef.RelToCwd(ctx, h.file)
		sb.WriteString(fmt.Sprintf("%s:%d\t[%s]\t%s\n", rel, h.line, h.severity, h.message))
	}
	return sb.String()
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}
