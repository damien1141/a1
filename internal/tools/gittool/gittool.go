// Package gittool is the governed git/gh facade: one tool, a mode enum, and
// APPA-gated semantics instead of raw bash strings. Read modes (status, diff,
// log, show, branch list, pr/issue list+view) map to ActionRead in
// permission/extract.go so they never prompt; add/restore/branch-create map to
// ActionWrite with paths; commit/push/pull/pr-create/issue-create map to
// ActionBash so the gate asks — "no commit without explicit request" becomes
// enforceable instead of doctrinal.
package gittool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/damien1141/a1/internal/tools/tooldef"

	"github.com/damien1141/a1/internal/llm"
)

const (
	// gitTimeout bounds one git/gh invocation. Git calls are short; the bound
	// exists so a hung remote (push/pull over a wedged network) cannot pin a
	// tool slot forever.
	gitTimeout = 120 * time.Second
	// maxOutputBytes caps what one call returns to the model. Diff/log on a
	// large change can far exceed a useful context window, so the head is kept
	// (git orders output newest/most-relevant-first, unlike streaming shell
	// output where the bash tool keeps the tail) and a notice explains the cut.
	maxOutputBytes = 64 * 1024
	// collectCapBytes bounds how much output the runner buffers before the
	// display cap is applied. Generous enough that real git output under the
	// display cap is never pre-truncated at the wrong offset, small enough
	// that a runaway command cannot balloon memory.
	collectCapBytes = 1024 * 1024
	// logDefaultCount is the default max commits for mode=log.
	logDefaultCount = 20
	// commitDetailMax caps the commit message in the UI detail identity.
	commitDetailMax = 40
)

var gitDescription = `Governed git/gh tool — use instead of bash for git: read modes never prompt, add/restore/branch-create are writes, commit/push/pull and pr/issue create always ask.

status: branch+changes. diff: staged?, path?. log: n?, path?, oneline?. show: rev. branch: name?, start? (name=create). add/restore: files (restore: staged?). commit: message, files? (only those paths), amend? (no message keeps old). push: remote?, branch?. pull: remote?. pr/issue: action=list|view|create, number (view), title, body?, base? (create).`

// GitTool returns the git tool definition + handler.
//
// One facade tool with a mode enum (roadmap anti-goal: don't grow the
// always-on tool count with per-operation tools). Every mode funnels through
// buildArgs, which produces a strict argv — never a shell string — so the
// exec boundary cannot be injection-reached, and the permission layer can
// classify the call by mode before it runs.
func GitTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "git",
			Description: gitDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"mode": llm.Object{
						"type": "string",
						"enum": []string{
							"status", "diff", "log", "show", "branch", "add",
							"restore", "commit", "push", "pull", "pr", "issue",
						},
					},
					"rev":     llm.Object{"type": "string"},
					"path":    llm.Object{"type": "string"},
					"files":   llm.Object{"type": "array", "items": llm.Object{"type": "string"}},
					"n":       llm.Object{"type": "integer"},
					"oneline": llm.Object{"type": "boolean"},
					"staged":  llm.Object{"type": "boolean"},
					"name":    llm.Object{"type": "string"},
					"start":   llm.Object{"type": "string"},
					"message": llm.Object{"type": "string"},
					"amend":   llm.Object{"type": "boolean"},
					"remote":  llm.Object{"type": "string"},
					"branch":  llm.Object{"type": "string"},
					"action":  llm.Object{"type": "string"},
					"number":  llm.Object{"type": "integer"},
					"title":   llm.Object{"type": "string"},
					"body":    llm.Object{"type": "string"},
					"base":    llm.Object{"type": "string"},
				},
				Required: []string{"mode"},
			},
			// Not Readable: add/restore/commit mutate the shared index and HEAD.
			// A parallel batch of [add, commit] could commit before the add
			// lands, and browser_read already sets Readable=false for the same
			// shared-mutable-state reason — mode purity can't be expressed in a
			// per-tool flag, so the tool stays sequential.
			Readable: false,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in gitInput
			_ = json.Unmarshal(input, &in)
			return detailFor(in)
		},
		Run: runGit,
	}
}

