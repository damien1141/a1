package permission

import (
        "encoding/json"
        "fmt"
        "strings"
)

// Extract builds a permission Request from a tool name and raw JSON args.
// Paths are absolute and cleaned against the process cwd.
func Extract(toolName string, args json.RawMessage) (Request, error) {
        return ExtractAt(toolName, args, "")
}

// ExtractAt is Extract with an explicit cwd for relative paths (session / job WorkDir).
//
// The tool set was collapsed: `write` folds edit (mode=write|edit), `search`
// folds grep/find/graph/rank (mode=auto|text|symbol|file|grep|find|graph|rank),
// `read` folds ls/watcher (mode=file|ls|watch). The permission system needs
// to know the *effective* action, which depends on the mode field — so for
// the merged tools, the mode is parsed here and the right Action / path
// extraction is picked. The old tool names (grep, find, ls, edit) stay
// handled for direct callers (e.g. tests) but won't appear in production
// traffic anymore.
func ExtractAt(toolName string, args json.RawMessage, cwd string) (Request, error) {
        req := Request{Tool: toolName, InputLabels: []Label{DefaultTrustForToolArgs(toolName)}}
        switch toolName {
        case "bash":
                var in struct {
                        Command string `json:"command"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("bash args: %w", err)
                }
                req.Action = ActionBash
                req.Command = strings.TrimSpace(in.Command)
                return req, nil

        case "read":
                var in struct {
                        Mode string `json:"mode"`
                        Path string `json:"path"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("read args: %w", err)
                }
                mode := strings.ToLower(strings.TrimSpace(in.Mode))
                if mode == "" {
                        mode = "file"
                }
                switch mode {
                case "ls":
                        req.Action = ActionLs
                case "watch":
                        req.Action = ActionRead // watch is read-only
                default: // "file"
                        req.Action = ActionRead
                }
                return withPath(req, in.Path, cwd)

        case "write":
                var in struct {
                        Mode   string `json:"mode"`
                        Path   string `json:"path"`
                        OldStr string `json:"old_str"`
                        NewStr string `json:"new_str"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("write args: %w", err)
                }
                mode := strings.ToLower(strings.TrimSpace(in.Mode))
                if mode == "" {
                        mode = "write"
                }
                switch mode {
                case "edit":
                        req.Action = ActionEdit
                default: // "write"
                        req.Action = ActionWrite
                }
                return withPath(req, in.Path, cwd)

        case "edit":
                // Legacy direct-edit caller — kept for backward compatibility with
                // tests that call Extract("edit", ...) directly. Production
                // traffic goes through `write` with mode=edit.
                var in struct {
                        Path     string `json:"path"`
                        FilePath string `json:"file_path"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("edit args: %w", err)
                }
                path := in.Path
                if path == "" {
                        path = in.FilePath
                }
                req.Action = ActionEdit
                return withPath(req, path, cwd)

        case "search":
                // `search` folds grep/find/graph/rank via mode. The path field is
                // the same for all sub-modes; only the Action differs.
                var in struct {
                        Mode string `json:"mode"`
                        Path string `json:"path"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("search args: %w", err)
                }
                mode := strings.ToLower(strings.TrimSpace(in.Mode))
                if mode == "" {
                        mode = "auto"
                }
                switch mode {
                case "grep":
                        req.Action = ActionGrep
                case "find":
                        req.Action = ActionFind
                default:
                        // auto/text/symbol/file/graph/rank — read-only analysis.
                        req.Action = ActionRead
                }
                if in.Path == "" {
                        in.Path = "."
                }
                return withPath(req, in.Path, cwd)

        case "grep":
                var in struct {
                        Path string `json:"path"`
                }
                _ = json.Unmarshal(args, &in)
                if in.Path == "" {
                        in.Path = "."
                }
                req.Action = ActionGrep
                return withPath(req, in.Path, cwd)

        case "find":
                var in struct {
                        Path string `json:"path"`
                }
                _ = json.Unmarshal(args, &in)
                if in.Path == "" {
                        in.Path = "."
                }
                req.Action = ActionFind
                return withPath(req, in.Path, cwd)

        case "ls":
                // Accept object or plain string path.
                var asString string
                if err := json.Unmarshal(args, &asString); err == nil && asString != "" {
                        req.Action = ActionLs
                        return withPath(req, asString, cwd)
                }
                var in struct {
                        Path string `json:"path"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("ls args: %w", err)
                }
                req.Action = ActionLs
                return withPath(req, in.Path, cwd)

        case "agent_spawn", "agent_wait", "agent_list", "agent_cancel":
                req.Action = ActionAgent
                return req, nil

        // Read-only analysis tools that were left unannotated after the
        // collapse (audit J3). Pre-collapse, their tool-name-specific cases
        // existed in this switch — but they all collapsed into `code`,
        // `context`, `docsync`, etc. None of them touch the filesystem
        // mutably, write network state, or run binaries that persist anything
        // — so they all map to ActionRead. This means:
        //   - ModeInteractive: no Ask prompt (the gate's default-case Ask is
        //     skipped because the action is now ActionRead, which the gate
        //     knows to allow under the default policy).
        //   - ModeReadonly: allowed, where previously they were denied by the
        //     readonly fold (Ask → Deny).
        //   - Sub-agent explore/review confinement: still subject to the
        //     AllowedTools list in child_spec.go, which has been widened to
        //     admit these (see audit J4 fix in child_spec.go).
        //
        // `code` mode=build / coverage / property run binaries (build / test
        // with coverage / property-based tests) — ActionBash. `code`
        // mode=scaffold writes new files → ActionWrite. `code` mode=migration
        // mutates the DB schema → ActionBash. All other `code` modes are
        // read-only analysis.
        case "code":
                var in struct {
                        Mode string `json:"mode"`
                }
                _ = json.Unmarshal(args, &in)
                switch strings.ToLower(strings.TrimSpace(in.Mode)) {
                case "build", "coverage", "property", "migration":
                        req.Action = ActionBash // runs binaries / mutates DB
                case "scaffold":
                        req.Action = ActionWrite // writes new files
                default:
                        // api/deadcode/impact/deps/nplusone/testimpact/
                        // vuln/token/runtime/judge/errtrans/error/stack/
                        // errpattern — read-only.
                        req.Action = ActionRead
                }
                return req, nil

        case "context":
                // context mode=snapshot writes a snapshot file, mode=fold is read-only.
                // All other modes (journal/batch/config) are read-only analysis.
                var in struct {
                        Mode string `json:"mode"`
                }
                _ = json.Unmarshal(args, &in)
                if strings.ToLower(strings.TrimSpace(in.Mode)) == "snapshot" {
                        req.Action = ActionWrite
                        return req, nil
                }
                req.Action = ActionRead
                return req, nil

        case "docsync", "vector_search", "session_search", "secret", "fetch", "ask_user", "browser_read", "contract", "budget":
                // All read-only analysis / inspection / network-fetch tools. They
                // never mutate the workspace, never run binaries that persist state,
                // and never spawn processes outside the agent harness. (`fetch` and
                // `browser_read` fetch from the network — read-only in the
                // permission-system sense: they don't write to the workspace.)
                //
                // `test` runs binaries (go test / pytest / etc.) → ActionBash.
                // `document` writes a file → ActionWrite.
                // `permission` is the meta-tool that flips the live permission mode
                // → kept in the default case (Action("permission")), so readonly
                // mode denies it (Ask→Deny fold). Allowing a readonly sub-agent to
                // disable readonly mode would be a privilege escalation.
                // `browser` / `browser_navigate` / `browser_interact` stay in the
                // default case because they drive a browser to submit forms,
                // click buttons, etc. — side effects outside the local filesystem.
                req.Action = ActionRead
                return req, nil

        case "test":
                // `test` runs `go test` / `pytest` / `cargo test` / etc. — same
                // shape as bash (executes a binary, can write coverage files, can
                // hit the network). Classified as ActionBash so the readonly fold
                // denies it and the interactive gate prompts for approval.
                req.Action = ActionBash
                return req, nil

        case "document":
                // `document` writes a rendered .docx/.pdf/.md to disk → ActionWrite.
                req.Action = ActionWrite
                return req, nil

        case "git":
                // `git` is the governed git/gh facade (roadmap: "a real git/gh tool …
                // with APPA gating instead of raw bash"). Mode-aware like `code`:
                // reads never prompt, index writes carry paths so the workspace /
                // sensitive-path checks have something to anchor to, and the
                // ask-by-default tier (commit, push, pull, pr/issue create) is
                // ActionBash — "no commit without explicit request" is enforced by
                // the gate, not doctrine.
                var in struct {
                        Mode   string   `json:"mode"`
                        Name   string   `json:"name"`
                        Files  []string `json:"files"`
                        Remote string   `json:"remote"`
                        Branch string   `json:"branch"`
                        Action string   `json:"action"`
                }
                if err := json.Unmarshal(args, &in); err != nil {
                        return req, fmt.Errorf("git args: %w", err)
                }
                mode := strings.ToLower(strings.TrimSpace(in.Mode))
                switch mode {
                case "status", "diff", "log", "show":
                        req.Action = ActionRead
                case "branch":
                        if strings.TrimSpace(in.Name) != "" {
                                // Creating a branch mutates the repo but has no file path.
                                // Pin Paths to the session dir so checkWrite's
                                // "write/edit without path denied" doesn't fire and the
                                // workspace check anchors to the repo the session runs in.
                                req.Action = ActionWrite
                                return withPath(req, ".", cwd)
                        }
                        req.Action = ActionRead
                case "add", "restore":
                        if len(in.Files) == 0 {
                                // Malformed call — the tool errors on the missing files.
                                // Bash-class (Ask) rather than write-without-path (Deny) so
                                // the model gets the actionable error back and can retry.
                                req.Action = ActionBash
                                req.Command = "git " + mode
                                return req, nil
                        }
                        req.Action = ActionWrite
                        return withPaths(req, in.Files, cwd)
                case "commit", "push", "pull":
                        req.Action = ActionBash
                        // Placeholder renderings, not the raw message: checkBash runs
                        // this string against the bash deny regexes, and a prose commit
                        // message ("document the sudo setup") must not be able to trip
                        // them. The ask dialog pairs this with the call's own args.
                        switch mode {
                        case "commit":
                                req.Command = "git commit (message)"
                        case "push":
                                req.Command = "git push " + gitRemoteOrOrigin(in.Remote)
                                if b := strings.TrimSpace(in.Branch); b != "" {
                                        req.Command += " " + b
                                }
                        default: // pull
                                req.Command = "git pull " + gitRemoteOrOrigin(in.Remote)
                        }
                case "pr", "issue":
                        if strings.ToLower(strings.TrimSpace(in.Action)) == "create" {
                                req.Action = ActionBash
                                req.Command = "gh " + mode + " create (title)"
                                return req, nil
                        }
                        // list (default) / view — read-only gh queries.
                        req.Action = ActionRead
                default:
                        // Unknown or missing mode: the tool's own validation rejects it
                        // before anything runs, but extraction must never default a
                        // malformed call to a read. Bash-class asks.
                        req.Action = ActionBash
                        req.Command = strings.TrimSpace("git " + strings.TrimSpace(in.Mode))
                }
                return req, nil

        case "personal_calendar":
                // The personal-data facade (dashboard chunk 2). Reads are ordinary
                // reads (no paths: the directory comes from config, not args).
                // Writes — create/update — are ActionPersonal: consented out of
                // band by the master switch + the model's personal_write opt-in,
                // both enforced by the tool at call time; the gate's only opinion
                // is the mode fold (readonly denies, isMutating). An unknown
                // action is not a read — it rides the personal class and the
                // tool's own validation rejects it.
                var in struct {
                        Action string `json:"action"`
                }
                _ = json.Unmarshal(args, &in)
                switch strings.ToLower(strings.TrimSpace(in.Action)) {
                case "agenda", "range":
                        req.Action = ActionRead
                default:
                        req.Action = ActionPersonal
                }
                return req, nil

        case "personal_kanban":
                // The board facade (dashboard chunk 3), the calendar's shape:
                // list is an ordinary read; add/move/edit/delete are
                // ActionPersonal — consented out of band by the master switch +
                // the model's personal_write opt-in, both enforced by the tool
                // at call time. An unknown action is not a read — it rides the
                // personal class and the tool's own validation rejects it.
                var in struct {
                        Action string `json:"action"`
                }
                _ = json.Unmarshal(args, &in)
                switch strings.ToLower(strings.TrimSpace(in.Action)) {
                case "list":
                        req.Action = ActionRead
                default:
                        req.Action = ActionPersonal
                }
                return req, nil

        case "personal_mail":
                // The Maildir facade (dashboard chunk 4). Every action is a
                // read — the surface has no write at all, and the envelope-only
                // ruling means even the deepest read touches no file the
                // model could mutate. An unknown action is still not a read:
                // it rides the personal class (so readonly mode denies it)
                // and the tool's own validation rejects it.
                var in struct {
                        Action string `json:"action"`
                }
                _ = json.Unmarshal(args, &in)
                switch strings.ToLower(strings.TrimSpace(in.Action)) {
                case "mailboxes", "messages", "headers":
                        req.Action = ActionRead
                default:
                        req.Action = ActionPersonal
                }
                return req, nil

        default:
                req.Action = Action(toolName)
                return req, nil
        }
}

// gitRemoteOrOrigin renders the push/pull remote for the ask dialog,
// defaulting to origin the way the tool's argv builder does.
func gitRemoteOrOrigin(remote string) string {
        if r := strings.TrimSpace(remote); r != "" {
                return r
        }
        return "origin"
}

func withPath(req Request, path, cwd string) (Request, error) {
        abs, err := AbsCleanAt(strings.TrimSpace(path), cwd)
        if err != nil {
                return req, err
        }
        req.Paths = []string{abs}
        return req, nil
}

// withPaths is withPath for multi-file requests (git add/restore): every
// entry resolves to an absolute cleaned path so the write checks run per
// file.
func withPaths(req Request, paths []string, cwd string) (Request, error) {
        for _, p := range paths {
                abs, err := AbsCleanAt(strings.TrimSpace(p), cwd)
                if err != nil {
                        return req, err
                }
                req.Paths = append(req.Paths, abs)
        }
        return req, nil
}

// Summarize returns a short human-readable summary of the request for UI.
func Summarize(req Request) string {
        switch {
        case req.Command != "":
                return truncate(req.Command, 200)
        case len(req.Paths) > 0:
                return strings.Join(req.Paths, ", ")
        default:
                return string(req.Action)
        }
}
