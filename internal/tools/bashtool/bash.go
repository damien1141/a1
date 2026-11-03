package bashtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"

	"github.com/damien1141/a1/internal/sandbox"
)

const (
	bashDefaultTimeout = 300
)

var bashDescription = `Run a shell command and return combined stdout/stderr.

Default (no mode) runs the command in the foreground and waits. Use for build, test,
git, and OS tasks that read/ls/find/grep/edit/write cannot do. Do not use for cat,
head, tail, ls(1), find(1), grep, or rg — those have dedicated tools. Large output
is truncated with the retained output written to a temp file.

Long-running commands (dev servers, watchers) should run in the background:
- {"command": "npm run dev", "mode": "start"} → starts detached, returns a shell id
- {"mode": "ps"} → lists background shells with status
- {"mode": "attach", "shell_id": "bg1"} → returns the output tail so far; add "wait": 15
  to block up to 15s for exit or more output
- {"mode": "kill", "shell_id": "bg1"} → stops the shell's whole process tree
Background output is not verification evidence — run checks in the foreground.`

// BashTool returns the bash tool definition + handler.
func BashTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "bash",
			Description: bashDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"command": llm.Object{
						"type":        "string",
						"description": "Shell command to run (modes run/start). Example: go test ./...",
					},
					"mode": llm.Object{
						"type":        "string",
						"enum":        []string{"run", "start", "ps", "attach", "kill"},
						"description": "run (default): execute and wait. start: run detached in the background. ps: list background shells. attach: read a background shell's output. kill: stop one.",
					},
					"shell_id": llm.Object{
						"type":        "string",
						"description": "Background shell id for modes attach/kill, e.g. bg1.",
					},
					"wait": llm.Object{
						"type":        "integer",
						"description": "Attach mode: seconds to block waiting for exit or more output (0 = return immediately).",
					},
					"cwd": llm.Object{
						"type":        "string",
						"description": "Working directory for this command (absolute, or relative to the session cwd). Example: internal/auth",
					},
					"stdin": llm.Object{
						"type":        "string",
						"description": "Text piped to the command's standard input. Enables pipe workflows (gpg, ssh, psql, REPLs) without heredocs.",
					},
					"env": llm.Object{
						"type":                 "object",
						"description":          "Extra environment variables for this call, merged over the inherited environment. Example: {\"CI\": \"1\"}",
						"additionalProperties": llm.Object{"type": "string"},
					},
					"timeout": llm.Object{
						"type":        "integer",
						"description": "Timeout in seconds, 1-3600 (foreground only; default: 300). Background shells run until killed.",
					},
				},
				Required: []string{"command"},
			},
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in bashInput
			_ = json.Unmarshal(input, &in)
			switch in.Mode {
			case "start":
				return strings.TrimSpace("bg-start: " + in.Command)
			case "ps":
				return "bg-shells"
			case "attach":
				return "bg-attach: " + in.ShellID
			case "kill":
				return "bg-kill: " + in.ShellID
			}
			return strings.TrimSpace(in.Command)
		},
		Run: runBash,
	}
}

type bashInput struct {
	Command string            `json:"command"`
	Timeout int               `json:"timeout"`
	Mode    string            `json:"mode"`
	ShellID string            `json:"shell_id"`
	Wait    int               `json:"wait"`
	Stdin   string            `json:"stdin"`
	Env     map[string]string `json:"env"`
	Cwd     string            `json:"cwd"`
}

func runBash(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in bashInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse bash arguments: %w", err)
	}
	switch in.Mode {
	case "start":
		cmd := strings.TrimSpace(in.Command)
		if cmd == "" {
			return tooldef.Result{}, errors.New("mode start requires a command")
		}
		id, err := startBackground(ctx, cmd)
		if err != nil {
			return tooldef.Result{}, err
		}
		content := fmt.Sprintf(
			"started %s — %s\n(attach with bash {\"mode\":\"attach\",\"shell_id\":\"%s\"}; kill with mode kill)",
			id,
			cmd,
			id,
		)
		return tooldef.Result{Content: content, Detail: cmd, Output: content}, nil
	case "ps":
		content := listBackground()
		return tooldef.Result{Content: content, Detail: "bg-shells", Output: content}, nil
	case "attach":
		id := strings.TrimSpace(in.ShellID)
		if id == "" {
			return tooldef.Result{}, errors.New("mode attach requires shell_id")
		}
		content, err := attachBackground(id, in.Wait)
		if err != nil {
			return tooldef.Result{}, err
		}
		return tooldef.Result{Content: content, Detail: "bg-attach: " + id, Output: content}, nil
	case "kill":
		id := strings.TrimSpace(in.ShellID)
		if id == "" {
			return tooldef.Result{}, errors.New("mode kill requires shell_id")
		}
		content, err := killBackground(id)
		if err != nil {
			return tooldef.Result{}, err
		}
		return tooldef.Result{Content: content, Detail: "bg-kill: " + id, Output: content}, nil
	}
	cmd := strings.TrimSpace(in.Command)
	if cmd == "" {
		return tooldef.Result{}, errors.New("empty command")
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = bashDefaultTimeout
	}
	if timeout > 3600 {
		timeout = 3600
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	c, err := buildShellCommand(ctx, cmd)
	if err != nil {
		return tooldef.Result{}, err
	}
	// Per-call stdin, env, and cwd. Env entries are appended after the
	// inherited environment — on duplicate keys the later entry wins, so
	// the call's values override. Relative cwd resolves against the
	// session cwd (the shell could `cd` anyway; this is ergonomics, not
	// confinement).
	if strings.TrimSpace(in.Stdin) != "" {
		c.Stdin = strings.NewReader(in.Stdin)
	}
	if len(in.Env) > 0 {
		env := c.Env
		keys := make([]string, 0, len(in.Env))
		for k := range in.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+in.Env[k])
		}
		c.Env = env
	}
	if dir := strings.TrimSpace(in.Cwd); dir != "" {
		if !filepath.IsAbs(dir) {
			if base, err := tooldef.Cwd(ctx); err == nil && base != "" {
				dir = filepath.Join(base, dir)
			}
		}
		c.Dir = dir
	}
	// Bound the shared stdout/stderr collector so runaway output cannot be
	// buffered unboundedly.
	cb := newCappedBuffer(BashMaxCollectBytes)
	c.Stdout = cb
	c.Stderr = cb
	// OS-level containment: the profile compiles from the permission policy
	// (workspace, caches, network toggle); nil leaves the exec untouched.
	prof := bashSandboxProfile(ctx)
	if prof != nil {
		// startLocked already prefixes enforcement failures with "sandbox:" and
		// names the failing anchor; re-wrapping here printed "sandbox: sandbox: …".
		if serr := sandbox.Start(c, prof); serr != nil {
			return tooldef.Result{}, serr
		}
		err = c.Wait()
	} else {
		err = c.Run()
	}

	out := formatBashOutput(cb.String(), cb.Truncated())
	if strings.TrimSpace(out) == "" {
		out = "(no output)"
	}

	content := out
	if err != nil {
		if ctx.Err() != nil {
			content = out + "\n(command canceled or timed out)"
		} else {
			content = fmt.Sprintf("%s\n(exit error: %v)", out, err)
		}
	}
	content = appendSandboxHint(content, prof != nil)
	return tooldef.Result{Content: content, Detail: cmd, Output: content}, nil
}
