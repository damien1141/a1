package agent

import (
	"github.com/damien1141/a1/internal/job"
	"github.com/damien1141/a1/internal/session/compaction"
)

// Caps for the structured handoff. The handoff is parent context the model
// consumes, so each section keeps its load-bearing head and drops the tail —
// same philosophy as the evidence record caps.
const (
	handoffMaxFiles  = 40
	handoffMaxChecks = 30
	handoffMaxDetail = 160
)

// buildHandoff assembles the structured result a parent engine receives from
// a finished child (Bet 04): the final summary, the files the child read and
// modified, and the current state of every check the child's ledger observed.
// The prose summary alone caps at 12KB and loses the machine-checkable parts;
// the handoff is what a fleet parent merges as a structured block instead.
func buildHandoff(engine *Engine, summary string) job.Handoff {
	h := job.Handoff{Summary: summary}
	if engine == nil {
		return h
	}

	if engine.session != nil {
		read, modified := compaction.ExtractFileLists(engine.session.PathEntries())
		h.FilesRead = capStringList(read, handoffMaxFiles)
		h.FilesModified = capStringList(modified, handoffMaxFiles)
	}

	if engine.evidence != nil {
		var checks []job.HandoffCheck
		for _, rec := range engine.evidence.State() {
			hc := job.HandoffCheck{
				Check:  rec.Check,
				Status: string(rec.Status),
			}
			if d := rec.Detail; d != "" {
				hc.Detail = truncateHandoffDetail(d)
			}
			checks = append(checks, hc)
		}
		if len(checks) > handoffMaxChecks {
			checks = checks[:handoffMaxChecks]
		}
		h.Checks = checks
	}

	return h
}

// capStringList clamps a list to n entries with a stable "+N more" marker
// dropped (counts stay derivable from the job dir; the parent does not need
// the tail).
func capStringList(in []string, n int) []string {
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func truncateHandoffDetail(detail string) string {
	if len(detail) <= handoffMaxDetail {
		return detail
	}
	return detail[:handoffMaxDetail] + "…"
}

// writeHandoff persists the structured handoff next to result.md. Best
// effort: a failed write leaves the prose summary path (result.md) intact.
func writeHandoff(dir, summary string, engine *Engine) (job.Handoff, error) {
	h := buildHandoff(engine, summary)
	if dir == "" {
		return h, nil
	}
	if err := job.WriteHandoff(dir, h); err != nil {
		return h, err
	}
	return h, nil
}
