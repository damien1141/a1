package compaction

import "strings"

// The evidence record is the Phase 4 recurrence carrier: a harness-written
// block appended to every compaction summary that carries the protected
// evidence ledger (protected checks, current failures, the turn contract's
// residual set) across the fold. Verified evidence — not the transcript —
// becomes the next recurrence state: after compaction the model no longer
// sees the tool outputs that produced the evidence, so the record is what
// keeps "what is verified" load-bearing context.
//
// The block is delimited so it can be recognized and handled mechanically:
//
//   - the harness APPENDS it verbatim after the model-written summary (the
//     summarizer never fabricates it — a model-written copy could drift
//     from the deterministic record);
//   - PrepareCompact STRIPS it from the previous summary before feeding it
//     to the update prompt, so stale evidence is never folded forward and
//     the summarizer is never tempted to restate it.
const (
        evidenceRecordOpen  = "<evidence-record>"
        evidenceRecordClose = "</evidence-record>"
)

// wrapEvidenceRecord delimits the harness evidence content. Empty content
// wraps to empty — a session with no observed checks and no declared
// contract (e.g. a plain chat session) gets no record at all, so its
// summaries are byte-identical to pre-evidence compaction output.
func wrapEvidenceRecord(content string) string {
        content = strings.TrimSpace(content)
        if content == "" {
                return ""
        }
        var b strings.Builder
        b.WriteString(evidenceRecordOpen)
        b.WriteString("\n")
        b.WriteString(content)
        b.WriteString("\n")
        b.WriteString(evidenceRecordClose)
        return b.String()
}

// stripEvidenceRecord removes every complete evidence record from a summary,
// together with the "\n\n" separator that joined it to the preceding text
// (Compact always appends records as "\n\n" + record, so a stripped summary
// must not keep a dangling blank-line pair). An unclosed opening marker is
// left untouched (the harness always writes both markers, so an unbalanced
// one is not ours to interpret — cutting to EOF on user content that merely
// mentions the marker would eat real summary text).
func stripEvidenceRecord(s string) string {
        for {
                start := strings.Index(s, evidenceRecordOpen)
                if start < 0 {
                        return s
                }
                end := strings.Index(s[start+len(evidenceRecordOpen):], evidenceRecordClose)
                if end < 0 {
                        return s
                }
                end += start + len(evidenceRecordOpen) + len(evidenceRecordClose)
                cutStart := start
                if cutStart >= 2 && s[cutStart-2:cutStart] == "\n\n" {
                        cutStart -= 2
                }
                s = s[:cutStart] + s[end:]
        }
}
