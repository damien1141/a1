package webserver

import (
        "bytes"
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/permission"
)

// The files endpoints' trio (dashboard chunk 5). The browser pane's own
// logic is plain composition; this file pins the HTTP layer: the write's
// three refusals (read-only mode, symlinks, no mkdir -p), the preview's
// sniff-and-cap doctrine, the kill-switch, and the listing against an
// os.ReadDir oracle that re-derives every field independently.

// newFilesServer builds a plain fixture server — the files surface reads
// no config, so the personal section is empty.
func newFilesServer(t *testing.T) *Server {
        t.Helper()
        return newPersonalFixtureProject(t, "")
}

// getFiles exercises one GET through the full route mux.
func getFiles(t *testing.T, srv *Server, path string) (int, string) {
        t.Helper()
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
        return rec.Code, rec.Body.String()
}

// putFiles exercises the write through the full route mux (guard and
// filesGate included). A same-origin look is simulated by omitting
// Origin, exactly what the browser's own fetch may do.
func putFiles(t *testing.T, srv *Server, path, content string, origin string) (int, string) {
        t.Helper()
        body, err := json.Marshal(filesWriteRequest{Path: path, Content: content})
        require.NoError(t, err)
        req := httptest.NewRequest(http.MethodPut, "/api/files/write", bytes.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        if origin != "" {
                req.Header.Set("Origin", origin)
        }
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, req)
        return rec.Code, rec.Body.String()
}

// ---------- positive ----------

func TestFilesWriteReadRoundTrip_Positive(t *testing.T) {
        srv := newFilesServer(t)
        dir := t.TempDir()

        // Content chosen to stress every boundary the round-trip crosses:
        // 4-byte emoji, CJK, quotes (JSON escaping), CRLF (never normalized),
        // a trailing newline, and an initial BOM-less plain line.
        content := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"你好，世界 🌍\")\r\n\t// \"quoted\" comment\n}\n"
        code, raw := putFiles(t, srv, filepath.Join(dir, "hello.go"), content, "")
        require.Equal(t, http.StatusOK, code, raw)
        var written filesWriteResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &written))
        assert.Equal(t, filepath.Join(dir, "hello.go"), written.Path)
        assert.Equal(t, len(content), written.Size)

        // Byte fidelity: what went in is what comes out — not a normalized,
        // re-quoted, or re-encoding approximation of it.
        code, raw = getFiles(t, srv, "/api/files/read?path="+filepath.Join(dir, "hello.go"))
        require.Equal(t, http.StatusOK, code, raw)
        var read filesReadResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &read))
        assert.Equal(t, content, read.Content, "the round-trip is byte-exact")
        assert.Equal(t, int64(len(content)), read.Size)
        assert.Equal(t, "go", read.Lang, "the extension picks the highlight language")

        // The listing sees the file, and the empty path seeds the address
        // bar with the server's own cwd.
        code, raw = getFiles(t, srv, "/api/files/list?path="+dir)
        require.Equal(t, http.StatusOK, code, raw)
        var list filesListResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.Equal(t, dir, list.Path)
        require.Len(t, list.Entries, 1)
        assert.Equal(t, "hello.go", list.Entries[0].Name)
        assert.False(t, list.Entries[0].Dir)
        assert.Equal(t, int64(len(content)), list.Entries[0].Size)

        cwd, err := os.Getwd()
        require.NoError(t, err)
        code, raw = getFiles(t, srv, "/api/files/list")
        require.Equal(t, http.StatusOK, code, raw)
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.Equal(t, cwd, list.Path, "an empty path means the server's cwd")

        // An overwrite preserves the file's permissions — editing a script
        // must not strip its execute bit.
        script := filepath.Join(dir, "run.sh")
        require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho v1\n"), 0o755))
        code, _ = putFiles(t, srv, script, "#!/bin/sh\necho v2\n", "")
        require.Equal(t, http.StatusOK, code)
        fi, err := os.Stat(script)
        require.NoError(t, err)
        assert.Equal(t, os.FileMode(0o755), fi.Mode().Perm(), "the write preserves the mode")
        got, err := os.ReadFile(script)
        require.NoError(t, err)
        assert.Equal(t, "#!/bin/sh\necho v2\n", string(got))
}

// ---------- negative ----------

