package webserver

import (
        "bytes"
        "encoding/json"
        "errors"
        "fmt"
        "io"
        "net/http"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "syscall"
        "time"
        "unicode/utf8"

        "github.com/damien1141/a1/internal/permission"
)

// filesapi.go serves the dashboard's file browser (dashboard chunk 5):
// the split-screen pane's three endpoints. Unlike every /api/personal/*
// surface this is WORKSPACE machinery, not personal data — there is no
// personalGate, the master switch has no say here — and unlike the /api/code
// viewer there is no project-root confinement: the ask was a full-system
// browser, so the address bar may go anywhere the server's own user may go.
// What keeps that honest:
//
//      GET /api/files/list?path=   a directory listing (empty path ⇒ cwd)
//      GET /api/files/read?path=   a UTF-8 text preview (capped, sniffed)
//      PUT  /api/files/write       the one write, three refusals deep
//
// The read face serves previews only: files are sniffed (a NUL byte is a
// binary file, invalid UTF-8 is not text) and capped at 2 MiB — an
// oversized file is REFUSED, not truncated, because a half a source file
// presented as the file is a lie a code viewer cannot tell. The write face
// refuses three ways before any byte moves: the CSRF guard (registered
// route composition, like every mutating verb here), read-only mode
// (checked at WRITE TIME against the same source /api/mode reports, so a
// mode switch on an open tab governs the very next Save), and symlinks
// (the final path being a symlink is refused — writing through a
// redirection is how a browser edits the file the user did not point at;
// symlinked DIRECTORIES in the path are fine, the listing surfaces them
// with their targets). No implicit mkdir -p: a missing parent is a 400,
// the same honest-refusal doctrine as every other surface. Writes are
// atomic (temp + rename in the target directory, calendar.Create's
// pattern) and preserve an existing file's permissions — editing a script
// must never strip its execute bit.
//
// The env kill-switch is A1_FILES (the A1_MCP precedent): "0", "false",
// "off", or "no" answers 503 on all three routes, reads included.

const (
        // filesPreviewCap is the read endpoint's hard ceiling. Exactly 2 MiB
        // passes; one byte more is a 413.
        filesPreviewCap = 2 << 20
        // filesWriteCap bounds the write body (the content field marshals to
        // roughly its own size, so the byte ceiling and the file ceiling
        // agree): what the browser cannot preview, it cannot round-trip.
        filesWriteCap = 2 << 20
)

// filesDisabled reports whether A1_FILES kills the whole surface. Read
// live per request (never at registration) so a supervisor restart or a
// test Setenv governs the next request, matching mcp.Disabled.
func filesDisabled() bool {
        v := strings.TrimSpace(strings.ToLower(os.Getenv("A1_FILES")))
        return v == "0" || v == "false" || v == "off" || v == "no"
}

// filesGate is the kill-switch enforcement, personalGate's shape.
func (s *Server) filesGate(next http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
                if filesDisabled() {
                        writeStatus(w, http.StatusServiceUnavailable, "files: disabled (A1_FILES=off)")
                        return
                }
                next(w, r)
        }
}

// fileEntryJSON is one listing row. Dir is NAVIGABILITY, not dirent type:
// a symlink whose target is a directory lists as a dir (clicking it in
// the browser has to go somewhere) while still carrying symlink:true and
// its target — the row is honest about both facts. Size and Mtime are
// lstat values (the link's own, ls -l semantics); broken symlinks keep
// their target and simply list as non-dirs.
type fileEntryJSON struct {
        Name    string `json:"name"`
        Dir     bool   `json:"dir"`
        Size    int64  `json:"size"`
        Mtime   string `json:"mtime"`
        Symlink bool   `json:"symlink,omitempty"`
        Target  string `json:"target,omitempty"`
}

// filesListResponse carries the resolved absolute path (the address bar
// seeds from it — an empty request path means "the server's cwd" and the
// client needs to learn where that landed) plus every entry, sorted
// dirs-first then case-insensitively by name.
type filesListResponse struct {
        Path    string          `json:"path"`
        Entries []fileEntryJSON `json:"entries"`
}

// filesReadResponse is one text preview with the highlight.js language
// the extension implies ("" ⇒ the client renders plaintext).
type filesReadResponse struct {
        Path    string `json:"path"`
        Content string `json:"content"`
        Size    int64  `json:"size"`
        Lang    string `json:"lang,omitempty"`
}

