package memfsck

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func tsetMtime(t *testing.T, root, rel string, mod time.Time) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.Chtimes(p, mod, mod))
}

// ---------- negative ----------

// A missing root is not an early error: the scan and the index read swallow
// their failures, the rebuilt (empty) index differs from the missing one, and
// the repair write is what finally fails.
func TestRun_MissingRoot_Negative(t *testing.T) {
	root := filepath.Join(t.TempDir(), "no-such-memory-dir")
	manifest, rep, err := Run(root)
	require.Error(t, err)
	assert.Nil(t, manifest)
	require.NotNil(t, rep)
	assert.True(t, rep.Changed, "Changed is set before the write fails")
	assert.Contains(t, err.Error(), "MEMORY.md")
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestRun_RootIsFile_Negative(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(root, []byte("i am a file"), 0o644))
	manifest, _, err := Run(root)
	require.Error(t, err)
	assert.Nil(t, manifest)
	assert.Contains(t, err.Error(), "not a directory")
}

// MEMORY.md itself being a directory: the read error is swallowed (index
// treated as empty) and the in-place repair dies on the write.
func TestRun_IndexUnwritable_Negative(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "MEMORY.md"), 0o755))
	twrite(t, root, "a.md", "---\nname: A\ntype: user\ndescription: d\n---\n")
	manifest, _, err := Run(root)
	require.Error(t, err)
	assert.Nil(t, manifest)
	assert.Contains(t, err.Error(), "MEMORY.md")
	assert.Contains(t, err.Error(), "is a directory")
}

// Hostile directory contents: every malformed entry is excluded with a
// warning rather than crashing the pass or landing in the manifest.
func TestRun_HostileEntries_Negative(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	twrite(t, root, "plain.md", "no frontmatter here\n")
	twrite(t, root, "empty.md", "")
	twrite(t, root, "quoted.md",
		"---\nname: \"Quoted Name\"\ndescription: 'single quoted'\ntype: user\n---\nbody\n")
	twrite(t, root, "keys.md",
		"---\nNAME: Upper\ncolor: blue\njust text no colon\n# a comment\ntype: user\nstatus: open\n---\n")
	twrite(t, root, "spaces in name.md",
		"---\nname: Spaces\ntype: user\ndescription: filename with spaces\n---\n")
	twrite(t, root, ".archive/secret.md", "---\nname: Hidden\n---\n")
	twrite(t, root, "notes.txt", "not markdown\n")
	// Dangling symlink: WalkDir records it, ReadFile fails, frontmatter is bad.
	require.NoError(t, os.Symlink("gone.md", filepath.Join(root, "link.md")))

	for i, rel := range []string{"plain.md", "empty.md", "quoted.md", "keys.md", "spaces in name.md"} {
		tsetMtime(t, root, rel, now.Add(time.Duration(i)*time.Minute))
	}
	tsetMtime(t, root, ".archive/secret.md", now)
	twrite(t, root, "MEMORY.md", "")

	manifest, rep, err := Run(root)
	require.NoError(t, err)

	var paths []string
	for _, e := range manifest {
		paths = append(paths, e.Path)
	}
	assert.ElementsMatch(t, []string{"quoted.md", "keys.md", "spaces in name.md"}, paths,
		"only the three well-formed files may enter the manifest")
	assert.ElementsMatch(t, []string{"plain.md", "empty.md", "link.md"}, rep.Excluded)
	for _, rel := range []string{"plain.md", "empty.md", "link.md"} {
		assert.Contains(t, rep.Warnings, "excluded from manifest (bad frontmatter): "+rel)
	}
	for _, w := range rep.Warnings {
		assert.NotContains(t, w, ".archive", "dot directories are skipped silently")
		assert.NotContains(t, w, "notes.txt", "non-markdown files are skipped silently")
	}

	byPath := map[string]Entry{}
	for _, e := range manifest {
		byPath[e.Path] = e
	}
	assert.Equal(t, "Quoted Name", byPath["quoted.md"].Name, "quoted frontmatter values are unquoted")
	assert.Equal(t, "single quoted", byPath["quoted.md"].Description)
	assert.Equal(t, "Upper", byPath["keys.md"].Name, "frontmatter keys are case-insensitive")
	assert.Equal(t, "user", byPath["keys.md"].Type)
	assert.Equal(t, "open", byPath["keys.md"].Status)
	assert.Equal(t, "Spaces", byPath["spaces in name.md"].Name)
}