func TestFilesPreviewRefusals_Negative(t *testing.T) {
        srv := newFilesServer(t)
        dir := t.TempDir()

        // Binary: one NUL byte is enough, wherever it sits.
        bin := filepath.Join(dir, "blob.bin")
        require.NoError(t, os.WriteFile(bin, []byte("text then\x00binary"), 0o644))
        code, raw := getFiles(t, srv, "/api/files/read?path="+bin)
        assert.Equal(t, http.StatusUnsupportedMediaType, code, raw)
        assert.Contains(t, raw, "binary")

        // Not UTF-8: latin-1 high bytes without any NUL — json would turn
        // them into U+FFFD soup, so the preview refuses instead of lying.
        latin := filepath.Join(dir, "latin.txt")
        require.NoError(t, os.WriteFile(latin, []byte{0x63, 0x61, 0x66, 0xE9, 0x0A}, 0o644))
        code, raw = getFiles(t, srv, "/api/files/read?path="+latin)
        assert.Equal(t, http.StatusUnsupportedMediaType, code, raw)
        assert.Contains(t, raw, "UTF-8")

        // Oversized: the cap is refused, not truncated — a half file
        // presented as the file is the one lie a code viewer cannot tell.
        big := filepath.Join(dir, "big.txt")
        require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("a"), filesPreviewCap+1), 0o644))
        code, raw = getFiles(t, srv, "/api/files/read?path="+big)
        assert.Equal(t, http.StatusRequestEntityTooLarge, code, raw)

        // Exactly at the cap passes (the boundary belongs to the allowed
        // side; one byte over refuses).
        require.NoError(t, os.WriteFile(big, bytes.Repeat([]byte("a"), filesPreviewCap), 0o644))
        code, _ = getFiles(t, srv, "/api/files/read?path="+big)
        assert.Equal(t, http.StatusOK, code)

        // A directory is the listing endpoint's business.
        code, raw = getFiles(t, srv, "/api/files/read?path="+dir)
        assert.Equal(t, http.StatusBadRequest, code, raw)

        // Missing file, missing dir, listing a file, and a missing path
        // parameter all answer honestly, never 500.
        code, _ = getFiles(t, srv, "/api/files/read?path="+filepath.Join(dir, "nope.txt"))
        assert.Equal(t, http.StatusNotFound, code)
        code, _ = getFiles(t, srv, "/api/files/list?path="+filepath.Join(dir, "nope"))
        assert.Equal(t, http.StatusNotFound, code)
        code, _ = getFiles(t, srv, "/api/files/list?path="+bin)
        assert.Equal(t, http.StatusBadRequest, code)
        code, _ = getFiles(t, srv, "/api/files/read")
        assert.Equal(t, http.StatusBadRequest, code)

        // The write body has its own cap, one byte over is a 413, and a
        // body that will not decode is a 400.
        code, _ = putFiles(t, srv, filepath.Join(dir, "cap.txt"), strings.Repeat("a", filesWriteCap+1), "")
        assert.Equal(t, http.StatusRequestEntityTooLarge, code)
        req := httptest.NewRequest(http.MethodPut, "/api/files/write", strings.NewReader("{not json"))
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, req)
        assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestFilesWriteRefusals_Negative(t *testing.T) {
        srv := newFilesServer(t)
        dir := t.TempDir()

        // A missing parent is a 400 — the browser never mkdir -p.
        code, raw := putFiles(t, srv, filepath.Join(dir, "no", "such", "dir", "f.txt"), "x", "")
        assert.Equal(t, http.StatusBadRequest, code, raw)
        assert.Contains(t, raw, "never creates directories")

        // A directory as the target is a 400, not a clobber.
        code, _ = putFiles(t, srv, dir, "x", "")
        assert.Equal(t, http.StatusBadRequest, code)

        // A symlink target is refused, and the file behind it is untouched.
        target := filepath.Join(dir, "real.txt")
        require.NoError(t, os.WriteFile(target, []byte("original"), 0o644))
        link := filepath.Join(dir, "link.txt")
        require.NoError(t, os.Symlink(target, link))
        code, raw = putFiles(t, srv, link, "replaced", "")
        assert.Equal(t, http.StatusForbidden, code, raw)
        assert.Contains(t, raw, "symlink")
        got, err := os.ReadFile(target)
        require.NoError(t, err)
        assert.Equal(t, "original", string(got), "the refusal left the target untouched")

        // A BROKEN symlink is refused the same way (Lstat sees the link;
        // Stat would miss it entirely).
        broken := filepath.Join(dir, "broken.txt")
        require.NoError(t, os.Symlink(filepath.Join(dir, "gone.txt"), broken))
        code, _ = putFiles(t, srv, broken, "x", "")
        assert.Equal(t, http.StatusForbidden, code)

        // Read-only mode refuses at write time — the same source the mode
        // switcher reports, so flipping the pill governs the next Save.
        _, err = srv.modeStore().setMode(permission.ModeReadonly)
        require.NoError(t, err)
        code, raw = putFiles(t, srv, filepath.Join(dir, "fresh.txt"), "x", "")
        assert.Equal(t, http.StatusForbidden, code, raw)
        assert.Contains(t, raw, "read-only")
        _, err = os.Stat(filepath.Join(dir, "fresh.txt"))
        assert.True(t, os.IsNotExist(err), "the refused write created nothing")
        _, err = srv.modeStore().setMode(permission.ModeInteractive)
        require.NoError(t, err)
        code, _ = putFiles(t, srv, filepath.Join(dir, "fresh.txt"), "x", "")
        assert.Equal(t, http.StatusOK, code, "flipping back re-enables the write")

        // The CSRF guard: a cross-origin PUT never reaches the handler.
        code, raw = putFiles(t, srv, filepath.Join(dir, "evil.txt"), "x", "http://evil.example")
        assert.Equal(t, http.StatusForbidden, code, raw)
        _, err = os.Stat(filepath.Join(dir, "evil.txt"))
        assert.True(t, os.IsNotExist(err))

        // An unreadable directory answers 403, not 500 — unless the tests
        // run as root, where chmod is theater.
        if os.Geteuid() != 0 {
                locked := filepath.Join(dir, "locked")
                require.NoError(t, os.MkdirAll(locked, 0o755))
                require.NoError(t, os.WriteFile(filepath.Join(locked, "secret.txt"), []byte("s"), 0o644))
                require.NoError(t, os.Chmod(locked, 0o0000))
                defer os.Chmod(locked, 0o755) //nolint:errcheck // best-effort cleanup
                code, raw = getFiles(t, srv, "/api/files/list?path="+locked)
                assert.Equal(t, http.StatusForbidden, code, raw)
        }
}

