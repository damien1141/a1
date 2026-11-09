// Package evidence implements a1's protected evidence state: a
// harness-owned ledger of deterministic check results (build, vet, tests)
// that only the harness can write.
//
// The model runs commands; the harness observes their results and upserts
// the ledger. Any check that has ever passed becomes PROTECTED — a later
// run of the same check that fails is a protected regression and surfaces
// as an advisory system note on the tool result. This is the ratchet from
// the VeriLoop-Governed Recurrence contract: the set of verified facts can
// only grow, so workspace state can only move forward through verified
// space, and a model can never self-certify progress that the deterministic
// record contradicts.
//
// The ledger is deliberately conservative: only well-understood commands
// (go build/vet/test, pytest, cargo test) produce records, ambiguous
// compound-command failures record nothing, and canceled or timed-out runs
// are ignored — incomplete output must never poison the evidence state.
package evidence

import (
        "encoding/json"
        "os"
        "path/filepath"
        "sort"
        "strings"
        "sync"
        "time"

	"github.com/damien1141/a1/internal/vault"
)

// Status is the observed outcome of a check.
type Status string

const (
        Pass Status = "pass"
        Fail Status = "fail"
)

// Record is one observation of one check.
type Record struct {
        // Check is the stable check id, e.g. "go-build", "go-vet",
        // "go-test:github.com/x/y", "go-test#TestName", "pytest:tests/a.py::test_b",
        // "cargo-test:name", "cargo-build".
        Check string `json:"check"`
        // Status is the observed outcome.
        Status Status `json:"status"`
        // At is when the observation was recorded.
        At time.Time `json:"at"`
        // Detail carries the first error line for failures (truncated); empty
        // for passes.
        Detail string `json:"detail,omitempty"`
        // Command is the classified command that produced the record.
        Command string `json:"command,omitempty"`
}

// Verdict summarizes what one Observe call did to the ledger. It is the
// per-observation slice of the VeriLoop commit rule: Regressed blocks a
// candidate, Improved is the strict-improvement signal, Neutral changed
// nothing.
type Verdict struct {
        // Regressed holds protected checks that flipped pass → fail.
        Regressed []Record
        // Improved holds checks that flipped fail → pass or passed for the
        // first time (new protected evidence).
        Improved []Record
        // Neutral counts observations that did not change state.
        Neutral int
}

// Regression reports whether the observation regressed anything protected.
func (v Verdict) Regression() bool { return len(v.Regressed) > 0 }

// Any reports whether the observation produced any record at all. A Verdict
// with no regressions, no improvements, and zero neutral observations means
// the command was not recognized as a check run — the verdict gate uses that
// to distinguish "checks ran and changed nothing" (a verification round)
// from "no checks ran" (not a candidate).
func (v Verdict) Any() bool {
        return len(v.Regressed) > 0 || len(v.Improved) > 0 || v.Neutral > 0
}

// LastPassAt reports when the most recent passing check passed. The
// verify-on-stop gate judges freshness against it: a check that passed
// before the turn's last edit describes the code as it was, not as it
// stands. Zero when nothing has ever passed.
func (l *Ledger) LastPassAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	var latest time.Time
	for _, rec := range l.lastPass {
		if rec.At.After(latest) {
			latest = rec.At
		}
	}
	return latest
}

// Ledger is the protected evidence state. Zero value is not usable; use
// NewLedger. All methods are safe for concurrent use — the executor may
// fold parallel tool results.
type Ledger struct {
        mu        sync.Mutex
        state     map[string]Record
        protected map[string]bool
        // lastPass holds the most recent Pass record per check. state only
        // keeps the last record overall, but pruning needs the last pass to
        // rebuild the protected set on replay (a check whose last record is a
        // Fail still needs its pass receipt in the file, or the replayed
        // ratchet forgets the check was ever protected).
        lastPass map[string]Record
        // watches are contract-declared expectations (exit:/http:/port:)
        // awaiting observations; nil until the first Watch call.
        watches map[string]watch
        // checks are config-defined checks (config.yaml `checks:`).
        checks []compiledCheck
        // path is the receipt identity: the JSONL file path (file backing)
        // or the synthetic path the vault era reports for the same scope —
        // screenshot baselines derive their directory from it either way.
        path string
        // receipts persists receipt lines; nil = memory only. The file
        // backing (jsonlReceipts) and the vault backing (vaultReceipts, Bet 08)
        // share it — the ratchet, pruning, and replay logic are identical.
        receipts receiptStore
        // pruneEvery caps receipt growth: after that many appends the store is
        // rewritten with the minimal replay-equivalent record set. 0 = never
        // prune (tests only; production always prunes).
        pruneEvery int
        // appends counts receipts written since the last prune.
        appends int
}

