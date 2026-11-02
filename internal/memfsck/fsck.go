// Package memfsck reconciles the memory dir with MEMORY.md (§5.3).
// A prompt rule is a hope; this pass is the guarantee. Run at session
// start and after every extraction-fork completion.
package memfsck

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MaxIndexLines = 200
	MaxIndexBytes = 25 * 1024
	MaxFiles      = 200
	HeadBytes     = 8192
	HeadLines     = 30
	HookMax       = 150
)

type Entry struct {
	Path        string
	Name        string
	Description string
	Type        string
	Status      string
	ModTime     time.Time
}

type Report struct {
	AddedIndex []string
	Dropped    []string
	Deduped    int
	Excluded   []string
	Warnings   []string
	Changed    bool
}

// Run scans root, repairs MEMORY.md in place, and returns the manifest
// plus the report. Caller git-commits on Changed.
func Run(root string) ([]Entry, *Report, error) {
	rep := &Report{}

	indexRaw, _ := os.ReadFile(filepath.Join(root, "MEMORY.md"))
	lines := parseIndex(indexRaw)

	files, warns := scanFiles(root)
	rep.Warnings = append(rep.Warnings, warns...)

	idxByPath := map[string]string{}
	var order []string
	for _, ln := range lines {
		p := normPath(ln.path)
		if p == "MEMORY.md" {
			continue
		}
		if _, dup := idxByPath[p]; dup {
			rep.Deduped++
			continue
		}
		idxByPath[p] = ln.raw
		order = append(order, p)
	}

	var out []string
	seen := map[string]bool{}
	for _, f := range files {
		if seen[f.rel] {
			continue
		}
		seen[f.rel] = true
		if raw, ok := idxByPath[f.rel]; ok {
			out = append(out, raw)
			delete(idxByPath, f.rel)
			continue
		}
		if !f.fmOK {
			rep.Excluded = append(rep.Excluded, f.rel)
			rep.Warnings = append(rep.Warnings,
				"excluded from manifest (bad frontmatter): "+f.rel)
			continue
		}
		title := f.fm.Name
		if title == "" {
			title = strings.TrimSuffix(path.Base(f.rel), ".md")
		}
		hook := clip(f.fm.Description, HookMax)
		out = append(out, fmt.Sprintf("- [%s](%s) — %s", title, f.rel, hook))
		rep.AddedIndex = append(rep.AddedIndex, f.rel)
	}
	for _, p := range order {
		if raw, ok := idxByPath[p]; ok && !seen[p] {
			rep.Dropped = append(rep.Dropped, p)
			_ = raw
			delete(idxByPath, p)
		}
	}

	b := []byte(strings.Join(out, "\n") + "\n")
	b, capWarns := enforceCaps(b)
	rep.Warnings = append(rep.Warnings, capWarns...)

	manifest := make([]Entry, 0, len(files))
	for _, f := range files {
		if f.fmOK && seen[f.rel] {
			manifest = append(manifest, Entry{
				Path: f.rel, Name: f.fm.Name, Description: f.fm.Description,
				Type: f.fm.Type, Status: f.fm.Status, ModTime: f.mod,
			})
		}
	}

	if !bytes.Equal(b, indexRaw) {
		rep.Changed = true
		if err := os.WriteFile(filepath.Join(root, "MEMORY.md"), b, 0o644); err != nil {
			return nil, rep, err
		}
	}
	return manifest, rep, nil
}

// ---------- file scan ----------

type fileEntry struct {
	rel  string
	mod  time.Time
	fm   Frontmatter
	fmOK bool
}

func scanFiles(root string) ([]fileEntry, []string) {
	var warns []string
	var entries []fileEntry
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && strings.HasPrefix(d.Name(), ".") && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || d.Name() == "MEMORY.md" {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		info, _ := d.Info()
		entries = append(entries, fileEntry{
			rel: filepath.ToSlash(rel),
			mod: info.ModTime(),
		})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].mod.Equal(entries[j].mod) {
			return entries[i].mod.After(entries[j].mod)
		}
		return entries[i].rel < entries[j].rel
	})
	if len(entries) > MaxFiles {
		warns = append(warns, fmt.Sprintf(
			"memory dir capped at %d files (newest kept, %d dropped)",
			MaxFiles, len(entries)-MaxFiles))
		entries = entries[:MaxFiles]
	}
	for i := range entries {
		full := filepath.Join(root, filepath.FromSlash(entries[i].rel))
		entries[i].fm, entries[i].fmOK, _ = readFrontmatter(full)
	}
	return entries, warns
}

// ---------- frontmatter ----------

type Frontmatter struct {
	Name, Description, Type, Project, Status, Opened string
}

func readFrontmatter(file string) (Frontmatter, bool, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return Frontmatter{}, false, err
	}
	if len(raw) > HeadBytes {
		raw = raw[:HeadBytes]
	}
	text := strings.TrimPrefix(string(raw), "\uFEFF")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) > HeadLines {
		lines = lines[:HeadLines]
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Frontmatter{}, false, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return Frontmatter{}, false, nil
	}
	var fm Frontmatter
	for _, ln := range lines[1:end] {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "name":
			fm.Name = val
		case "description":
			fm.Description = val
		case "type":
			fm.Type = val
		case "project":
			fm.Project = val
		case "status":
			fm.Status = val
		case "opened":
			fm.Opened = val
		}
	}
	return fm, true, nil
}

// ---------- index parsing ----------

type indexLine struct {
	raw  string
	path string
}

var lineRe = regexp.MustCompile(`^[-*]\s+\[([^\]]*)\]\(([^)\s]+)\)\s*(?:—|–|-)\s*(.*)$`)

func parseIndex(b []byte) []indexLine {
	var out []indexLine
	for _, ln := range strings.Split(strings.TrimPrefix(string(b), "\uFEFF"), "\n") {
		if m := lineRe.FindStringSubmatch(strings.TrimRight(ln, "\r")); m != nil {
			out = append(out, indexLine{raw: ln, path: m[2]})
		}
	}
	return out
}

// ---------- caps ----------

func enforceCaps(b []byte) ([]byte, []string) {
	var warns []string
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > MaxIndexLines {
		warns = append(warns, fmt.Sprintf(
			"memory index truncated to %d lines (was %d)", MaxIndexLines, len(lines)))
		b = []byte(strings.Join(lines[:MaxIndexLines], "\n") + "\n")
	}
	if len(b) > MaxIndexBytes {
		warns = append(warns, fmt.Sprintf(
			"memory index truncated to %d bytes (was %d) — long lines are the usual culprit",
			MaxIndexBytes, len(b)))
		b = b[:MaxIndexBytes]
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			b = b[:i+1]
		}
	}
	return b, warns
}

func normPath(p string) string { return path.Clean(filepath.ToSlash(p)) }

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
