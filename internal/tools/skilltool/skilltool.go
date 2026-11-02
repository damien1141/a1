package skilltool

import (
        "context"
        "database/sql"
        "encoding/json"
        "fmt"
        "strings"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/llm/skills"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/skillstats"
        "github.com/damien1141/a1/internal/tools/tooldef"
)

// demoteScore and demotePicks are the bandit's demotion thresholds (Bet 07):
// a skill sinks to the bottom of the catalog only when it has been picked
// often enough for the Laplace-smoothed win rate to mean something AND that
// rate has collapsed below 0.35. Fewer picks keeps the catalog order the
// user authored — the bandit earns the right to reorder.
const (
        demoteScore = 0.35
        demotePicks = 5
)

// orderCatalog renders the routed catalog: skills in their on-disk order,
// except demoted skills (per skillstats) sink to the bottom annotated with
// "(recently unhelpful)" so the model can still choose them deliberately.
// db nil (file layout / tests without a vault) keeps the authored order.
func orderCatalog(list []*skills.Skill, db *sql.DB) string {
        if db == nil || len(list) == 0 {
                return skills.ToPromptMarkdown(list)
        }
        stats, err := skillstats.Stats(db)
        if err != nil {
                return skills.ToPromptMarkdown(list) // stats are advisory, never fatal
        }
        bySkill := make(map[string]skillstats.Row, len(stats))
        for _, r := range stats {
                bySkill[r.Skill] = r
        }

        demoted := func(s *skills.Skill) bool {
                r, ok := bySkill[s.Name]
                return ok && r.Picks >= demotePicks && skillScore(r) < demoteScore
        }

        // Partition first, then keep each partition's authored order: a
        // stable demotion instead of a full re-sort the bandit hasn't earned.
        kept := make([]*skills.Skill, 0, len(list))
        var sunk []*skills.Skill
        for _, s := range list {
                if demoted(s) {
                        sunk = append(sunk, s)
                        continue
                }
                kept = append(kept, s)
        }
        if len(sunk) == 0 {
                return skills.ToPromptMarkdown(kept)
        }

        // Annotate by copying: mutating the loaded skill would leak the
        // annotation into anything else reading the same struct.
        annotated := make([]*skills.Skill, 0, len(sunk))
        for _, s := range sunk {
                c := *s
                c.Name = c.Name + " (recently unhelpful)"
                annotated = append(annotated, &c)
        }
        return skills.ToPromptMarkdown(append(kept, annotated...))
}

// skillScore is the same Laplace formula as skillstats.Score, inlined so
// the catalog path needs no per-skill SQL round-trip once Stats has run.
func skillScore(r skillstats.Row) float64 {
        return float64(r.Wins+1) / float64(r.Picks+2)
}

// vaultDB resolves the durable store the bandit ledger lives in. Nil means
// no vault (file layout): the catalog stays in its authored order.
func vaultDB() *sql.DB {
        if v := session.DefaultVault(); v != nil {
                return v.DB()
        }
        return nil
}

// ListSkillsTool returns the `list_skills` tool definition + handler.
// The tool reads the skill directory, generates the catalog via
// skills.ToPromptMarkdown, and returns it as a tool result. The model
// calls this when it needs to discover available skills and their
// triggers — replacing the old system-prompt catalog injection that
// cost ~3.4K tokens on every request.
//
// Typical usage: the system prompt says "Before any hard task, list
// available skills using the `list_skills` tool." The model calls
// list_skills, sees the catalog (name + triggers per skill), reads
// the relevant SKILL.md files via `read`, then proceeds with the
// skill's operating loop.
//
// The tool is cheap (one directory walk + parse, no network), so the
// model can call it freely. The catalog is NOT persisted in the
// session — each call re-reads from disk so newly installed skills
// appear immediately.
func ListSkillsTool(skillPath string) tooldef.Tool {
        return tooldef.Tool{
                Definition: llm.ToolDefinition{
                        Name: "list_skills",
                        Description: `List available skills with their trigger keywords — each skill's
name plus when to apply it. Call this before starting any hard task;
then read the SKILL.md of any matching skill (paths are in the catalog)
for operating loops, checklists, and rules that override your defaults.
Cheap local read; call freely.`,
                        Params: &llm.FunctionParameters{
                                Type:       "object",
                                Properties: llm.Object{},
                                Required:   []string{},
                        },
                },
                DetailFromArgs: func(_ json.RawMessage) string {
                        return "list"
                },
                Run: func(ctx context.Context, _ json.RawMessage) (tooldef.Result, error) {
                        path := strings.TrimSpace(skillPath)
                        if path == "" {
                                return tooldef.Result{
                                        Content: "No skill directory configured.",
                                        Detail:  "list",
                                        Summary: "0 skills",
                                }, nil
                        }
                        list, err := skills.LoadSkills(path)
                        if err != nil {
                                return tooldef.Result{}, fmt.Errorf("list_skills: %w", err)
                        }
                        if len(list) == 0 {
                                return tooldef.Result{
                                        Content: "No skills found in " + path + ".",
                                        Detail:  "list",
                                        Summary: "0 skills",
                                }, nil
                        }
                        catalog := orderCatalog(list, vaultDB())
                        return tooldef.Result{
                                Content: catalog,
                                Detail:  "list",
                                Summary: fmt.Sprintf("%d skills", len(list)),
                                Output:  catalog,
                        }, nil
                },
        }
}
