package agent

import (
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools"
)

// ChildSpec is the capability profile for a sub-agent role.
type ChildSpec struct {
	Role        job.Role
	Tools       []tools.Tool
	Mode        permission.Mode               // used when EngineRunner.Gate is nil
	Hint        string                        // appended to the child prompt
	Confinement *permission.ConfinementPolicy // nil = no trajectory confinement
}

// ChildTools returns the default (explore) tool set.
func ChildTools() []tools.Tool {
	return SpecForRole(job.RoleExplore).Tools
}

// readonlyAnalysisTools is the subset of ReadonlyTools() that explore / review
// sub-agents are actually allowed to call (audit J4 fix). The other readonly
// tools (`test`, `document`, `browser`, `browser_navigate`,
// `browser_interact`, `permission`) stay in ReadonlyTools() for the
// parent / web / TUI surfaces but are filtered out for sub-agents because:
//   - `test` runs binaries (ActionBash) — a sub-agent shouldn't trigger
//     test runs that can mutate coverage state or hit the network
//   - `document` writes a file (ActionWrite)
//   - `browser` / `browser_navigate` / `browser_interact` drive a browser
//     with side effects outside the local filesystem
//   - `permission` is the meta-tool that flips the live permission mode;
//     admitting it would let a readonly sub-agent disable readonly mode
//     (privilege escalation)
//
// `code` (mode=build/coverage/property/migration/scaffold) is denied at the
// permission-gate level (ActionBash/ActionWrite under ModeReadonly) even
// though the tool name is admitted here — so the LLM can call code
// mode=deadcode but not code mode=build. Same for `context` mode=snapshot.
//
// `git` follows the same shape: status/diff/log/show/branch-list are
// ActionRead; add/restore/branch-create are ActionWrite and denied by the
// readonly fold; commit/push/pull/pr-create are ActionBash — under
// ChildPolicy that is the same exposure the child's `bash` tool already has
// (non-denied bash allowed), so admitting the name adds the governed
// interface without widening capability.
var readonlyAnalysisToolNames = []string{
	"read", "search", "bash", // core recon
	"git",           // read modes at the gate; mutations behave like bash
	"code",          // analysis modes only — build/scaffold/migration denied at gate
	"context",       // fold/journal/batch/config only — snapshot denied at gate
	"docsync",       // doc drift check
	"vector_search", // semantic search
	"secret",        // secret scan
	"fetch",         // network fetch (read-only on workspace)
	"ask_user",      // clarify with the user
	"browser_read",  // read web content (no side effects)
}

// stepsByDepth is the per-generation step-budget table (Bet 04): a direct
// child gets the full confinement budget, grandchildren and deeper get
// progressively cheaper steps — a fleet's leaf work must not cost what its
// trunk work costs. The tables clamp at their last entry.
var stepsByDepth = map[job.Role][]int{
	job.RoleWorker:  {20, 14, 10},
	job.RoleReview:  {15, 10, 7},
	job.RoleExplore: {10, 7, 5},
}

func stepsForDepth(role job.Role, depth int) int {
	tbl := stepsByDepth[role]
	if len(tbl) == 0 {
		return 0
	}
	if depth < 0 {
		depth = 0
	}
	if depth >= len(tbl) {
		depth = len(tbl) - 1
	}
	return tbl[depth]
}

// SpecForRole returns tools, permission mode, and closing hint for a role
// at depth 0 (a direct child of the spawning engine).
func SpecForRole(role job.Role) ChildSpec {
	return SpecForRoleAt(role, 0)
}

