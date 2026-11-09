// Command toolaudit estimates the token cost of the tool layer: each
// tool's description plus its JSON parameter schema, using the same
// bytes/4 heuristic the /context breakdown uses. Run it after editing
// any tool description to keep the tool budget honest.
//
// Coverage: the default builtin set, list_skills, and the personal trio
// + session_search (mounted with stub deps — definitions only, nothing
// executes). Deliberately NOT covered: the memory quartet, the agent
// quartet, schedule, contract, and budget — their constructors need a
// live store/manager/engine, and their descriptions are small; the
// number below is therefore a floor, not the exact per-session total.
//
// Usage:
//
//      go run ./cmd/toolaudit            # per-tool table + total
//      go run ./cmd/toolaudit -json      # machine-readable output
package main

import (
        "database/sql"
        "encoding/json"
        "flag"
        "fmt"
        "os"
        "sort"
        "strings"

        "github.com/damien1141/a1/internal/tools"
        "github.com/damien1141/a1/internal/tools/calendartool"
        "github.com/damien1141/a1/internal/tools/kanbantool"
        "github.com/damien1141/a1/internal/tools/mailtool"
        "github.com/damien1141/a1/internal/tools/memtool"
        "github.com/damien1141/a1/internal/tools/sessiontool"
        "github.com/damien1141/a1/internal/tools/skilltool"
)

func main() {
        asJSON := flag.Bool("json", false, "emit JSON instead of a table")
        flag.Parse()

        all := auditSet()

        type row struct {
                Name      string `json:"name"`
                DescTok   int    `json:"desc_tokens"`
                SchemaTok int    `json:"schema_tokens"`
                TotalTok  int    `json:"total_tokens"`
        }
        rows := make([]row, 0, len(all))
        total := 0
        for _, t := range all {
                schema, _ := json.Marshal(t.Definition.Params)
                r := row{
                        Name:      t.Definition.Name,
                        DescTok:   len(t.Definition.Description) / 4,
                        SchemaTok: len(schema) / 4,
                }
                r.TotalTok = r.DescTok + r.SchemaTok
                total += r.TotalTok
                rows = append(rows, r)
        }
        sort.Slice(rows, func(i, j int) bool { return rows[i].TotalTok > rows[j].TotalTok })

        if *asJSON {
                out := struct {
                        Tools []row `json:"tools"`
                        Total int   `json:"total_tokens"`
                }{rows, total}
                enc := json.NewEncoder(os.Stdout)
                enc.SetIndent("", "  ")
                _ = enc.Encode(out)
                return
        }

        w := 0
        for _, r := range rows {
                if len(r.Name) > w {
                        w = len(r.Name)
                }
        }
        for _, r := range rows {
                fmt.Printf("%-*s  desc %5d  schema %5d  total %5d\n", w, r.Name, r.DescTok, r.SchemaTok, r.TotalTok)
        }
        fmt.Println(strings.Repeat("-", w+34))
        fmt.Printf("%-*s  %37d\n", w, "TOTAL", total)
}

// auditSet assembles the tool definitions the audit prices. The stub
// deps satisfy the constructors' nil-guards (a nil CalendarDir/DB/MailDir
// collapses to a zero Tool with no name); nothing here executes — the
// Run closures are never called, only the Definition is read.
func auditSet() []tools.Tool {
        all := append(tools.DefaultTools(), skilltool.ListSkillsTool("skills"))
        all = append(all, memtool.Tools(nil)...)
        all = append(all,
                calendartool.CalendarTool(calendartool.Deps{
                        CalendarDir: func() string { return "" },
                }),
                kanbantool.KanbanTool(kanbantool.Deps{
                        DB: func() *sql.DB { return nil },
                }),
                mailtool.MailTool(mailtool.Deps{
                        MailDir: func() string { return "" },
                }),
                sessiontool.SessionSearchTool(".", ""),
        )
        return all
}
