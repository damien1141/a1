package ranktool

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	rankDefaultLimit = 50
	rankDefaultGlob  = "*"
)

var rankDescription = `Rank files by importance heuristics to prioritize reading order.

Uses size, directory depth, and file-type signals (tests/docs/configs are penalized).
Returns a scored list so the agent reads the most central files first.`

// RankTool returns the file importance ranker tool definition + handler.
func RankTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "rank",
			Description: rankDescription,
		Params: &llm.FunctionParameters{
			Type: "object",
			Properties: llm.Object{
				"path": llm.Object{
					"type":        "string",
					"description": "Directory to rank. Example: ./src",
				},
				"glob": llm.Object{
					"type":        "string",
					"description": fmt.Sprintf("Glob filter for file names. Example: **/*.go (default: %s)", rankDefaultGlob),
				},
				"limit": llm.Object{
					"type":        "integer",
					"description": fmt.Sprintf("Maximum results to return. Example: 20 (default: %d)", rankDefaultLimit),
				},
			},
			Required: []string{},
		},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in rankInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("rank %s", p)
		},
		Run: runRank,
	}
}

type rankInput struct {
	Path  string `json:"path,omitempty"`
	Glob  string `json:"glob,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type fileScore struct {
	path    string
	score   float64
	reasons []string
}

func runRank(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in rankInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse rank arguments: %w", err)
	}

	searchRel := strings.TrimSpace(in.Path)
	if searchRel == "" {
		searchRel = "."
	}
	searchPath, err := tooldef.ResolveToCwd(ctx, searchRel)
	if err != nil {
		return tooldef.Result{}, err
	}

	info, err := os.Stat(searchPath)
	if err != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s", searchPath)
	}
	if !info.IsDir() {
		return tooldef.Result{}, fmt.Errorf("path is not a directory: %s", searchPath)
	}

	glob := strings.TrimSpace(in.Glob)
	if glob == "" {
		glob = rankDefaultGlob
	}
	limit := in.Limit
	if limit <= 0 {
		limit = rankDefaultLimit
	}

	scores, err := rankFiles(searchPath, glob, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(scores) == 0 {
		return tooldef.Result{Content: "No files matched", Detail: "0 files", Output: "No files matched"}, nil
	}

	content := renderRankResults(ctx, scores)
	detail := fmt.Sprintf("%d files ranked", len(scores))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func rankFiles(root, glob string, limit int) ([]fileScore, error) {
	var results []fileScore

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

		score, reasons := scoreFile(root, path, info)
		results = append(results, fileScore{
			path:    path,
			score:   score,
			reasons: reasons,
		})
		return nil
	})

	if err != nil {
		return nil, err
	}

	sortScores(results)

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "__pycache__", ".tox", "dist", "build":
		return true
	}
	return false
}

func scoreFile(root, path string, info os.FileInfo) (float64, []string) {
	var score float64
	var reasons []string

	// Size signal: larger files tend to be more central.
	size := float64(info.Size())
	if size > 0 {
		sizeScore := math.Log(size+1) * 10
		score += sizeScore
		if size > 10000 {
			reasons = append(reasons, fmt.Sprintf("large file (%.1f KB)", size/1024))
		}
	}

	// Depth signal: shallower files are usually more important.
	rel := strings.TrimPrefix(path, root)
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	depth := strings.Count(rel, string(filepath.Separator))
	depthScore := math.Max(0, 30-float64(depth)*5)
	score += depthScore
	if depth == 0 {
		reasons = append(reasons, "root level")
	}

	// File-type penalties.
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))

	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasSuffix(base, "_test.rs"), strings.HasSuffix(base, "_test.js"), strings.HasSuffix(base, "_test.ts"):
		score -= 40
		reasons = append(reasons, "test file")
	case strings.HasSuffix(base, ".test.go"), strings.HasSuffix(base, ".spec.js"), strings.HasSuffix(base, ".spec.ts"):
		score -= 40
		reasons = append(reasons, "test file")
	case ext == ".md":
		score -= 30
		reasons = append(reasons, "documentation")
	case ext == ".json" || ext == ".yaml" || ext == ".yml" || ext == ".toml" || ext == ".ini" || ext == ".cfg" || base == "Makefile" || base == "Dockerfile":
		score -= 20
		reasons = append(reasons, "config")
	case ext == ".mod" || ext == ".sum" || base == "go.sum" || base == "package-lock.json" || base == "yarn.lock" || base == "pnpm-lock.yaml":
		score -= 50
		reasons = append(reasons, "lockfile")
	case ext == ".gitignore" || ext == ".editorconfig" || ext == ".env":
		score -= 60
		reasons = append(reasons, "dotfile")
	}

	if score < 0 {
		score = 0
	}

	return score, reasons
}

func sortScores(scores []fileScore) {
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
}

func renderRankResults(ctx context.Context, scores []fileScore) string {
	var sb strings.Builder
	for _, s := range scores {
		rel := tooldef.RelToCwd(ctx, s.path)
		scoreStr := fmt.Sprintf("%.1f", s.score)
		reason := ""
		if len(s.reasons) > 0 {
			reason = " — " + strings.Join(s.reasons, ", ")
		}
		sb.WriteString(fmt.Sprintf("%s\t%s%s\n", scoreStr, rel, reason))
	}
	return sb.String()
}