// SpecForRoleAt is [SpecForRole] with per-depth step budgets: depth 0 is a
// direct child (full budget); each deeper generation is cheaper. The
// returned Confinement is always a fresh value — callers may tune it.
func SpecForRoleAt(role job.Role, depth int) ChildSpec {
	role = job.NormalizeRole(string(role))
	switch role {
	case job.RoleWorker:
		return ChildSpec{
			Role:        job.RoleWorker,
			Tools:       tools.DefaultTools(), // no agent_* unless the runner nests; writable
			Mode:        permission.ModeHeadlessStrict,
			Hint:        workerSummaryHint,
			Confinement: &permission.ConfinementPolicy{MaxSteps: stepsForDepth(job.RoleWorker, depth)},
		}
	case job.RoleReview:
		return ChildSpec{
			Role:  job.RoleReview,
			Tools: filterReadonlyAnalysis(tools.ReadonlyTools()),
			Mode:  permission.ModeReadonly,
			Hint:  reviewSummaryHint,
			Confinement: &permission.ConfinementPolicy{
				MaxSteps:     stepsForDepth(job.RoleReview, depth),
				AllowedTools: append([]string(nil), readonlyAnalysisToolNames...),
			},
		}
	default:
		return ChildSpec{
			Role:  job.RoleExplore,
			Tools: filterReadonlyAnalysis(tools.ReadonlyTools()),
			Mode:  permission.ModeReadonly,
			Hint:  exploreSummaryHint,
			Confinement: &permission.ConfinementPolicy{
				MaxSteps:     stepsForDepth(job.RoleExplore, depth),
				AllowedTools: append([]string(nil), readonlyAnalysisToolNames...),
			},
		}
	}
}

// filterReadonlyAnalysis returns only the tools whose names appear in
// readonlyAnalysisToolNames. Used so the LLM's tool schema for a sub-agent
// matches the trajectory confinement allowlist — the LLM never sees a tool
// it can't actually call (audit J4: pre-fix, the LLM saw all 17 readonly
// tools but only 3 were admitted, so calls to e.g. `vector_search` got
// hard-denied at the confinement gate with no schema-level signal).
func filterReadonlyAnalysis(in []tools.Tool) []tools.Tool {
	allow := make(map[string]struct{}, len(readonlyAnalysisToolNames))
	for _, n := range readonlyAnalysisToolNames {
		allow[n] = struct{}{}
	}
	out := make([]tools.Tool, 0, len(in))
	for _, t := range in {
		if _, ok := allow[t.Definition.Name]; ok {
			out = append(out, t)
		}
	}
	return out
}

const exploreSummaryHint = `You are an explore sub-agent (no file edits). Recon the codebase and return dense findings the parent can act on without re-reading everything.

Notes:
1. Be direct. Prefer cwd-relative paths. Use bash freely for inspection (git, builds, tests, pipelines) — hard-denied destructive commands still fail.
2. Structure the final reply roughly as: Files (paths + why), Key findings (types/APIs/snippets only when useful), Start here (what the parent should do next).
3. The parent sees only this final reply — not your tool transcript.
4. You cannot modify files; if edits are needed, say what should change and where.`

const reviewSummaryHint = `You are a review sub-agent (no file edits). Inspect diffs, run checks, and report findings — do not implement fixes.

Notes:
1. Prefer cwd-relative paths. Cite evidence (commands, failing tests, hunks). Prefer git diff/log/show and targeted tests, but bash is available beyond that.
2. Separate must-fix issues from nits. Do not edit files; recommend concrete changes for the parent.
3. Structure roughly as: Files reviewed, Critical, Warnings, Suggestions, Summary.
4. The parent sees only this final reply — not your tool transcript.`

const workerSummaryHint = `You are a worker sub-agent. Implement the assigned scoped task, verify with bash/tests when useful, then finish with one concise final reply.

Notes:
1. Stay within the assigned scope; do not expand into unrelated refactors.
2. Prefer cwd-relative paths. Summarize what you changed and how you verified (Completed / Files changed / Verification).
3. The parent sees only this final reply — not your tool transcript.
4. You may spawn your own sub-agents (worker role only) for scoped pieces; they get cheaper step budgets and inherit this task's declared contract checks. Do not fan out reflexively — nest only when a piece is genuinely parallel.`