// filesWriteRequest is the PUT body. An absent content field writes an
// empty file — presence of the field is not load-bearing because the edit
// flow always sends the textarea's full text, and an empty file is a
// legitimate file.
type filesWriteRequest struct {
        Path    string `json:"path"`
        Content string `json:"content"`
}

// filesWriteResponse reports the stored truth.
type filesWriteResponse struct {
        Path string `json:"path"`
        Size int    `json:"size"`
}

// fileLanguages maps extensions to highlight.js language ids — a curated
// set the client registers exactly (anything unmapped previews as
// plaintext). Server-side so the browser never guesses twice.
var fileLanguages = map[string]string{
        ".go":    "go",
        ".ts":    "typescript",
        ".tsx":   "tsx",
        ".js":    "javascript",
        ".mjs":   "javascript",
        ".cjs":   "javascript",
        ".jsx":   "jsx",
        ".py":    "python",
        ".rs":    "rust",
        ".rb":    "ruby",
        ".java":  "java",
        ".c":     "c",
        ".h":     "c",
        ".cpp":   "cpp",
        ".cc":    "cpp",
        ".hpp":   "cpp",
        ".cs":    "csharp",
        ".kt":    "kotlin",
        ".swift": "swift",
        ".php":   "php",
        ".lua":   "lua",
        ".json":  "json",
        ".yaml":  "yaml",
        ".yml":   "yaml",
        ".toml":  "toml",
        ".ini":   "ini",
        ".cfg":   "ini",
        ".conf":  "ini",
        ".md":    "markdown",
        ".css":   "css",
        ".scss":  "scss",
        ".html":  "xml",
        ".htm":   "xml",
        ".xml":   "xml",
        ".svg":   "xml",
        ".sh":    "bash",
        ".bash":  "bash",
        ".zsh":   "bash",
        ".sql":   "sql",
        ".diff":  "diff",
        ".patch": "diff",
        ".proto": "protobuf",
}

// fileLanguage resolves a path's highlight.js id: the extension map
// first, then the extensionless basenames that carry their language in
// their name.
func fileLanguage(path string) string {
        ext := strings.ToLower(filepath.Ext(path))
        if lang, ok := fileLanguages[ext]; ok {
                return lang
        }
        switch strings.ToLower(filepath.Base(path)) {
        case "makefile":
                return "makefile"
        case "dockerfile":
                return "dockerfile"
        }
        return ""
}

// resolveFilePath cleans a request path the browser's way: absolute
// paths stay themselves, anything else lands under the server's cwd
// ("current directory" is the ask's word for the empty path, and a bare
// relative name deserves the same resolution). The cleaned absolute
// result is returned for the response's path field.
func resolveFilePath(raw string) (string, error) {
        p := filepath.Clean(strings.TrimSpace(raw))
        if p == "" {
                return os.Getwd()
        }
        if !filepath.IsAbs(p) {
                cwd, err := os.Getwd()
                if err != nil {
                        return "", err
                }
                p = filepath.Join(cwd, p)
        }
        return filepath.Clean(p), nil
}

// mapFilesListErr answers the listing's filesystem failures honestly:
// missing is 404, a file where a directory should be is 400, a directory
// the server's user may not read is 403, and nothing else is a 500.
func mapFilesListErr(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, os.ErrNotExist):
                writeStatus(w, http.StatusNotFound, "files: "+err.Error())
        case errors.Is(err, syscall.ENOTDIR):
                writeStatus(w, http.StatusBadRequest, "files: not a directory (this endpoint lists one)")
        case errors.Is(err, os.ErrPermission):
                writeStatus(w, http.StatusForbidden, "files: "+err.Error())
        default:
                writeStatus(w, http.StatusInternalServerError, "files: "+err.Error())
        }
}

