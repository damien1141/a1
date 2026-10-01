## skill library curation

- populate `~/.a1/skills/<lang>/SKILL.md` with idioms, gotchas, tool preferences per language
- harness already parses/injects these via `internal/llm/skills`
- work is curation, not implementation

### suggestions

- **Go** — gofmt/goimports/golines conventions, `make` targets, common pitfall catalog (mutex copy, nil deref, loop var capture)
- **Rust** — borrow-checker idioms, cargo workspace layout, common async pitfalls
- **Python** — `uv`/`poetry` conventions, typing discipline, common stdlib gotchas
- **TypeScript** — project-local TSConfig flags, eslint/prettier rules, module boundary conventions
- **Terraform** — resource naming, state file discipline, drift detection patterns
- **Git** — commit message conventions, branch naming, rebase/merge policy
- **Testing** — testify patterns, table-driven tests, when to use property-based tests
