package gittool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	gitDefaultTimeout = 10 * time.Second
)

var gitDescription = `Inspect git history and blame for a file or path. Prefer this over bash for git log/blame queries.`

// GitTool returns the git tool definition + handler.
func GitTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "git",
			Description: gitDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"subcommand": llm.Object{
						"type":        "string",
						"description": "Git subcommand: log, blame, merge-tree. Example: log",
					},
					"path": llm.Object{
						"type":        "string",
						"description": "File or directory path. Example: src/main.go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": "Max entries for log. Example: 20 (default: 20)",
					},
					"branch": llm.Object{
						"type":        "string",
						"description": "Branch or ref for merge-tree. Example: main",
					},
					"branch2": llm.Object{
						"type":        "string",
						"description": "Second branch or ref for merge-tree. Example: feat/x",
					},
					"format": llm.Object{
						"type":        "string",
						"description": "Log format: default, oneline, short, medium, full, fuller, email, raw, format:<string>. Example: oneline",
					},
				},
				Required: []string{"subcommand"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in gitInput
			_ = json.Unmarshal(input, &in)
			return fmt.Sprintf("git %s %s", in.Subcommand, strings.TrimSpace(in.Path))
		},
		Run: runGit,
	}
}

type gitInput struct {
	Subcommand string `json:"subcommand"`
	Path       string `json:"path,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Branch2    string `json:"branch2,omitempty"`
	Format     string `json:"format,omitempty"`
}

func runGit(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in gitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse git arguments: %w", err)
	}

	sub := strings.TrimSpace(in.Subcommand)
	if sub == "" {
		return tooldef.Result{}, errors.New("subcommand is required: log, blame, or merge-tree")
	}

	switch sub {
	case "log", "blame", "merge-tree":
		// supported
	default:
		return tooldef.Result{}, fmt.Errorf("unsupported git subcommand %q: use log, blame, or merge-tree", sub)
	}

	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		return tooldef.Result{}, err
	}

	gitDir := filepathJoin(cwd, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return tooldef.Result{}, fmt.Errorf("not a git repository (or any parent up to mount point): %s", cwd)
	}

	switch sub {
	case "log":
		return runGitLog(ctx, cwd, in)
	case "blame":
		return runGitBlame(ctx, cwd, in)
	case "merge-tree":
		return runGitMergeTree(ctx, cwd, in)
	default:
		return tooldef.Result{}, fmt.Errorf("unhandled git subcommand %q", sub)
	}
}

func runGitLog(ctx context.Context, cwd string, in gitInput) (tooldef.Result, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	args := []string{"log", "--decorate"}
	if in.Path != "" {
		args = append(args, "--", in.Path)
	}

	fmtStr := strings.TrimSpace(in.Format)
	switch fmtStr {
	case "", "default":
		fmtStr = "medium"
	}

	if strings.HasPrefix(fmtStr, "format:") || strings.HasPrefix(fmtStr, "format=") {
		args = append(args, fmtStr)
	} else if isBuiltinFormat(fmtStr) {
		args = append(args, "--pretty="+fmtStr)
	} else {
		args = append(args, "--pretty=format:"+fmtStr)
	}

	args = append(args, fmt.Sprintf("-%d", limit))

	rel := "."
	if in.Path != "" {
		abs, pathErr := tooldef.ResolveToCwd(ctx, in.Path)
		if pathErr == nil {
			rel = tooldef.RelToCwd(ctx, abs)
		}
	}

	out, err := execGit(ctx, cwd, args...)
	if err != nil {
		if strings.Contains(err.Error(), "does not have any commits yet") {
			content := "(no commits)"
			detail := fmt.Sprintf("git log %s", rel)
			return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
		}
		return tooldef.Result{}, err
	}

	content := strings.TrimSpace(out)
	if content == "" {
		content = "(no commits)"
	}

	detail := fmt.Sprintf("git log %s", rel)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func runGitBlame(ctx context.Context, cwd string, in gitInput) (tooldef.Result, error) {
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return tooldef.Result{}, errors.New("path is required for blame")
	}

	abs, err := tooldef.ResolveToCwd(ctx, path)
	if err != nil {
		return tooldef.Result{}, err
	}

	if _, statErr := os.Stat(abs); statErr != nil {
		return tooldef.Result{}, fmt.Errorf("path not found: %s", tooldef.RelToCwd(ctx, abs))
	}

	args := []string{"blame", "--line-porcelain", "--", abs}
	out, err := execGit(ctx, cwd, args...)
	if err != nil {
		return tooldef.Result{}, err
	}

	content := parseBlamePorcelain(out, abs)
	detail := fmt.Sprintf("git blame %s", tooldef.RelToCwd(ctx, abs))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

type blameLine struct {
	hash       string
	author     string
	authorTime string
	lineNum    int
	line       string
}

func parseBlamePorcelain(out, absPath string) string {
	lines := strings.Split(out, "\n")
	var sb strings.Builder
	var current blameLine
	var lineNum int

	for _, line := range lines {
		if strings.HasPrefix(line, "\t") {
			lineNum++
			current.line = strings.TrimPrefix(line, "\t")
			if current.line == "" {
				current.line = " "
			}
			rel := tooldef.RelToCwd(context.Background(), absPath)
			hash := current.hash
			if len(hash) < 7 {
				hash = "unknown"
			}
			sb.WriteString(fmt.Sprintf("%s:%d#%s | %s | %s | %s\n", rel, lineNum, hash[:7], current.authorTime, current.author, current.line))
			continue
		}

		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		key := fields[0]
		val := fields[1]

		switch key {
		case "author":
			current.author = val
		case "author-time":
			if ts, err := strconv.ParseInt(val, 10, 64); err == nil {
				t := time.Unix(ts, 0).UTC()
				current.authorTime = t.Format("2006-01-02")
			} else {
				current.authorTime = val
			}
		case "hash":
			current.hash = val
		}
	}

	return sb.String()
}

func runGitMergeTree(ctx context.Context, cwd string, in gitInput) (tooldef.Result, error) {
	branch := strings.TrimSpace(in.Branch)
	branch2 := strings.TrimSpace(in.Branch2)
	if branch == "" || branch2 == "" {
		return tooldef.Result{}, errors.New("branch and branch2 are required for merge-tree")
	}

	args := []string{"merge-tree", "--write-tree", "--no-messages", branch, branch2}
	out, err := execGit(ctx, cwd, args...)
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			content := fmt.Sprintf("Merge conflict between %s and %s:\n%s", branch, branch2, err.Error())
			detail := fmt.Sprintf("git merge-tree %s %s (conflict)", branch, branch2)
			return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
		}
		return tooldef.Result{}, err
	}

	treeHash := strings.TrimSpace(out)
	content := fmt.Sprintf("Clean merge between %s and %s.\nResulting tree: %s", branch, branch2, treeHash)
	detail := fmt.Sprintf("git merge-tree %s %s (clean)", branch, branch2)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func execGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitDefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, "git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cctx.Err() != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), cctx.Err())
		}
		text := strings.TrimSpace(string(out))
		if text != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), text)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func isBuiltinFormat(fmt string) bool {
	switch fmt {
	case "oneline", "short", "medium", "full", "fuller", "email", "raw":
		return true
	}
	return false
}

func filepathJoin(elem ...string) string {
	parts := make([]string, len(elem))
	for i, e := range elem {
		parts[i] = e
	}
	return strings.Join(parts, "/")
}