// handleFilesList serves one directory, dirs first.
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
        p, err := resolveFilePath(r.URL.Query().Get("path"))
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, "files: resolve path: "+err.Error())
                return
        }
        dirents, err := os.ReadDir(p)
        if err != nil {
                mapFilesListErr(w, err)
                return
        }
        entries := make([]fileEntryJSON, 0, len(dirents))
        for _, de := range dirents {
                row := fileEntryJSON{Name: de.Name()}
                isLink := de.Type()&os.ModeSymlink != 0
                full := filepath.Join(p, de.Name())
                // Navigability: a true directory, or a symlink whose target
                // is one (a broken symlink resolves to nothing and lists as
                // a plain row with its target spelled out).
                if isLink {
                        row.Symlink = true
                        if target, lerr := os.Readlink(full); lerr == nil {
                                row.Target = target
                        }
                        if fi, serr := os.Stat(full); serr == nil && fi.IsDir() {
                                row.Dir = true
                        }
                } else {
                        row.Dir = de.IsDir()
                }
                // Size/mtime are the entry's own (lstat) — a symlink shows
                // the link, not its target, the ls -l ruling. A racing
                // delete makes Info fail; the entry is gone, so it is
                // skipped, never fabricated.
                if info, ierr := de.Info(); ierr == nil {
                        row.Size = info.Size()
                        row.Mtime = info.ModTime().Format(time.RFC3339)
                } else {
                        continue
                }
                entries = append(entries, row)
        }
        sort.Slice(entries, func(i, j int) bool {
                if entries[i].Dir != entries[j].Dir {
                        return entries[i].Dir // directories first
                }
                li, lj := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
                if li != lj {
                        return li < lj
                }
                return entries[i].Name < entries[j].Name
        })
        writeJSON(w, http.StatusOK, filesListResponse{Path: p, Entries: entries})
}

// handleFilesRead serves one file as a UTF-8 text preview. Symlinks are
// followed for reads (the listing shows them; navigating one is the
// point), so the stat is the target's.
func (s *Server) handleFilesRead(w http.ResponseWriter, r *http.Request) {
        if strings.TrimSpace(r.URL.Query().Get("path")) == "" {
                writeStatus(w, http.StatusBadRequest, "files: path is required")
                return
        }
        p, err := resolveFilePath(r.URL.Query().Get("path"))
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, "files: resolve path: "+err.Error())
                return
        }
        fi, err := os.Stat(p)
        switch {
        case err == nil:
        case errors.Is(err, os.ErrNotExist):
                writeStatus(w, http.StatusNotFound, "files: "+err.Error())
        case errors.Is(err, os.ErrPermission):
                writeStatus(w, http.StatusForbidden, "files: "+err.Error())
        default:
                writeStatus(w, http.StatusInternalServerError, "files: "+err.Error())
        }
        if err != nil {
                return
        }
        if fi.IsDir() {
                writeStatus(w, http.StatusBadRequest, "files: "+filepath.Base(p)+" is a directory (the listing endpoint serves those)")
                return
        }
        if fi.Size() > filesPreviewCap {
                writeStatus(w, http.StatusRequestEntityTooLarge,
                        fmt.Sprintf("files: %s is %d bytes; the preview cap is %d", filepath.Base(p), fi.Size(), filesPreviewCap))
                return
        }
        data, err := os.ReadFile(p)
        if err != nil {
                if errors.Is(err, os.ErrPermission) {
                        writeStatus(w, http.StatusForbidden, "files: "+err.Error())
                        return
                }
                writeStatus(w, http.StatusInternalServerError, "files: "+err.Error())
                return
        }
        // The sniff: a NUL byte is binary (the file(1) heuristic), and
        // invalid UTF-8 is not text the browser could edit honestly — a
        // latin-1 file would come through json as U+FFFD soup, so it is
        // refused with its own name for the message.
        if idx := bytes.IndexByte(data, 0); idx >= 0 {
                writeStatus(w, http.StatusUnsupportedMediaType,
                        fmt.Sprintf("files: %s looks binary (a NUL byte at offset %d) — no text preview", filepath.Base(p), idx))
                return
        }
        if !utf8.Valid(data) {
                writeStatus(w, http.StatusUnsupportedMediaType,
                        fmt.Sprintf("files: %s is not valid UTF-8 text — no preview", filepath.Base(p)))
                return
        }
        writeJSON(w, http.StatusOK, filesReadResponse{
                Path:    p,
                Content: string(data),
                Size:    int64(len(data)),
                Lang:    fileLanguage(p),
        })
}

