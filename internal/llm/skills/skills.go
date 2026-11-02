package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	// SkillFileName is the required file name inside each skill directory.
	SkillFileName        = "SKILL.md"
	frontmatterDelimiter = "---"
)

// Sentinel errors returned when parsing a skill file.
var (
	ErrInvalidFrontmatter   = errors.New("skill file must start with YAML front matter (---)")
	ErrFrontmatterNotClosed = errors.New("skill file frontmatter not properly closed with ---")
	ErrInvalidYAML          = errors.New("skill file frontmatter is invalid")
)

// Skill represents a single skill loaded from a SKILL.md file.
type Skill struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Body          string
	Path          string
	SkillFilePath string
	// Triggers are the routing keywords from the frontmatter's
	// metadata.triggers list. They are the match surface the prompt routes on,
	// and they are a fraction of the description's length — so the catalog
	// renders them instead of the prose.
	Triggers []string
}

// Parse parses a single SKILL.md file, extracting YAML frontmatter and body.
func Parse(skillFilePath string) (*Skill, error) {
	content, err := os.ReadFile(skillFilePath)
	if err != nil {
		return nil, err
	}

	contentStr := string(content)
	if !strings.HasPrefix(contentStr, frontmatterDelimiter) {
		return nil, ErrInvalidFrontmatter
	}

	parts := strings.SplitN(contentStr, frontmatterDelimiter, 3)
	if len(parts) < 3 {
		return nil, ErrFrontmatterNotClosed
	}

	skill, err := parseFrontmatter(parts[1])
	if err != nil {
		return nil, err
	}

	skill.Body = strings.TrimSpace(parts[2])
	skill.Path = filepath.Dir(skillFilePath)
	skill.SkillFilePath = skillFilePath
	return skill, nil
}

// parseFrontmatter reads flat "key: value" lines and common YAML block scalars
// (>, >-, |, |-). Nested YAML maps/lists are not supported.
func parseFrontmatter(fm string) (*Skill, error) {
	skill := &Skill{}
	// inMetadata reports whether the line being examined sits inside the
	// metadata: block, so only its nested triggers key is read while every
	// other nested key keeps being skipped.
	inMetadata := false
	lines := strings.Split(fm, "\n")
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, hasKey := strings.Cut(trimmed, ":")
		// A top-level key ends the metadata block; only its nested triggers key
		// is read.
		if raw != "" && (raw[0] == ' ' || raw[0] == '\t') {
			if inMetadata && strings.TrimSpace(key) == "triggers" {
				if v, ok := strings.CutPrefix(trimmed, "triggers:"); ok {
					value, err := unquote(strings.TrimSpace(v))
					if err != nil {
						return nil, err
					}
					if isYAMLBlockScalar(value) {
						block, next := readBlockScalar(lines, i+1)
						value = block
						i = next
					}
					skill.Triggers = parseTriggers(value)
					inMetadata = false
					continue
				}
			}
			continue
		}
		inMetadata = hasKey && strings.TrimSpace(key) == "metadata"
		if inMetadata {
			continue
		}
		key, val, ok := strings.Cut(trimmed, ":")
		if !ok {
			return nil, ErrInvalidYAML
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			return nil, ErrInvalidYAML
		}
		if isYAMLBlockScalar(val) {
			var body string
			body, i = readBlockScalar(lines, i+1)
			val = body
		} else {
			var err error
			val, err = unquote(val)
			if err != nil {
				return nil, err
			}
		}
		switch key {
		case "name":
			skill.Name = val
		case "description":
			skill.Description = val
		case "license":
			skill.License = val
		case "compatibility":
			skill.Compatibility = val
		}
	}
	return skill, nil
}

// parseTriggers splits a comma-separated trigger list, dropping empties and
// surrounding whitespace. A quoted list unquotes to its inner text, so
// "go,golang,goroutine" yields three triggers.
func parseTriggers(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func isYAMLBlockScalar(val string) bool {
	switch val {
	case ">", ">-", ">|", "|", "|-", "|+":
		return true
	default:
		return false
	}
}

// readBlockScalar collects indented continuation lines for >, >-, |, |-.
// Folded style (>) joins lines with spaces; literal style (|) keeps newlines.
func readBlockScalar(lines []string, start int) (string, int) {
	var parts []string
	i := start
	for ; i < len(lines); i++ {
		raw := lines[i]
		if strings.TrimSpace(raw) == "" {
			parts = append(parts, "")
			continue
		}
		if raw == "" || (raw[0] != ' ' && raw[0] != '\t') {
			break
		}
		parts = append(parts, strings.TrimSpace(raw))
	}
	// Caller loop will i++ after return; step back one so the next key is seen.
	return strings.Join(parts, " "), i - 1
}

func unquote(val string) (string, error) {
	if val == "" {
		return "", nil
	}
	q := val[0]
	if q != '"' && q != '\'' {
		return val, nil
	}
	if len(val) < 2 || val[len(val)-1] != q {
		return "", ErrInvalidYAML
	}
	return val[1 : len(val)-1], nil
}

// ToPromptMarkdown converts skills into the catalog fragment injected into the
// system prompt.
//
// It renders the routing surface, not the prose. The prompt's routing section
// tells the model to match a task against each skill's `triggers`, so shipping
// the full description instead meant the prompt and the data disagreed: the
// model was asked to route on a field that never appeared. Triggers are also
// roughly half the description's length, which cuts the per-turn cost of every
// turn that loads this catalog.
//
// A skill with no triggers still renders, falling back to its description so a
// hand-written third-party skill is never invisible.
func ToPromptMarkdown(skills []*Skill) string {
	if len(skills) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, s := range skills {
		sb.WriteString("### ")
		sb.WriteString(s.Name)
		sb.WriteString("\n\n")
		route, kind := s.Routing()
		sb.WriteString(kind)
		sb.WriteString(route)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// Routing returns the text the catalog shows for a skill and the label it is
// shown under: the trigger list when it has one, otherwise the description so
// the row is never empty.
func (s *Skill) Routing() (text, label string) {
	if len(s.Triggers) > 0 {
		return strings.Join(s.Triggers, ", "), "triggers: "
	}
	return strings.TrimSpace(s.Description), "description: "
}

// Find returns the skill matching name by exact name, case-insensitive name,
// or directory basename. Returns nil if none match.
func Find(list []*Skill, name string) *Skill {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	for _, s := range list {
		if s.Name == name {
			return s
		}
	}
	for _, s := range list {
		if strings.EqualFold(s.Name, name) {
			return s
		}
	}
	for _, s := range list {
		if strings.EqualFold(filepath.Base(s.Path), name) {
			return s
		}
	}
	return nil
}

// LoadSkills walks skillDir looking for SKILL.md files, parses each one, and
// returns the list of skills. If skillDir does not exist, it returns nil, nil.
// Skills that fail to parse are skipped so one bad file cannot hide the rest.
func LoadSkills(skillDir string) ([]*Skill, error) {
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return nil, nil
	}

	var skills []*Skill
	err := filepath.WalkDir(skillDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != SkillFileName {
			return nil
		}
		skill, err := Parse(path)
		if err != nil {
			return nil // skip invalid skill files
		}
		skills = append(skills, skill)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return skills, nil
}