// The 200-file boundary: the 201st file drops with a warning and the NEWEST
// 200 survive.
func TestRun_FileCapBoundary_Negative(t *testing.T) {
	root := t.TempDir()
	base := time.Now().Add(-time.Hour)
	for i := 0; i < MaxFiles+5; i++ {
		rel := fmt.Sprintf("f%03d.md", i)
		twrite(t, root, rel, fmt.Sprintf("---\nname: F%d\ntype: user\ndescription: d%d\n---\n", i, i))
		tsetMtime(t, root, rel, base.Add(time.Duration(i)*time.Minute))
	}
	twrite(t, root, "MEMORY.md", "")

	manifest, rep, err := Run(root)
	require.NoError(t, err)
	require.Len(t, manifest, MaxFiles)
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, fmt.Sprintf("memory dir capped at %d files (newest kept, 5 dropped)", MaxFiles)) {
			found = true
		}
	}
	assert.True(t, found, "cap warning must fire, got %v", rep.Warnings)

	paths := make(map[string]bool, len(manifest))
	for _, e := range manifest {
		paths[e.Path] = true
	}
	for _, dropped := range []string{"f000.md", "f001.md", "f002.md", "f003.md", "f004.md"} {
		assert.False(t, paths[dropped], "%s is among the 5 oldest and must be dropped", dropped)
	}
	assert.True(t, paths["f204.md"], "the newest file must survive")
}

// ---------- differential ----------

// refIndexLine mirrors the documented index format with plain string ops:
// a "- " or "* " bullet, [title](path), then an em/en/ascii dash separator.
func refIndexLine(ln string) (string, bool) {
	if len(ln) == 0 || (ln[0] != '-' && ln[0] != '*') {
		return "", false
	}
	rest := strings.TrimLeft(ln[1:], " \t\n\f\r") // the \s+ after the bullet
	if !strings.HasPrefix(rest, "[") {
		return "", false
	}
	end := strings.Index(rest, "]")
	if end < 0 {
		return "", false
	}
	after := rest[end+1:]
	if !strings.HasPrefix(after, "(") {
		return "", false
	}
	close := strings.Index(after, ")")
	if close < 0 {
		return "", false
	}
	p := after[1:close]
	if p == "" || strings.ContainsAny(p, " \t\n\f\r)") {
		return "", false
	}
	tail := strings.TrimLeft(after[close+1:], " \t\n\f\r")
	for _, sep := range []string{"—", "–", "-"} {
		if strings.HasPrefix(tail, sep) {
			return p, true
		}
	}
	return "", false
}

// refClip is the 150-rune hook truncation, re-derived independently.
func refClip(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= 150 {
		return string(r)
	}
	return string(r[:149]) + "…"
}

// refScan walks with os.ReadDir instead of filepath.WalkDir: dot dirs,
// non-markdown, and MEMORY.md are skipped; newest first, ties by path.
func refScan(t *testing.T, root string) []string {
	t.Helper()
	var rels []string
	var walk func(dir, rel string)
	walk = func(dir, rel string) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			name := e.Name()
			r := path.Join(rel, name)
			if e.IsDir() {
				if strings.HasPrefix(name, ".") {
					continue
				}
				walk(filepath.Join(dir, name), r)
				continue
			}
			if !strings.HasSuffix(name, ".md") || name == "MEMORY.md" {
				continue
			}
			rels = append(rels, r)
		}
	}
	walk(root, "")
	sort.Slice(rels, func(i, j int) bool { return rels[i] < rels[j] })
	return rels
}

// refFrontmatter is a strings-only frontmatter reader for the corpus shapes.
func refFrontmatter(t *testing.T, full string) (name, desc, typ, status string, ok bool) {
	t.Helper()
	raw, err := os.ReadFile(full)
	require.NoError(t, err)
	lines := strings.Split(strings.ReplaceAll(strings.TrimPrefix(string(raw), "\uFEFF"), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", "", "", false
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return "", "", "", "", false
	}
	for _, ln := range lines[1:end] {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, found := strings.Cut(ln, ":")
		if !found {
			continue
		}
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "name":
			name = val
		case "description":
			desc = val
		case "type":
			typ = val
		case "status":
			status = val
		}
	}
	return name, desc, typ, status, true
}

