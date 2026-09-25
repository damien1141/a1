package watchertool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	watcherDefaultInterval = 5 * time.Second
	watcherMaxFiles        = 5000
)

var watcherDescription = `Detect files changed on disk since the last check.

Call this before sensitive operations to avoid stale reads or overwriting
manual edits. Returns cwd-relative paths for files whose mtime advanced.`

// WatcherTool returns the file change detector tool definition + handler.
func WatcherTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "watcher",
			Description: watcherDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
			Properties: llm.Object{
				"path": llm.Object{
					"type":        "string",
					"description": "Directory to watch. Example: .",
				},
				"since": llm.Object{
					"type":        "string",
					"description": "ISO8601 timestamp to check from. Empty uses last check.",
				},
				"glob": llm.Object{
					"type":        "string",
					"description": "Optional file pattern filter. Example: *.go",
				},
			},
				Required: []string{},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in watcherInput
			_ = json.Unmarshal(input, &in)
			p := strings.TrimSpace(in.Path)
			if p == "" {
				p = "."
			}
			return fmt.Sprintf("watcher %s", p)
		},
		Run: runWatcher,
	}
}

type watcherInput struct {
	Path  string `json:"path,omitempty"`
	Since string `json:"since,omitempty"`
	Glob  string `json:"glob,omitempty"`
}

var (
	lastCheckMu sync.Mutex
	lastCheck   time.Time
)

func runWatcher(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in watcherInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse watcher arguments: %w", err)
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
		return tooldef.Result{}, fmt.Errorf("path not found: %s", tooldef.RelToCwd(ctx, searchPath))
	}
	if !info.IsDir() {
		return tooldef.Result{}, fmt.Errorf("path is not a directory: %s", tooldef.RelToCwd(ctx, searchPath))
	}

	since := time.Time{}
	if strings.TrimSpace(in.Since) != "" {
		since, err = time.Parse(time.RFC3339, strings.TrimSpace(in.Since))
		if err != nil {
			return tooldef.Result{}, fmt.Errorf("invalid since timestamp (use ISO8601, e.g. 2024-01-01T00:00:00Z): %w", err)
		}
	} else {
		lastCheckMu.Lock()
		since = lastCheck
		lastCheckMu.Unlock()
	}

	glob := strings.TrimSpace(in.Glob)

	changed, err := findChangedFiles(searchPath, since, glob)
	if err != nil {
		return tooldef.Result{}, err
	}

	lastCheckMu.Lock()
	lastCheck = time.Now()
	lastCheckMu.Unlock()

	if len(changed) == 0 {
		content := fmt.Sprintf("No files changed since %s", since.Format(time.RFC3339))
		return tooldef.Result{Content: content, Detail: "0 changed", Output: content}, nil
	}

	relChanged := make([]string, len(changed))
	for i, abs := range changed {
		relChanged[i] = tooldef.RelToCwd(ctx, abs)
	}

	content := strings.Join(relChanged, "\n")
	detail := fmt.Sprintf("%d changed since %s", len(relChanged), since.Format(time.RFC3339))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func findChangedFiles(root string, since time.Time, glob string) ([]string, error) {
	var changed []string
	count := 0

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info == nil || info.IsDir() {
			return nil
		}

		if count >= watcherMaxFiles {
			return filepath.SkipDir
		}

		if glob != "" {
			matched, err := filepath.Match(glob, filepath.Base(path))
			if err != nil || !matched {
				return nil
			}
		}

		if info.ModTime().After(since) {
			changed = append(changed, path)
			count++
		}

		return nil
	})

	return changed, err
}
