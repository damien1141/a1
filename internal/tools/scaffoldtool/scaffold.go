package scaffoldtool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	scaffoldDefaultLimit = 50
)

var scaffoldDescription = `Detect project type and suggest appropriate development tools.

Scans for common project files (go.mod, package.json, Makefile, Dockerfile,
etc.) and returns a suggested toolchain for the detected stack.`

// ScaffoldTool returns the project scaffold aware tool definition + handler.
func ScaffoldTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "scaffold",
			Description: scaffoldDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory to analyze. Example: .",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum results to return. Example: 20 (default: %d)", scaffoldDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in scaffoldInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("scaffold %s", p)
		},
		Run: runScaffold,
	}
}

type scaffoldInput struct {
	Path  string `json:"path,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type scaffoldHint struct {
	file     string
	category string
	message  string
}

func runScaffold(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in scaffoldInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse scaffold arguments: %w", err)
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

	limit := in.Limit
	if limit <= 0 {
		limit = scaffoldDefaultLimit
	}

	hints, err := detectScaffold(searchPath, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(hints) == 0 {
		return tooldef.Result{Content: "No recognized project scaffold detected", Detail: "0 hints", Output: "No recognized project scaffold detected"}, nil
	}

	content := renderScaffoldResults(ctx, hints)
	detail := fmt.Sprintf("%d scaffold hints", len(hints))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func detectScaffold(root string, limit int) ([]scaffoldHint, error) {
	var hints []scaffoldHint

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

		name := filepath.Base(path)
		rel := path
		if r, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}

		switch name {
		case "go.mod":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Go module detected; use go build, go test, go vet",
			})
		case "go.sum":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Go module checksums present",
			})
		case "package.json":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Node.js project detected; use npm, yarn, or pnpm",
			})
		case "pnpm-lock.yaml", "yarn.lock", "package-lock.json":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Node.js lockfile detected; use the corresponding package manager",
			})
		case "Makefile":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "build",
				message:  "Makefile present; inspect targets with make help or make list",
			})
		case "Dockerfile":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "deployment",
				message:  "Dockerfile present; use docker build and docker run",
			})
		case "docker-compose.yml", "docker-compose.yaml":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "deployment",
				message:  "Docker Compose detected; use docker compose up/down",
			})
		case ".github":
			if info.IsDir() {
				hints = append(hints, scaffoldHint{
					file:     rel,
					category: "ci",
					message:  "GitHub Actions workflows detected under .github/workflows",
				})
			}
		case ".gitlab-ci.yml":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "ci",
				message:  "GitLab CI detected; use gitlab-runner or CI pipeline",
			})
		case "Cargo.toml":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Rust project detected; use cargo build, cargo test, cargo clippy",
			})
		case "pyproject.toml", "setup.py", "requirements.txt":
			hints = append(hints, scaffoldHint{
				file:     rel,
				category: "language",
				message:  "Python project detected; use pip, poetry, or pdm",
			})
		}

		if len(hints) >= limit {
			return filepath.SkipDir
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if len(hints) > limit {
		hints = hints[:limit]
	}

	return hints, nil
}

func renderScaffoldResults(ctx context.Context, hints []scaffoldHint) string {
	var sb strings.Builder
	for _, h := range hints {
		sb.WriteString(fmt.Sprintf("%s\t[%s]\t%s\n", h.file, h.category, h.message))
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