func TestFilesKillSwitch_Negative(t *testing.T) {
        srv := newFilesServer(t)
        dir := t.TempDir()

        // Every spelling of the switch, every endpoint: 503 before any
        // handler runs, writes included. "OFF " and " No" prove the
        // value is trimmed and lowercased before it is judged.
        for _, v := range []string{"off", "0", "false", "no", "OFF ", " No"} {
                t.Setenv("A1_FILES", v)
                code, raw := getFiles(t, srv, "/api/files/list?path="+dir)
                assert.Equal(t, http.StatusServiceUnavailable, code, "A1_FILES=%s list", v)
                assert.Contains(t, raw, "A1_FILES=off")
                code, _ = getFiles(t, srv, "/api/files/read?path="+dir)
                assert.Equal(t, http.StatusServiceUnavailable, code, "A1_FILES=%s read", v)
                code, _ = putFiles(t, srv, filepath.Join(dir, "f.txt"), "x", "")
                assert.Equal(t, http.StatusServiceUnavailable, code, "A1_FILES=%s write", v)
        }

        // Anything else (including the empty string) leaves the surface on.
        for _, v := range []string{"", "on", "1", "yes"} {
                t.Setenv("A1_FILES", v)
                code, _ := getFiles(t, srv, "/api/files/list?path="+dir)
                assert.Equal(t, http.StatusOK, code, "A1_FILES=%q stays on", v)
        }
}

// ---------- differential ----------