// receiptStore is the persistence seam under a ledger: append one
// receipt line, load all lines in order, or rewrite the whole set (the
// prune path). Implementations: jsonlReceipts (the original file),
// vaultReceipts (rows in the one durable store).
type receiptStore interface {
        Append(line []byte) error
        LoadAll() ([][]byte, error)
        ReplaceAll(lines [][]byte) error
}

// defaultPruneEvery is the receipt-rewrite interval. Receipts are one JSON
// line each; 512 keeps both the file and the replay bounded while amortizing
// the rewrite cost across hundreds of observations.
const defaultPruneEvery = 512

// NewLedger creates a ledger. When path is non-empty, every record is also
// appended to that JSONL file and Load can rebuild state from it. A path
// failure never blocks observation: receipts degrade to memory-only.
func NewLedger(path string) *Ledger {
        return newLedgerEvery(path, defaultPruneEvery)
}

// newLedgerEvery is the test constructor: an explicit prune interval (0
// disables pruning) so replay-equivalence can be exercised deterministically
// without writing hundreds of receipts.
func newLedgerEvery(path string, every int) *Ledger {
        if every < 0 {
                every = 0
        }
        l := &Ledger{
                state:      make(map[string]Record),
                protected:  make(map[string]bool),
                lastPass:   make(map[string]Record),
                path:       path,
                pruneEvery: every,
        }
        if path != "" {
                l.receipts = jsonlReceipts{path: path}
        }
        return l
}

// withReceipts attaches a receipt store to a ledger (vault backing).
func (l *Ledger) withReceipts(store receiptStore) *Ledger {
        if l == nil {
                return l
        }
        l.receipts = store
        return l
}

// NewVaultLedger creates a vault-backed ledger for one project scope
// (Bet 08): receipts are rows in the one durable store instead of a
// JSONL file beside the sessions. The reported path is the synthetic
// path the file era used for the same scope, so screenshot baselines
// (which derive their directory from Path()) keep landing where they
// always did. Nil vault = memory-only ledger.
func NewVaultLedger(scope string, v *vault.Vault) *Ledger {
        l := newLedgerEvery(filepath.Join(scope, "evidence.jsonl"), defaultPruneEvery)
        if v == nil {
                return l
        }
        return l.withReceipts(vaultReceipts{v: v, scope: scope})
}

// Path returns the receipt file backing this ledger ("" = memory only).
func (l *Ledger) Path() string {
        if l == nil {
                return ""
        }
        return l.path
}

// Load replays the receipt store into state. Corrupt lines are skipped;
// a missing store is not an error (first run).
func (l *Ledger) Load() error {
        if l == nil || l.receipts == nil {
                return nil
        }
        lines, err := l.receipts.LoadAll()
        if err != nil {
                // A missing file is a first run, not an error.
                if os.IsNotExist(err) {
                        return nil
                }
                return err
        }

        l.mu.Lock()
        defer l.mu.Unlock()
        applied := 0
        for _, raw := range lines {
                line := strings.TrimSpace(string(raw))
                if line == "" {
                        continue
                }
                var rec Record
                if err := json.Unmarshal([]byte(line), &rec); err != nil {
                        continue
                }
                if rec.Check == "" || (rec.Status != Pass && rec.Status != Fail) {
                        continue
                }
                l.apply(rec)
                applied++
        }
        // Self-heal: a session resumed after many verification rounds replays a
        // receipt set that has grown past the prune interval. Compact it here so
        // the next resume replays the minimal set, not the whole history.
        if l.pruneEvery > 0 && applied >= l.pruneEvery {
                l.pruneLocked()
        }
        return nil
}