// gitInput is the union of every mode's params; the mode field picks which
// subset is meaningful. Same pattern as readtool/writetool facades.
type gitInput struct {
	Mode    string   `json:"mode"`
	Rev     string   `json:"rev"`
	Path    string   `json:"path"`
	Files   []string `json:"files"`
	N       int      `json:"n"`
	Oneline bool     `json:"oneline"`
	Staged  bool     `json:"staged"`
	Name    string   `json:"name"`
	Start   string   `json:"start"`
	Message string   `json:"message"`
	Amend   bool     `json:"amend"`
	Remote  string   `json:"remote"`
	Branch  string   `json:"branch"`
	Action  string   `json:"action"`
	Number  int      `json:"number"`
	Title   string   `json:"title"`
	Body    string   `json:"body"`
	Base    string   `json:"base"`
}

func runGit(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in gitInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse git arguments: %w", err)
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	binary, argv, err := buildArgs(mode, in)
	if err != nil {
		return tooldef.Result{}, err
	}
	// Same session-cwd resolution as readtool: relative paths and repo state
	// are anchored to where the session started, not the process cwd.
	cwd, err := tooldef.Cwd(ctx)
	if err != nil {
		return tooldef.Result{}, err
	}
	out, err := runCommand(ctx, binary, argv, cwd)
	if err != nil {
		// A failed git call is a real error (not bash-style content): the
		// executor surfaces err.Error() to the model, which carries git's
		// output plus the exit-status line.
		return tooldef.Result{}, err
	}
	content := capOutput(out)
	if strings.TrimSpace(content) == "" {
		content = "(no output)"
	}
	detail := detailFor(in)
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

// buildArgs turns a validated mode + input into the binary and strict argv to
// run. Pure function: no ctx, no exec — tests assert exact argv slices.
func buildArgs(mode string, in gitInput) (string, []string, error) {
	switch mode {
	case "status":
		// -b prepends the "## branch...upstream [ahead/behind]" summary line.
		return "git", []string{"status", "--porcelain=v1", "-b"}, nil

	case "diff":
		argv := []string{"diff"}
		if in.Staged {
			argv = append(argv, "--cached")
		}
		if p := strings.TrimSpace(in.Path); p != "" {
			if err := checkNotFlag("path", p); err != nil {
				return "", nil, err
			}
			argv = append(argv, "--", p)
		}
		return "git", argv, nil

	case "log":
		n := in.N
		if n <= 0 {
			n = logDefaultCount
		}
		argv := []string{"log", "-n", strconv.Itoa(n)}
		if in.Oneline {
			argv = append(argv, "--oneline")
		}
		if p := strings.TrimSpace(in.Path); p != "" {
			if err := checkNotFlag("path", p); err != nil {
				return "", nil, err
			}
			argv = append(argv, "--", p)
		}
		return "git", argv, nil

	case "show":
		rev := strings.TrimSpace(in.Rev)
		if rev == "" {
			return "", nil, errors.New("mode show requires rev (commit, tag, or branch)")
		}
		if err := checkNotFlag("rev", rev); err != nil {
			return "", nil, err
		}
		return "git", []string{"show", rev}, nil

	case "branch":
		if name := strings.TrimSpace(in.Name); name != "" {
			if err := checkNotFlag("name", name); err != nil {
				return "", nil, err
			}
			argv := []string{"branch", name}
			if start := strings.TrimSpace(in.Start); start != "" {
				if err := checkNotFlag("start", start); err != nil {
					return "", nil, err
				}
				argv = append(argv, start)
			}
			return "git", argv, nil
		}
		if strings.TrimSpace(in.Start) != "" {
			return "", nil, errors.New("branch start requires name — set name to create a branch")
		}
		return "git", []string{"branch", "-vv"}, nil

	case "add":
		files, err := cleanFiles(in.Files)
		if err != nil {
			return "", nil, err
		}
		return "git", append([]string{"add", "--"}, files...), nil

	case "restore":
		files, err := cleanFiles(in.Files)
		if err != nil {
			return "", nil, err
		}
		argv := []string{"restore"}
		if in.Staged {
			argv = append(argv, "--staged")
		}
		return "git", append(append(argv, "--"), files...), nil

	case "commit":
		msg := strings.TrimSpace(in.Message)
		if msg == "" && !in.Amend {
			return "", nil, errors.New("mode commit requires message (or amend=true to keep the previous one)")
		}
		argv := []string{"commit"}
		if in.Amend {
			argv = append(argv, "--amend")
		}
		if msg != "" {
			argv = append(argv, "-m", msg)
		} else {
			argv = append(argv, "--no-edit")
		}
		// files is optional here: absent = commit whatever is staged.
		if len(in.Files) > 0 {
			files, err := cleanFiles(in.Files)
			if err != nil {
				return "", nil, err
			}
			// Pathspecs after -- commit only those paths' changes directly,
			// bypassing the index for tracked files — the add+commit-of-subset
			// semantics without a second call.
			argv = append(append(argv, "--"), files...)
		}
		return "git", argv, nil

	case "push":
		remote := defaultRemote(in.Remote)
		if err := checkNotFlag("remote", remote); err != nil {
			return "", nil, err
		}
		argv := []string{"push", remote}
		if b := strings.TrimSpace(in.Branch); b != "" {
			if err := checkNotFlag("branch", b); err != nil {
				return "", nil, err
			}
			argv = append(argv, b)
		}
		return "git", argv, nil

	case "pull":
		remote := defaultRemote(in.Remote)
		if err := checkNotFlag("remote", remote); err != nil {
			return "", nil, err
		}
		return "git", []string{"pull", remote}, nil

	case "pr", "issue":
		return buildGhArgs(mode, in)

	case "":
		return "", nil, errors.New(
			"mode is required: use status, diff, log, show, branch, add, restore, commit, push, pull, pr, or issue")
	default:
		return "", nil, fmt.Errorf(
			"unknown git mode %q: use status, diff, log, show, branch, add, restore, commit, push, pull, pr, or issue", in.Mode)
	}
}

// buildGhArgs builds the gh CLI argv for pr/issue modes. list and view are
// read-only queries; create posts new content.
func buildGhArgs(mode string, in gitInput) (string, []string, error) {
	action := strings.ToLower(strings.TrimSpace(in.Action))
	if action == "" {
		action = "list"
	}
	switch action {
	case "list":
		return "gh", []string{mode, "list"}, nil
	case "view":
		if in.Number <= 0 {
			return "", nil, fmt.Errorf("%s view requires number (the %s number)", mode, mode)
		}
		return "gh", []string{mode, "view", strconv.Itoa(in.Number)}, nil
	case "create":
		title := strings.TrimSpace(in.Title)
		if title == "" {
			return "", nil, fmt.Errorf("%s create requires title", mode)
		}
		// --body always passed (even empty) so gh never falls back to an
		// interactive editor prompt in headless runs.
		argv := []string{mode, "create", "--title", title, "--body", strings.TrimSpace(in.Body)}
		if mode == "pr" {
			if b := strings.TrimSpace(in.Base); b != "" {
				if err := checkNotFlag("base", b); err != nil {
					return "", nil, err
				}
				argv = append(argv, "--base", b)
			}
		}
		return "gh", argv, nil
	default:
		return "", nil, fmt.Errorf("unknown %s action %q: use list, view, or create", mode, in.Action)
	}
}

// checkNotFlag rejects values that start with "-" so a rev/branch/remote/path
// can never be re-parsed as a git option. Args are exec'd as a strict argv so
// there is no shell-injection surface; this closes the option-injection one.
func checkNotFlag(field, v string) error {
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s must not start with '-' (got %q)", field, v)
	}
	return nil
}

