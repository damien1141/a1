package agent

import (
        "strconv"
        "strings"

        "github.com/damien1141/a1/internal/evidence"
)

// Caps that keep the evidence record bounded. A large workspace can observe
// hundreds of distinct check ids (every package, every test); the record is
// context the model consumes, so it lists the load-bearing head of each
// section and summarizes the rest as a count.
const (
        evidenceRecordMaxChecks = 30
        evidenceRecordMaxDetail = 160
)

// renderEvidenceRecord renders the harness evidence record that compaction
// appends to every summary (Phase 4: evidence-driven recurrence). The record
// is what survives the fold: after compaction the tool outputs that produced
// the evidence are gone, so the verified state itself — protected checks,
// current regressions, the turn contract's standing — becomes the next
// recurrence round's context instead of a transcript-shaped memory of it.
//
// It is rendered by the harness from ledger + contract state, never by the
// summarizer: a model-written copy could drop or distort the failure set the
// zero-rank stop and the verdict gate depend on.
//
// Chat-inert by construction: a session that never ran a recognized check
// and never declared a contract (plain chat) renders "", and compaction
// appends nothing — its summaries are byte-identical to pre-evidence output.
func (engine *Engine) renderEvidenceRecord() string {
        ledger := engine.evidence
        contract := engine.turnContract.snapshot()

        state := ledger.State()
        if len(state) == 0 && len(contract) == 0 {
                return ""
        }
        regressions := ledger.Regressions()
        regressed := make(map[string]bool, len(regressions))
        for _, r := range regressions {
                regressed[r.Check] = true
        }

        var passing []string
        var neverPassed []evidence.Record
        for _, rec := range state {
                switch {
                case rec.Status == evidence.Pass:
                        passing = append(passing, rec.Check)
                case !regressed[rec.Check]:
                        neverPassed = append(neverPassed, rec)
                }
        }

        var b strings.Builder
        b.WriteString("**Verified evidence — harness record.** ")
        b.WriteString("Written by the harness from the evidence ledger, re-appended fresh at every compaction; treat it as authoritative and do not restate it from memory.")

        b.WriteString("\n\nProtected checks (passed at least once): ")
        b.WriteString(strconv.Itoa(ledger.ProtectedCount()))

        if len(passing) > 0 {
                b.WriteString("\nPassing now: ")
                writeCappedIDs(&b, passing)
        }
        if len(regressions) > 0 {
                b.WriteString("\nProtected regressions (were passing, now failing — fix these before anything else; do not delete or weaken them to go green):")
                writeCappedRecords(&b, regressions)
        }
        if len(neverPassed) > 0 {
                b.WriteString("\nFailing, never passed (no prior green — known-broken or work-in-progress):")
                writeCappedRecords(&b, neverPassed)
        }

        if len(contract) > 0 {
                b.WriteString("\nDeclared done-criteria (this turn's contract, judged against the ledger):")
                statuses := ledger.ContractStatus(contract)
                for i, st := range statuses {
                        if i == evidenceRecordMaxChecks {
                                b.WriteString("\n- … (+")
                                b.WriteString(strconv.Itoa(len(statuses) - i))
                                b.WriteString(" more)")
                                break
                        }
                        b.WriteString("\n- ")
                        b.WriteString(st.Check)
                        b.WriteString(": ")
                        switch st.State {
                        case evidence.CheckPassing:
                                b.WriteString("PASSING")
                        case evidence.CheckFailing:
                                b.WriteString("FAILING — ")
                                b.WriteString(st.Detail)
                        default:
                                b.WriteString("NO EVIDENCE — never observed passing; run it")
                        }
                }
                if residual := ledger.Residual(contract); len(residual) > 0 {
                        b.WriteString("\nResidual: ")
                        b.WriteString(strconv.Itoa(len(residual)))
                        b.WriteString(" declared check(s) not green — the turn cannot be certified complete until they pass.")
                } else {
                        b.WriteString("\nAll declared checks green — contract satisfied.")
                }
        }

        return b.String()
}

// writeCappedIDs writes a comma-joined id list, capped, with a "+N more"
// tail when truncated.
func writeCappedIDs(b *strings.Builder, ids []string) {
        limit := len(ids)
        if limit > evidenceRecordMaxChecks {
                limit = evidenceRecordMaxChecks
        }
        b.WriteString(strings.Join(ids[:limit], ", "))
        if limit < len(ids) {
                b.WriteString(" (+")
                b.WriteString(strconv.Itoa(len(ids)-limit))
                b.WriteString(" more)")
        }
}

// writeCappedRecords writes record lines with their failure detail, capped,
// with a "+N more" tail when truncated.
func writeCappedRecords(b *strings.Builder, recs []evidence.Record) {
        limit := len(recs)
        if limit > evidenceRecordMaxChecks {
                limit = evidenceRecordMaxChecks
        }
        for _, rec := range recs[:limit] {
                b.WriteString("\n- ")
                b.WriteString(rec.Check)
                if detail := strings.TrimSpace(rec.Detail); detail != "" {
                        b.WriteString(" — ")
                        b.WriteString(truncateDetail(detail))
                }
        }
        if limit < len(recs) {
                b.WriteString("\n- … (+")
                b.WriteString(strconv.Itoa(len(recs)-limit))
                b.WriteString(" more)")
        }
}

// truncateDetail clamps a failure detail line defensively; classify already
// truncates, but records can also arrive from replayed receipts.
func truncateDetail(detail string) string {
        if len(detail) <= evidenceRecordMaxDetail {
                return detail
        }
        return detail[:evidenceRecordMaxDetail] + "…"
}
