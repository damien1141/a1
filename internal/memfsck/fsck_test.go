package memfsck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mk(t *testing.T) string {
	t.Helper()
	mem := filepath.Join(t.TempDir(), "memory")
	os.MkdirAll(filepath.Join(mem, "threads"), 0o755)
	return mem
}

func wr(t *testing.T, mem, rel, content string) {
	t.Helper()
	p := filepath.Join(mem, filepath.FromSlash(rel))
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFsckRepairs(t *testing.T) {
	mem := mk(t)
	wr(t, mem, "MEMORY.md",
		"- [Job hunt](threads/job_hunt.md) — onsite at Acme pending\n"+
			"- [Ghost](ghost.md) — points nowhere\n"+
			"- [Dup](threads/job_hunt.md) — duplicate pointer\n")
	wr(t, mem, "threads/job_hunt.md",
		"---\nname: Job hunt\ntype: thread\nstatus: open\n"+
			"description: Interviewing at Acme; onsite late April\n---\n- beat\n")
	wr(t, mem, "orphan.md",
		"---\nname: Orphan\ntype: project\ndescription: never indexed\n---\n")
	wr(t, mem, "bom.md",
		"\uFEFF---\nname: BOM file\ntype: user\ndescription: has a byte order mark\n---\n")
	wr(t, mem, "unterminated.md",
		"---\nname: Unterminated\ndescription: no closing fence\n")

	manifest, rep, err := Run(mem)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.AddedIndex) != 2 {
		t.Fatalf("added: %v", rep.AddedIndex)
	}
	if len(rep.Dropped) != 1 || rep.Dropped[0] != "ghost.md" {
		t.Fatalf("dropped: %v", rep.Dropped)
	}
	if rep.Deduped != 1 {
		t.Fatalf("deduped: %d", rep.Deduped)
	}
	if len(rep.Excluded) != 1 || rep.Excluded[0] != "unterminated.md" {
		t.Fatalf("excluded: %v", rep.Excluded)
	}
	byPath := map[string]bool{}
	for _, e := range manifest {
		byPath[e.Path] = true
	}
	if !byPath["bom.md"] {
		t.Fatal("BOM file should be in manifest")
	}
	if byPath["unterminated.md"] {
		t.Fatal("unterminated frontmatter must be excluded")
	}
	idx, _ := os.ReadFile(filepath.Join(mem, "MEMORY.md"))
	if strings.Count(string(idx), "threads/job_hunt.md") != 1 {
		t.Fatalf("index should contain exactly one pointer:\n%s", idx)
	}
	if strings.Contains(string(idx), "ghost.md") {
		t.Fatal("dangling pointer survived")
	}
}

func TestByteCapFires(t *testing.T) {
	mem := mk(t)
	for i := 0; i < 200; i++ {
		fm := fmt.Sprintf("---\nname: Topic %d\ntype: user\ndescription: %s\n---\n",
			i, strings.Repeat("x", 300))
		wr(t, mem, fmt.Sprintf("f%d.md", i), fm)
	}
	wr(t, mem, "MEMORY.md", "")
	_, rep, err := Run(mem)
	if err != nil {
		t.Fatal(err)
	}
	fired := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "bytes") {
			fired = true
		}
	}
	if !fired {
		t.Fatalf("byte cap should fire: %v", rep.Warnings)
	}
	idx, _ := os.ReadFile(filepath.Join(mem, "MEMORY.md"))
	if len(idx) > MaxIndexBytes {
		t.Fatalf("index is %d bytes > cap", len(idx))
	}
	if !strings.HasSuffix(string(idx), "\n") {
		t.Fatal("truncation must end on a line boundary")
	}
}

func TestCRLFFrontmatter(t *testing.T) {
	mem := mk(t)
	wr(t, mem, "crlf.md",
		"---\r\nname: CRLF\r\ntype: user\r\ndescription: windows endings\r\n---\r\n")
	manifest, _, err := Run(mem)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 1 || manifest[0].Name != "CRLF" {
		t.Fatalf("manifest: %+v", manifest)
	}
}