// cleanFiles trims and drops empty entries; a missing/empty files array is an
// error, not an implicit "everything" — that is what the permission layer's
// ActionBash fallback is for.
func cleanFiles(files []string) ([]string, error) {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("files is required: pass a non-empty array of paths")
	}
	return out, nil
}

func defaultRemote(remote string) string {
	if r := strings.TrimSpace(remote); r != "" {
		return r
	}
	return "origin"
}

// detailFor renders the one-line UI identity for a call ("commit fix auth…").
// Mode is best-effort parsed — DetailFromArgs must never error.
func detailFor(in gitInput) string {
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	switch mode {
	case "":
		return "git"
	case "status":
		return "status"
	case "diff", "log":
		if p := strings.TrimSpace(in.Path); p != "" {
			return mode + " " + p
		}
		return mode
	case "show":
		return "show " + strings.TrimSpace(in.Rev)
	case "branch":
		if name := strings.TrimSpace(in.Name); name != "" {
			return "branch " + name
		}
		return "branch list"
	case "add", "restore":
		return mode + " " + strings.Join(in.Files, " ")
	case "commit":
		msg := strings.TrimSpace(in.Message)
		if msg == "" {
			return "commit --amend"
		}
		if r := []rune(msg); len(r) > commitDetailMax {
			msg = string(r[:commitDetailMax]) + "…"
		}
		return "commit " + msg
	case "push":
		if b := strings.TrimSpace(in.Branch); b != "" {
			return "push " + defaultRemote(in.Remote) + " " + b
		}
		return "push " + defaultRemote(in.Remote)
	case "pull":
		return "pull " + defaultRemote(in.Remote)
	case "pr", "issue":
		switch strings.ToLower(strings.TrimSpace(in.Action)) {
		case "", "list":
			return mode + " list"
		case "view":
			return mode + " view " + strconv.Itoa(in.Number)
		case "create":
			return mode + " create " + truncateRunes(strings.TrimSpace(in.Title), commitDetailMax)
		}
		return mode + " " + strings.TrimSpace(in.Action)
	default:
		return "git " + mode
	}
}

func truncateRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// runCommand executes binary with a strict argv in dir (never through a
// shell) and returns combined output. Package-level var so tests stub the exec
// boundary without touching real git.
var runCommand = func(ctx context.Context, binary string, argv []string, dir string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, binary, argv...)
	cmd.Dir = dir
	buf := newCappedBuffer(collectCapBytes)
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	out := buf.String()
	if err != nil {
		return "", commandError(binary, out, cctx, err)
	}
	return out, nil
}

// commandError renders a failed invocation so the model sees git's own output
// plus a recognizable exit-status line (the same "(exit error: …)" suffix the
// bash tool uses, kept as an error here because a governed tool's failure is a
// failure, not exploration output).
func commandError(binary, out string, cctx context.Context, err error) error {
	if cctx.Err() != nil {
		if msg := strings.TrimSpace(out); msg != "" {
			return fmt.Errorf("%s\n(command canceled or timed out after %s)", msg, gitTimeout)
		}
		return fmt.Errorf("command canceled or timed out after %s", gitTimeout)
	}
	if errors.Is(err, exec.ErrNotFound) {
		if binary == "gh" {
			return errors.New("gh CLI not found on PATH — install it from https://cli.github.com/ for git modes pr and issue")
		}
		return fmt.Errorf("%s not found on PATH — install it to use the git tool", binary)
	}
	msg := strings.TrimSpace(capOutput(out))
	if msg == "" {
		return fmt.Errorf("(exit error: %v)", err)
	}
	return fmt.Errorf("%s\n(exit error: %v)", msg, err)
}

// capOutput keeps the first maxOutputBytes with a notice when more existed.
func capOutput(out string) string {
	if len(out) <= maxOutputBytes {
		return out
	}
	return out[:maxOutputBytes] + fmt.Sprintf(
		"\n[output truncated: kept the first %d KiB — narrow the query (path, n, rev)]", maxOutputBytes/1024)
}

// cappedBuffer collects output keeping the first limit bytes (git orders
// output most-relevant-first, so the head is the part worth keeping). Mutex
// mirrors bashtool's collector: os/exec coalesces equal writers onto one copy
// path, but the guard keeps Write safe regardless of how callers wire it.
type cappedBuffer struct {
	mu    sync.Mutex
	buf   strings.Builder
	limit int
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - c.buf.Len()
	if room <= 0 {
		return len(p), nil
	}
	if len(p) > room {
		p = p[:room]
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}