// TestFilesListMatchesReadDirOracle_Differential seeds the cases that
// make listing code lie (symlinks both live and broken, dotfiles, an
// extensionless file, nested dirs, case-sensitive ordering) and compares
// the endpoint against an oracle that re-derives every field from
// os.ReadDir + os.Stat/os.Readlink independently of the handler.
func TestFilesListMatchesReadDirOracle_Differential(t *testing.T) {
        srv := newFilesServer(t)
        dir := t.TempDir()

        require.NoError(t, os.MkdirAll(filepath.Join(dir, "Beta"), 0o755))
        require.NoError(t, os.MkdirAll(filepath.Join(dir, "alpha", "deep"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "alpha", "z.go"), []byte("package z\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("dot\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "noext"), []byte("no extension\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# hi\n"), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "Zebra.txt"), []byte("caps\n"), 0o644))
        require.NoError(t, os.Symlink(filepath.Join(dir, "readme.md"), filepath.Join(dir, "link-file")))
        require.NoError(t, os.Symlink(filepath.Join(dir, "alpha"), filepath.Join(dir, "link-dir")))
        require.NoError(t, os.Symlink(filepath.Join(dir, "alpha", "gone"), filepath.Join(dir, "link-broken")))

        // The endpoint.
        code, raw := getFiles(t, srv, "/api/files/list?path="+dir)
        require.Equal(t, http.StatusOK, code, raw)
        var got filesListResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &got))
        require.Equal(t, dir, got.Path)

        // The oracle: same contract, re-derived. Dir is NAVIGABILITY (a
        // symlink to a directory lists as one), size/mtime are the entry's
        // own lstat values, targets come from Readlink.
        dirents, err := os.ReadDir(dir)
        require.NoError(t, err)
        want := make([]filesOracleRow, 0, len(dirents))
        for _, de := range dirents {
                full := filepath.Join(dir, de.Name())
                r := filesOracleRow{name: de.Name()}
                info, ierr := de.Info()
                require.NoError(t, ierr, de.Name())
                r.size = info.Size()
                if de.Type()&os.ModeSymlink != 0 {
                        r.symlink = true
                        target, lerr := os.Readlink(full)
                        require.NoError(t, lerr, de.Name())
                        r.target = target
                        if fi, serr := os.Stat(full); serr == nil && fi.IsDir() {
                                r.dir = true
                        }
                } else {
                        r.dir = de.IsDir()
                }
                want = append(want, r)
        }
        sortOracleRows(want)

        require.Len(t, got.Entries, len(want), "every dirent appears exactly once")
        for i, w := range want {
                g := got.Entries[i]
                assert.Equal(t, w.name, g.Name, "row %d", i)
                assert.Equal(t, w.dir, g.Dir, "row %d (%s): dir means navigable", i, w.name)
                assert.Equal(t, w.size, g.Size, "row %d (%s)", i, w.name)
                assert.Equal(t, w.symlink, g.Symlink, "row %d (%s)", i, w.name)
                assert.Equal(t, w.target, g.Target, "row %d (%s)", i, w.name)
                assert.NotEmpty(t, g.Mtime, "row %d (%s) carries an mtime", i, w.name)
        }

        // The ordering ruling, pinned directly: dirs (and symlinked dirs)
        // first, then case-insensitive names, with the raw name breaking
        // exact ties.
        names := make([]string, 0, len(got.Entries))
        for _, e := range got.Entries {
                names = append(names, fmt.Sprintf("%d:%s", boolToInt(e.Dir), e.Name))
        }
        expectedOrder := []string{
                "1:alpha", "1:Beta", "1:link-dir",
                "0:.hidden", "0:link-broken", "0:link-file", "0:noext", "0:readme.md", "0:Zebra.txt",
        }
        assert.Equal(t, expectedOrder, names, "dirs first, then case-insensitive names")
}

// filesOracleRow is the differential's independent derivation of a
// listing row.
type filesOracleRow struct {
        name    string
        dir     bool
        size    int64
        symlink bool
        target  string
}

// sortOracleRows is the oracle's independent copy of the ordering
// contract (dirs first, case-insensitive, raw-name tie-break) — kept as
// a local mirror so the differential compares two derivations, not one.
func sortOracleRows(rows []filesOracleRow) {
        for i := 1; i < len(rows); i++ {
                for j := i; j > 0; j-- {
                        a, b := rows[j-1], rows[j]
                        less := false
                        if a.dir != b.dir {
                                less = b.dir
                        } else if la, lb := strings.ToLower(a.name), strings.ToLower(b.name); la != lb {
                                less = la > lb
                        } else {
                                less = a.name > b.name
                        }
                        if !less {
                                break
                        }
                        rows[j-1], rows[j] = rows[j], rows[j-1]
                }
        }
}

func boolToInt(b bool) int {
        if b {
                return 1
        }
        return 0
}