// oracle: naive os.ReadDir + strings reference implementation in this test —
// an independently written scanner (ReadDir recursion instead of WalkDir), an
// independently written index-line matcher (string ops instead of the regexp),
// and an independently composed repaired index, compared against Run's
// manifest, report, and the MEMORY.md bytes it leaves on disk.
func TestRun_NaiveReference_Differential(t *testing.T) {
	root := t.TempDir()
	now := time.Now()

	twrite(t, root, "MEMORY.md",
		"- [Old title](threads/job.md) — old hook text kept verbatim\n"+
			"- [Ghost](ghost.md) — points nowhere\n"+
			"- [Dup](threads/job.md) — duplicate pointer\n"+
			"  - [NoBullet](notes/note.md) — indented line is not an index entry\n"+
			"* [Star](star.md) – star bullet with en dash\n"+
			"- [Empty]() — empty target is not an index line\n"+
			"- [Dash](dash.md) - ascii dash separator\n")

	twrite(t, root, "threads/job.md",
		"---\nname: Job hunt\ntype: thread\nstatus: open\n"+
			"description: Interviewing at Acme; onsite late April\nopened: 2026-10-01\n---\n- beat\n")
	twrite(t, root, "notes/note.md",
		"---\ntype: project\ndescription: "+strings.Repeat("n", 200)+"\n---\n")
	twrite(t, root, "star.md", "---\nname: Star\ntype: user\nstatus: open\ndescription: star hook\n---\n")
	twrite(t, root, "dash.md", "---\nname: Dash\ndescription: dash hook\n---\n")
	twrite(t, root, "aa.md", "---\nname: Aa\n---\n")
	twrite(t, root, "bb.md", "---\nname: Bb\n---\n")
	twrite(t, root, "nofm.md", "no frontmatter\n")
	twrite(t, root, "data.txt", "not markdown\n")
	twrite(t, root, ".hidden/x.md", "---\nname: Hidden\n---\n")

	// Newest first: aa/bb tie, then dash/star tie, then note, then job.
	tsetMtime(t, root, "aa.md", now)
	tsetMtime(t, root, "bb.md", now)
	tsetMtime(t, root, "dash.md", now.Add(-time.Hour))
	tsetMtime(t, root, "star.md", now.Add(-time.Hour))
	tsetMtime(t, root, "notes/note.md", now.Add(-2*time.Hour))
	tsetMtime(t, root, "threads/job.md", now.Add(-3*time.Hour))
	tsetMtime(t, root, "nofm.md", now.Add(-4*time.Hour))

	// --- reference computation ---
	rels := refScan(t, root)
	type refFile struct {
		rel                 string
		name, desc, typ, st string
		mod                 time.Time
		fmOK                bool
	}
	files := make([]refFile, 0, len(rels))
	for _, rel := range rels {
		full := filepath.Join(root, filepath.FromSlash(rel))
		fi, err := os.Stat(full)
		require.NoError(t, err)
		name, desc, typ, st, ok := refFrontmatter(t, full)
		files = append(files, refFile{rel: rel, name: name, desc: desc, typ: typ, st: st, mod: fi.ModTime(), fmOK: ok})
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].mod.Equal(files[j].mod) {
			return files[i].mod.After(files[j].mod)
		}
		return files[i].rel < files[j].rel
	})

	indexRaw, err := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	require.NoError(t, err)
	idxByPath := map[string]string{}
	var order []string
	deduped := 0
	for _, ln := range strings.Split(string(indexRaw), "\n") {
		p, ok := refIndexLine(ln)
		if !ok {
			continue
		}
		p = path.Clean(filepath.ToSlash(p))
		if p == "MEMORY.md" {
			continue
		}
		if _, dup := idxByPath[p]; dup {
			deduped++
			continue
		}
		idxByPath[p] = ln
		order = append(order, p)
	}

	var wantLines []string
	var wantAdded, wantDropped, wantExcluded []string
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.rel] = true
		if raw, ok := idxByPath[f.rel]; ok {
			wantLines = append(wantLines, raw)
			delete(idxByPath, f.rel)
			continue
		}
		if !f.fmOK {
			wantExcluded = append(wantExcluded, f.rel)
			continue
		}
		title := f.name
		if title == "" {
			title = strings.TrimSuffix(path.Base(f.rel), ".md")
		}
		wantLines = append(wantLines, fmt.Sprintf("- [%s](%s) — %s", title, f.rel, refClip(f.desc)))
		wantAdded = append(wantAdded, f.rel)
	}
	for _, p := range order {
		if _, ok := idxByPath[p]; ok && !seen[p] {
			wantDropped = append(wantDropped, p)
			delete(idxByPath, p)
		}
	}
	wantIndex := []byte(strings.Join(wantLines, "\n") + "\n")

	var wantManifest []Entry
	for _, f := range files {
		if !f.fmOK {
			continue
		}
		wantManifest = append(wantManifest, Entry{
			Path: f.rel, Name: f.name, Description: f.desc,
			Type: f.typ, Status: f.st, ModTime: f.mod,
		})
	}

	// --- implementation ---
	manifest, rep, err := Run(root)
	require.NoError(t, err)

	assert.Equal(t, wantAdded, rep.AddedIndex)
	assert.Equal(t, wantDropped, rep.Dropped)
	assert.Equal(t, deduped, rep.Deduped)
	assert.Equal(t, wantExcluded, rep.Excluded)
	assert.True(t, rep.Changed)

	require.Len(t, manifest, len(wantManifest))
	for i, want := range wantManifest {
		got := manifest[i]
		assert.Equal(t, want.Path, got.Path, "manifest entry %d", i)
		assert.Equal(t, want.Name, got.Name, "manifest entry %d", i)
		assert.Equal(t, want.Description, got.Description, "manifest entry %d", i)
		assert.Equal(t, want.Type, got.Type, "manifest entry %d", i)
		assert.Equal(t, want.Status, got.Status, "manifest entry %d", i)
		assert.True(t, got.ModTime.Equal(want.ModTime), "manifest entry %d", i)
	}

	gotIndex, err := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	require.NoError(t, err)
	assert.Equal(t, string(wantIndex), string(gotIndex),
		"the repaired index must match the naive reference byte for byte")

	// A second pass over the repaired index is a no-op.
	_, rep2, err := Run(root)
	require.NoError(t, err)
	assert.False(t, rep2.Changed, "the repair must be idempotent")
	assert.Empty(t, rep2.AddedIndex)
	assert.Empty(t, rep2.Dropped)
	assert.Zero(t, rep2.Deduped)
}
