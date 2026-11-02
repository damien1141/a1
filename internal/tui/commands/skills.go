package commands

import (
	"strings"

	"github.com/damien1141/a1/internal/components/palette"
	"github.com/damien1141/a1/internal/llm/skills"
)

// SkillsCommands owns the skills palette command.
type SkillsCommands struct {
	SkillPath string
	Add       skillAdder
}

// skillAdder adds a pending skill to the composer.
type skillAdder interface {
	AddPendingSkill(name string)
}

// Register wires the skills palette entry into r.
func (s *SkillsCommands) Register(r *CommandRegistry) {
	if s == nil || r == nil {
		return
	}
	r.Register(Command{
		Name: "skills",
		Build: func(_ Context) palette.PaletteCommand {
			return buildSkillsPalette(s.SkillPath, s.add)
		},
	})
}

func (s *SkillsCommands) add(name string) {
	if s == nil || s.Add == nil {
		return
	}
	s.Add.AddPendingSkill(name)
}

func buildSkillsPalette(skillPath string, add func(string)) palette.PaletteCommand {
	submenu := skillSubcommands(skillPath, add)
	return palette.PaletteCommand{
		ID:           "skills",
		Noun:         "skills",
		Verb:         "invoke",
		Keywords:     []string{"skill", "use skill", "load skill", "pending"},
		SubmenuTitle: "Select skill",
		Submenu:      submenu,
	}
}

func skillSubcommands(skillPath string, add func(string)) []palette.PaletteCommand {
	list, err := skills.LoadSkills(skillPath)
	if err != nil || len(list) == 0 {
		return []palette.PaletteCommand{{
			ID:       "skills-empty",
			Verb:     "No skills found",
			Disabled: true,
		}}
	}

	out := make([]palette.PaletteCommand, 0, len(list))
	for _, sk := range list {
		name := sk.Name
		if strings.TrimSpace(name) == "" {
			name = strings.TrimSpace(sk.Path)
		}
		out = append(out, palette.PaletteCommand{
			ID:       "skill-" + name,
			Verb:     name,
			Keywords: []string{sk.Description, "skill"},
			Run: func() {
				if add != nil {
					add(name)
				}
			},
		})
	}
	return out
}
