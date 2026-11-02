package evidence

import (
	"regexp"
	"strings"
	"time"
)

// JEV-admitted evidence: records sourced from the decision model rather
// than a deterministic parser (see internal/agent/jev_evidence.go for the
// advisor). This file owns the ledger-side surface: the stable check id
// grammar and the commit path. The advisor guarantees the discipline —
// confidence floor, no-record-on-doubt — before anything reaches here.

// jevSlugRe collapses everything outside the conservative id alphabet into
// single dashes.
var jevSlugRe = regexp.MustCompile(`[^A-Za-z0-9_.:/#-]+`)

// jevCheckIDCap bounds the slug so a pathological command cannot forge an
// unbounded id.
const jevCheckIDCap = 100

// JevCheckID derives the stable check id for a JEV-admitted command:
// "jev:" + a whitespace-free slug of the command. The slug keeps the id
// inside the ValidCheckID grammar (so a contract can declare it as a
// done-criterion) while staying deterministic across runs of the same
// command, so the ratchet matches repeated observations. Two commands that
// slug identically share one check — acceptable for the long tail the
// advisor serves; the deterministic classifiers handle the precise cases.
func JevCheckID(command string) string {
	slug := jevSlugRe.ReplaceAllString(strings.TrimSpace(command), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > jevCheckIDCap {
		slug = slug[:jevCheckIDCap]
	}
	if slug == "" {
		slug = "unknown"
	}
	return "jev:" + slug
}

// ObserveJev commits a decision-model verdict as evidence for a command no
// deterministic classifier recognized. detail carries the advisor-rendered
// provenance (first error line + confidence for failures; "" for passes,
// matching the Record convention). The record ratchets like any other: a
// JEV pass protects, and a later fail — JEV or deterministic — regresses.
// The caller (the advisor) is responsible for the admission bar; the
// ledger treats what arrives as observed fact.
func (l *Ledger) ObserveJev(command string, status Status, detail string, at time.Time) Verdict {
	if l == nil {
		return Verdict{}
	}
	rec := Record{
		Check:   JevCheckID(command),
		Status:  status,
		At:      at,
		Detail:  detail,
		Command: command,
	}
	return l.commitRecords(command, []Record{rec}, at)
}