// Observe classifies a completed command against its combined output and
// records every check it can determine. failed reports whether the command
// exited non-zero; callers must not invoke Observe for canceled or
// timed-out runs (incomplete output). The returned Verdict reports
// protected regressions and strict improvements. The process exit code is
// unknown on this path — use ObserveExit when it is available.
func (l *Ledger) Observe(command, output string, failed bool, at time.Time) Verdict {
        return l.ObserveExit(command, output, -1, failed, at)
}

// commitRecords folds classified records into state under one lock and
// returns the verdict. Shared by every observe entry point.
func (l *Ledger) commitRecords(command string, records []Record, at time.Time) Verdict {
        l.mu.Lock()
        defer l.mu.Unlock()
        var v Verdict
        for _, rec := range records {
                rec.At = at
                rec.Command = strings.TrimSpace(command)
                verdict := l.apply(rec)
                switch verdict {
                case verdictRegressed:
                        v.Regressed = append(v.Regressed, l.state[rec.Check])
                case verdictImproved:
                        v.Improved = append(v.Improved, l.state[rec.Check])
                default:
                        v.Neutral++
                }
                l.appendReceiptLocked(rec)
        }
        return v
}

// RevertTo restores the last-known state of the named checks to their
// pre-candidate records. It is the evidence-state counterpart of the
// workspace rollback: when the harness rejects a candidate (protected
// regression) and restores the working tree to the last verified snapshot,
// the ledger must not keep claiming those checks are failing in a workspace
// that was just restored to a state where they passed — otherwise the next
// run of the same broken edit looks "neutral" (fail → fail) instead of a
// fresh regression, and the ratchet goes blind.
//
// Semantics:
//   - only checks whose current status is Fail are reverted, and only when
//     the supplied pre-record is a Pass — reverting is strictly a
//     regression-undo, never a way to flip arbitrary state;
//   - the protected set never shrinks (the ratchet is monotone);
//   - each reversion appends a receipt with Command "harness:<cause>" so a
//     session resume replays the reversion and stays coherent. The model
//     can never produce such a receipt: receipts only come from harness
//     classification of bash output, which never reports a "harness:" command.
func (l *Ledger) RevertTo(pre []Record, checks []string, cause string) {
        if l == nil || len(checks) == 0 {
                return
        }
        wanted := make(map[string]Record, len(checks))
        for _, name := range checks {
                for _, rec := range pre {
                        if rec.Check == name && rec.Status == Pass {
                                wanted[name] = rec
                                break
                        }
                }
        }
        if len(wanted) == 0 {
                return
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        now := time.Now()
        for name := range wanted {
                if cur, ok := l.state[name]; !ok || cur.Status != Fail {
                        // Already passing (or never observed): nothing to revert.
                        // Reverting is strictly a regression-undo.
                        continue
                }
                rev := Record{
                        Check:   name,
                        Status:  Pass,
                        At:      now,
                        Detail:  "",
                        Command: "harness:" + cause,
                }
                l.state[name] = rev
                l.appendReceiptLocked(rev)
        }
}

// Regressions returns the checks currently failing that were protected
// (have passed before). Empty when the workspace is clean.
func (l *Ledger) Regressions() []Record {
        if l == nil {
                return nil
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        return l.regressionsLocked()
}

// State returns the last-known record for every observed check, sorted by
// check id.
func (l *Ledger) State() []Record {
        if l == nil {
                return nil
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        out := make([]Record, 0, len(l.state))
        for _, rec := range l.state {
                out = append(out, rec)
        }
        sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
        return out
}

// ProtectedCount reports how many distinct checks have ever passed.
func (l *Ledger) ProtectedCount() int {
        if l == nil {
                return 0
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        return len(l.protected)
}

// Summary renders a one-line human-readable ledger state for context
// injection and UI display.
func (l *Ledger) Summary() string {
        if l == nil {
                return "evidence: none"
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        protected := len(l.protected)
        failing := len(l.regressionsLocked())
        if protected == 0 && failing == 0 {
                return "evidence: no checks observed yet"
        }
        s := "evidence: " + itoa(protected) + " protected"
        if failing > 0 {
                s += ", " + itoa(failing) + " FAILING"
        }
        return s
}

type applyVerdict int

const (
        verdictNeutral applyVerdict = iota
        verdictRegressed
        verdictImproved
)

// apply folds one record into state. Caller must hold l.mu.
func (l *Ledger) apply(rec Record) applyVerdict {
        prev, existed := l.state[rec.Check]
        l.state[rec.Check] = rec

        if rec.Status == Pass {
                l.lastPass[rec.Check] = rec
                wasProtected := l.protected[rec.Check]
                l.protected[rec.Check] = true
                switch {
                case existed && prev.Status == Fail:
                        return verdictImproved
                case !wasProtected:
                        // First pass: new protected evidence.
                        return verdictImproved
                default:
                        return verdictNeutral
                }
        }
        // rec.Status == Fail
        if existed && prev.Status == Pass {
                return verdictRegressed // protected by definition: pass ⇒ protected
        }
        return verdictNeutral
}

func (l *Ledger) regressionsLocked() []Record {
        var out []Record
        for check, rec := range l.state {
                if rec.Status == Fail && l.protected[check] {
                        out = append(out, rec)
                }
        }
        sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
        return out
}

// appendReceiptLocked persists one record as a JSONL line. Best-effort:
// receipt failures never fail the observation. Once pruneEvery receipts
// have accumulated, the file is rewritten with the minimal
// replay-equivalent set so evidence.jsonl stays bounded across a long
// session (Phase 4: receipt pruning).
func (l *Ledger) appendReceiptLocked(rec Record) {
        if l.receipts == nil {
                return
        }
        line, err := json.Marshal(rec)
        if err != nil {
                return
        }
        if err := l.receipts.Append(line); err != nil {
                return
        }
        l.appends++
        if l.pruneEvery > 0 && l.appends >= l.pruneEvery {
                l.pruneLocked()
        }
}

// pruneLocked rewrites the receipt store with the minimal replay-equivalent
// record set: for each check, its last Pass record (when one exists)
// followed by its last record. Replaying that set reproduces state,
// protected, and lastPass exactly — the ratchet survives pruning, including
// for checks currently failing (their pass receipt is what keeps them
// protected on replay). On failure the old set stands and the counter
// resets, so the next interval retries. Caller must hold l.mu.
func (l *Ledger) pruneLocked() {
        l.appends = 0
        if l.receipts == nil {
                return
        }
        checks := make([]string, 0, len(l.state))
        for check := range l.state {
                checks = append(checks, check)
        }
        sort.Strings(checks)

        var lines [][]byte
        for _, check := range checks {
                cur := l.state[check]
                if cur.Status == Pass {
                        // The current record is itself the last pass — one line covers
                        // both state and protection on replay.
                        lines = append(lines, receiptLine(cur))
                        continue
                }
                if lp, ok := l.lastPass[check]; ok {
                        lines = append(lines, receiptLine(lp))
                }
                lines = append(lines, receiptLine(cur))
        }
        _ = l.receipts.ReplaceAll(lines)
}

func receiptLine(rec Record) []byte {
        line, err := json.Marshal(rec)
        if err != nil {
                return nil
        }
        return line
}

// RegressionNote renders the advisory system note appended to a bash tool
// result when protected checks regressed. The closing line restates the
// harness rule that survives any prompt: broken checks get fixed, not
// deleted or weakened.
func RegressionNote(regressed []Record) string {
        var b strings.Builder
        b.WriteString("[evidence] PROTECTED REGRESSION — previously passing check(s) now failing:")
        for _, r := range regressed {
                detail := strings.TrimSpace(r.Detail)
                if len(detail) > 160 {
                        detail = detail[:160] + "…"
                }
                b.WriteString("\n- " + r.Check)
                if detail != "" {
                        b.WriteString(" — " + detail)
                }
        }
        b.WriteString("\nFix the regression before reporting completion. Do not delete, skip, or weaken the failing checks to go green.")
        return b.String()
}

func itoa(n int) string {
        if n == 0 {
                return "0"
        }
        neg := n < 0
        if neg {
                n = -n
        }
        var buf [20]byte
        i := len(buf)
        for n > 0 {
                i--
                buf[i] = byte('0' + n%10)
                n /= 10
        }
        if neg {
                i--
                buf[i] = '-'
        }
        return string(buf[i:])
}
