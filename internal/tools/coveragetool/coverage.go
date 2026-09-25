package coveragetool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	coverageDefaultLimit = 100
)

var coverageDescription = `Run Go tests and return a test coverage summary.

Executes ` + "`" + `go test -coverprofile` + "`" + ` in the target path, parses the coverage
profile, and returns per-file coverage percentages. Helps identify untested
packages and files.`

// CoverageTool returns the test coverage mapper tool definition + handler.
func CoverageTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "coverage",
			Description: coverageDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"path": llm.Object{
						"type":        "string",
						"description": "Directory or package path. Example: ./internal/tools",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum files to return. Example: 50 (default: %d)", coverageDefaultLimit),
					},
				},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in coverageInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("coverage %s", p)
		},
		Run: runCoverage,
	}
}

type coverageInput struct {
	Path  string `json:"path,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type fileCoverage struct {
	path    string
	percent float64
}

func runCoverage(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in coverageInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse coverage arguments: %w", err)
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
		limit = coverageDefaultLimit
	}

	profilePath, err := runGoTestCover(searchPath)
	if err != nil {
		return tooldef.Result{}, err
	}
	defer os.Remove(profilePath)

	coverage, err := parseCoverageProfile(profilePath, searchPath, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(coverage) == 0 {
		return tooldef.Result{Content: "No coverage data found", Detail: "0 files", Output: "No coverage data found"}, nil
	}

	content := renderCoverageResults(ctx, coverage)
	detail := fmt.Sprintf("%d files", len(coverage))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func runGoTestCover(searchPath string) (string, error) {
	tmpFile, err := os.CreateTemp("", "coverage-*.out")
	if err != nil {
		return "", fmt.Errorf("failed to create temp coverage file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	cmd := exec.Command("go", "test", "-coverprofile="+tmpPath, "./...")
	cmd.Dir = searchPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("go test -coverprofile failed: %s", strings.TrimSpace(string(out)))
	}

	if _, err := os.Stat(tmpPath); err != nil {
		return "", fmt.Errorf("coverage profile not generated: %w", err)
	}

	return tmpPath, nil
}

var coverageLineRe = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+)\s+(\d+)\s+(\d+)$`)

func parseCoverageProfile(profilePath, searchPath string, limit int) ([]fileCoverage, error) {
	b, err := os.ReadFile(profilePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(b), "\n")

	fileCoverageMap := make(map[string]*coverageStats)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		m := coverageLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		filePath := m[1]
		lengthStr := m[4]
		countStr := m[7]

		length, _ := strconv.ParseFloat(lengthStr, 64)
		count, _ := strconv.ParseFloat(countStr, 64)

		stats, ok := fileCoverageMap[filePath]
		if !ok {
			stats = &coverageStats{}
			fileCoverageMap[filePath] = stats
		}
		stats.total += length
		if count > 0 {
			stats.covered += length
		}
	}

	var results []fileCoverage
	for path, stats := range fileCoverageMap {
		percent := 0.0
		if stats.total > 0 {
			percent = (stats.covered / stats.total) * 100
		}
		results = append(results, fileCoverage{
			path:    path,
			percent: percent,
		})
	}

	sortCoverage(results)

	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

type coverageStats struct {
	total   float64
	covered float64
}

func sortCoverage(coverage []fileCoverage) {
	for i := 0; i < len(coverage); i++ {
		for j := i + 1; j < len(coverage); j++ {
			if coverage[j].percent < coverage[i].percent {
				coverage[i], coverage[j] = coverage[j], coverage[i]
			}
		}
	}
}

func renderCoverageResults(ctx context.Context, coverage []fileCoverage) string {
	var sb strings.Builder
	for _, c := range coverage {
		rel := tooldef.RelToCwd(ctx, c.path)
		percentStr := fmt.Sprintf("%.1f%%", c.percent)
		sb.WriteString(fmt.Sprintf("%s\t%s\n", percentStr, rel))
	}
	return sb.String()
}
