package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

func writeSkillTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		fp := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(fp), 0o755), "fixture dir for %s", rel)
		require.NoError(t, os.WriteFile(fp, []byte(content), 0o644), "fixture file %s", rel)
	}
	return root
}

type naiveSkill struct {
	name        string
	description string
	body        string
	triggers    []string
}

// naiveUnquote mirrors plain YAML scalar quoting: a value that opens with a
// quote must close with the same one, or the file is invalid.
func naiveUnquote(val string) (string, bool) {
	if val == "" {
		return "", true
	}
	if q := val[0]; q == '"' || q == '\'' {
		if len(val) < 2 || val[len(val)-1] != q {
			return "", false
		}
		return val[1 : len(val)-1], true
	}
	return val, true
}

// naiveParseSkill re-derives one skill with plain string scanning: the
// frontmatter is the text between the leading --- and the next line that
// starts with ---, keys are matched by prefix, folded (>) blocks join their
// indented continuation lines with spaces, and metadata.triggers is the only
// nested key read. Deliberately independent of Parse's SplitN state machine.
func naiveParseSkill(content string) (naiveSkill, bool) {
	if !strings.HasPrefix(content, "---") {
		return naiveSkill{}, false
	}
	rest := content[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return naiveSkill{}, false
	}
	fm, body := rest[:end], rest[end+4:]

	var s naiveSkill
	s.body = strings.TrimSpace(body)
	lines := strings.Split(fm, "\n")
	inMeta := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if !inMeta {
				continue
			}
			if v, ok := strings.CutPrefix(trimmed, "triggers:"); ok {
				val, ok := naiveUnquote(strings.TrimSpace(v))
				if !ok {
					return naiveSkill{}, false
				}
				for _, part := range strings.Split(val, ",") {
					if p := strings.TrimSpace(part); p != "" {
						s.triggers = append(s.triggers, p)
					}
				}
				inMeta = false
			}
			continue
		}
		key, val, hasColon := strings.Cut(trimmed, ":")
		key = strings.TrimSpace(key)
		if !hasColon || key == "" {
			return naiveSkill{}, false
		}
		val = strings.TrimSpace(val)
		inMeta = key == "metadata"
		if inMeta {
			continue
		}
		switch key {
		case "name":
			v, ok := naiveUnquote(val)
			if !ok {
				return naiveSkill{}, false
			}
			s.name = v
		case "description":
			if val != ">" && val != ">-" {
				v, ok := naiveUnquote(val)
				if !ok {
					return naiveSkill{}, false
				}
				s.description = v
				continue
			}
			var parts []string
			for j := i + 1; j < len(lines); j++ {
				l := lines[j]
				if strings.TrimSpace(l) == "" {
					parts = append(parts, "")
					continue
				}
				if !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t") {
					break
				}
				parts = append(parts, strings.TrimSpace(l))
				i = j
			}
			s.description = strings.Join(parts, " ")
		}
	}
	return s, true
}

// naiveLoadSkills re-derives the library with os.ReadDir recursion and the
// naive parser above — a separate walk and a separate frontmatter reader from
// LoadSkills's filepath.WalkDir + Parse.
func naiveLoadSkills(t *testing.T, root string) map[string]naiveSkill {
	t.Helper()
	out := map[string]naiveSkill{}
	var walk func(dir string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if e.IsDir() {
				walk(p)
				continue
			}
			if e.Name() != "SKILL.md" { // the naive walk hardcodes the name
				continue
			}
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			s, ok := naiveParseSkill(string(data))
			if !ok {
				continue // one bad file must not hide the rest
			}
			out[p] = s
		}
	}
	walk(root)
	return out
}