// handleFilesWrite is PUT /api/files/write — the browser's one write
// face, and the endpoint whose refusals are the chunk's security story:
// read-only mode (at write time, the live /api/mode source), symlinks
// (the final path must not be one), and missing parents (no mkdir -p).
func (s *Server) handleFilesWrite(w http.ResponseWriter, r *http.Request) {
        // Read-only refuses before anything else: "this session writes
        // nothing" includes the user's own file browser, checked against
        // the SAME source the mode switcher reports so an open tab's next
        // Save is governed by the very switch it displays.
        if permission.Mode(s.effectivePermissionMode()) == permission.ModeReadonly {
                writeStatus(w, http.StatusForbidden, "files: read-only mode — this session writes nothing, files included")
                return
        }
        body, err := io.ReadAll(io.LimitReader(r.Body, filesWriteCap+1))
        if err != nil {
                writeStatus(w, http.StatusBadRequest, "files: read body: "+err.Error())
                return
        }
        if len(body) > filesWriteCap {
                writeStatus(w, http.StatusRequestEntityTooLarge,
                        fmt.Sprintf("files: content exceeds the %d byte write cap", filesWriteCap))
                return
        }
        var req filesWriteRequest
        if err := json.Unmarshal(body, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, "files: bad request: "+err.Error())
                return
        }
        if strings.TrimSpace(req.Path) == "" {
                writeStatus(w, http.StatusBadRequest, "files: path is required")
                return
        }
        p, err := resolveFilePath(req.Path)
        if err != nil {
                writeStatus(w, http.StatusInternalServerError, "files: resolve path: "+err.Error())
                return
        }
        // No implicit mkdir -p: the parent must exist and be a directory,
        // and the error says which one is missing.
        parent := filepath.Dir(p)
        pfi, err := os.Stat(parent)
        switch {
        case err == nil:
        case errors.Is(err, os.ErrNotExist):
                writeStatus(w, http.StatusBadRequest, "files: parent directory does not exist (the file browser never creates directories)")
                return
        case errors.Is(err, os.ErrPermission):
                writeStatus(w, http.StatusForbidden, "files: "+err.Error())
                return
        default:
                writeStatus(w, http.StatusInternalServerError, "files: "+err.Error())
                return
        }
        if !pfi.IsDir() {
                writeStatus(w, http.StatusBadRequest, "files: parent path is not a directory")
                return
        }
        // The symlink refusal: Lstat sees the link itself (Stat would
        // happily resolve it). A symlink target — live or broken — is a
        // redirection, and writing through it edits a file the user did
        // not point at; the listing shows the target so the honest path is
        // one click away. Symlinked DIRECTORIES along the path are fine:
        // they are visible in the listing with their targets.
        if li, lerr := os.Lstat(p); lerr == nil {
                if li.Mode()&os.ModeSymlink != 0 {
                        writeStatus(w, http.StatusForbidden, "files: "+filepath.Base(p)+" is a symlink — refusing to write through it")
                        return
                }
                if li.IsDir() {
                        writeStatus(w, http.StatusBadRequest, "files: "+filepath.Base(p)+" is a directory")
                        return
                }
        }
        // Preserve an existing file's permissions (editing a script must
        // not strip its execute bit); new files are 0644.
        mode := os.FileMode(0o644)
        if li, lerr := os.Lstat(p); lerr == nil {
                mode = li.Mode().Perm()
        }
        // Atomic replace, calendar.Create's pattern: a temp file beside
        // the target, fully written, then renamed over — a failed save
        // never truncates the original.
        tmp, err := os.CreateTemp(parent, ".a1write-*")
        if err != nil {
                if errors.Is(err, os.ErrPermission) {
                        writeStatus(w, http.StatusForbidden, "files: "+err.Error())
                        return
                }
                writeStatus(w, http.StatusInternalServerError, "files: "+err.Error())
                return
        }
        tmpName := tmp.Name()
        if _, err := tmp.WriteString(req.Content); err != nil {
                _ = tmp.Close()
                _ = os.Remove(tmpName)
                writeStatus(w, http.StatusInternalServerError, "files: write: "+err.Error())
                return
        }
        if err := tmp.Chmod(mode); err != nil {
                _ = tmp.Close()
                _ = os.Remove(tmpName)
                writeStatus(w, http.StatusInternalServerError, "files: chmod: "+err.Error())
                return
        }
        if err := tmp.Close(); err != nil {
                _ = os.Remove(tmpName)
                writeStatus(w, http.StatusInternalServerError, "files: close: "+err.Error())
                return
        }
        if err := os.Rename(tmpName, p); err != nil {
                _ = os.Remove(tmpName)
                if errors.Is(err, os.ErrPermission) {
                        writeStatus(w, http.StatusForbidden, "files: "+err.Error())
                        return
                }
                writeStatus(w, http.StatusInternalServerError, "files: rename: "+err.Error())
                return
        }
        writeJSON(w, http.StatusOK, filesWriteResponse{Path: p, Size: len(req.Content)})
}