// oracle: naive reference implementation in this test — a fixture skills tree
// (nested dirs, folded descriptions, metadata triggers, invalid files,
// non-skill files) is scanned with os.ReadDir + string scanning, and the
// resulting name/description/body/triggers per file are compared against
// LoadSkills's filepath.WalkDir + Parse on the same tree.
func TestLoadSkills_VsNaiveReadDirScan_Differential(t *testing.T) {
	root := writeSkillTree(t, map[string]string{
		"alpha/SKILL.md": "---\nname: Alpha Skill\ndescription: does alpha things\nlicense: MIT\n" +
			"metadata:\n  author: someone\n  triggers: \"go,golang,goroutine\"\n---\nUse alpha when asked.\n",
		"beta/SKILL.md": "---\nname: beta\ntriggers: top-level triggers are not read\n---\n",
		"nested/deep/gamma/SKILL.md": "---\nname: gamma\ndescription: >-\n  folded one\n  folded two\n" +
			"license: BSD\n---\nGamma body.\n",
		// A file named SKILL.md at any depth loads, even under references/.
		"delta/references/SKILL.md": "---\nname: delta-ref\n---\nA SKILL.md under references loads too.\n",
		"broken/SKILL.md":           "no frontmatter here\n",
		"brokenquote/SKILL.md":      "---\nname: \"unclosed\n---\nbody\n",
		"nocolon/SKILL.md":          "---\nname: ok\na line without a colon\n---\nbody\n",
		"readme.md":                 "not a skill\n",
		"nested/notes.txt":          "also not a skill\n",
	})

	want := naiveLoadSkills(t, root)
	require.Len(t, want, 4, "naive reference sanity: four parseable SKILL.md files")

	got, err := LoadSkills(root)
	require.NoError(t, err)
	require.NotEmpty(t, got)

	byPath := make(map[string]*Skill, len(got))
	for _, s := range got {
		byPath[s.SkillFilePath] = s
	}
	assert.Equal(t, len(want), len(byPath), "LoadSkills must find exactly the naive walk's files")
	for path, expected := range want {
		actual, ok := byPath[path]
		require.True(t, ok, "LoadSkills must load %s", path)
		assert.Equal(t, expected.name, actual.Name, "name for %s", path)
		assert.Equal(t, expected.description, actual.Description, "description for %s", path)
		assert.Equal(t, expected.body, actual.Body, "body for %s", path)
		assert.Equal(t, expected.triggers, actual.Triggers, "triggers for %s", path)
	}
}

// oracle: naive reference implementation in this test — a single-pass scorer
// (exact name 3, case-folded name 2, directory basename 1; highest score
// wins, first skill on ties) re-derives the documented lookup precedence
// independently of Find's three sequential loops.
func TestFind_VsNaivePrecedenceScorer_Differential(t *testing.T) {
	list := []*Skill{
		{Name: "Go Pro", Path: "/skills/go-pro"},
		{Name: "rust", Path: "/skills/gopher"},
		{Name: "C", Path: "/skills/c-lang"},
		{Name: "gopher", Path: "/skills/zz-last"},
	}
	naiveFind := func(query string) string {
		q := strings.TrimSpace(query)
		if q == "" {
			return ""
		}
		best, bestScore := "", 0
		for _, s := range list {
			score := 0
			switch {
			case s.Name == q:
				score = 3
			case strings.EqualFold(s.Name, q):
				score = 2
			case strings.EqualFold(filepath.Base(s.Path), q):
				score = 1
			}
			if score > bestScore {
				best, bestScore = s.Name, score
			}
		}
		return best
	}

	for _, query := range []string{
		"Go Pro", "go pro", "go-pro", "GO-PRO",
		"rust", "RUST", "  rust  ",
		"gopher", // a later exact name outranks an earlier basename match
		"C", "c", "c-lang",
		"missing", "", "   ",
	} {
		want := naiveFind(query)
		got := Find(list, query)
		if want == "" {
			assert.Nil(t, got, "query %q must match nothing", query)
			continue
		}
		require.NotNil(t, got, "query %q must match %q", query, want)
		assert.Equal(t, want, got.Name, "query %q", query)
	}

	assert.Nil(t, Find(nil, "anything"), "an empty library matches nothing")
}

// oracle: round-trip through an independent parser — the rendered catalog is
// parsed back line by line; each skill must surface as a "### name" section
// followed by exactly its routing line (joined triggers, or the description
// fallback), in input order.
func TestToPromptMarkdown_ParseBack_Differential(t *testing.T) {
	skills := []*Skill{
		{Name: "golang-pro", Triggers: []string{"go", "golang", "goroutine"}},
		{Name: "homegrown", Description: "Does our house style"},
		{Name: "api-design", Triggers: []string{"rest"}, Description: "ignored when triggers exist"},
	}

	type section struct{ name, routing string }
	var got []section
	cur := -1
	for _, line := range strings.Split(ToPromptMarkdown(skills), "\n") {
		switch {
		case strings.HasPrefix(line, "### "):
			got = append(got, section{name: strings.TrimPrefix(line, "### ")})
			cur = len(got) - 1
		case line == "":
			// separator
		default:
			if cur >= 0 && got[cur].routing == "" {
				got[cur].routing = line
			}
		}
	}

	want := []section{
		{name: "golang-pro", routing: "triggers: go, golang, goroutine"},
		{name: "homegrown", routing: "description: Does our house style"},
		{name: "api-design", routing: "triggers: rest"},
	}
	assert.Equal(t, want, got, "the parsed-back catalog must equal the input skills' routing surface, in order")
}
