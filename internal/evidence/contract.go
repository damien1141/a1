package evidence

import (
        "sort"
        "strings"
)

// Check-id grammar for contracts (Phase 3, zero-rank stop). A contract is a
// set of check ids the harness can judge against ledger state:
//
//      go-build, go-vet, cargo-build                  exact harness checks
//      js-test, playwright-test, mypy, ruff, eslint,
//      cargo-clippy, dotnet-test                      polyglot suite checks
//      go-test:<pkg>                                  package-level check
//      go-test#<TestName>                             per-test check (verbose runs)
//      go-test:<prefix>/...  /  go-test:...           package patterns
//      pytest:<nodeid>, cargo-test:<name>              exact foreign checks
//      exit:<n>:<command>                             command exits n (watched)
//      http:<code>:<url>                              url answers code (curl probe)
//      port:<host>:<port>                             tcp port open (nc -z probe)
//      screenshot:<name>                              named capture matches baseline
//      check:<name>                                   config-defined check
//
// Patterns are go-test only: go package paths form a tree, so a prefix with
// "/..." (or the bare "go-test:..." form for the whole module) denotes
// "every observed package check under this prefix". A pattern is satisfied
// only when at least one ledger check matches it and every match passes —
// zero matches is NO EVIDENCE, never a vacuous pass ("existing but
// unchecked = not done").
//
// Watched forms (exit:/http:/port:/screenshot:) are observation-gated: the
// id only records evidence after the contract declares it, so an incidental
// bash call or browser capture never grows the ledger or arms regression
// notes. The model declares the expectation, then runs the command (or the
// curl/nc probe, or the named screenshot); the harness records the
// observation against the declared expectation.
//
// All contract judgment is deterministic and read-only: it consumes the same
// last-known state the ratchet admits, so a contract check that passed and
// later regressed reads as failing. Contracts cannot fabricate freshness —
// the model still has to run the checks; the harness only refuses to
// certify "done" on stale or absent evidence.

// CheckState is a contract check's standing against current ledger state.
type CheckState string

const (
        // CheckPassing: last-known evidence for the check is a pass (and for
        // patterns: every matching check passes, at least one match).
        CheckPassing CheckState = "passing"
        // CheckFailing: the check has evidence and its last-known status is a
        // fail (patterns: at least one matching check fails).
        CheckFailing CheckState = "failing"
        // CheckNoEvidence: the ledger holds nothing for the check (patterns:
        // nothing matches).
        CheckNoEvidence CheckState = "no-evidence"
)

// CheckStatus is one contract check's judgment.
type CheckStatus struct {
        // Check is the declared check id (exact id or pattern).
        Check string
        // State is the judgment.
        State CheckState
        // Detail names the failing check (patterns may cover several) and its
        // first error line when State is CheckFailing.
        Detail string
        // Matches counts ledger checks the id matched (exact: 0 or 1; patterns:
        // any). A pattern with Matches == 0 is CheckNoEvidence.
        Matches int
}

// ValidCheckID reports whether id is a well-formed check id or pattern the
// ledger knows how to judge. The id is judged AS GIVEN — no trimming — so
// malformed ids (trailing spaces, blanks) are refused at declaration time
// and a contract can never hold a check nothing can ever satisfy or
// evidence. Callers that accept free-form model input trim before storing.
func ValidCheckID(id string) bool {
        switch {
        case id == "go-build" || id == "go-vet" || id == "cargo-build":
                return true
        case id == "js-test" || id == "playwright-test" || id == "mypy" ||
                id == "ruff" || id == "eslint" || id == "cargo-clippy" || id == "dotnet-test":
                return true
        case strings.HasPrefix(id, "go-test:"):
                rest := strings.TrimPrefix(id, "go-test:")
                // Exact package ids and the two pattern forms ("go-test:...",
                // "go-test:<prefix>/...") are all non-empty and whitespace-free.
                return isPlainGoTestPkg(rest)
        case strings.HasPrefix(id, "go-test#"):
                name := strings.TrimPrefix(id, "go-test#")
                return name != "" && !strings.ContainsAny(name, " \t")
        case strings.HasPrefix(id, "pytest:"):
                // pytest nodeids may contain spaces (parametrize ids); require
                // non-empty only.
                return strings.TrimPrefix(id, "pytest:") != ""
        case strings.HasPrefix(id, "cargo-test:"):
                name := strings.TrimPrefix(id, "cargo-test:")
                return name != "" && !strings.ContainsAny(name, " \t")
        case strings.HasPrefix(id, "exit:"):
                _, ok := parseWatch(id)
                return ok
        case strings.HasPrefix(id, "http:"):
                _, ok := parseWatch(id)
                return ok
        case strings.HasPrefix(id, "port:"):
                _, ok := parseWatch(id)
                return ok
        case strings.HasPrefix(id, "screenshot:"):
                _, ok := parseWatch(id)
                return ok
        case strings.HasPrefix(id, "check:"):
                name := strings.TrimPrefix(id, "check:")
                return name != "" && !strings.ContainsAny(name, " \t")
        case strings.HasPrefix(id, "jev:"):
                // JEV-admitted evidence (see jev.go): the rest is a command slug,
                // whitespace-free by construction.
                rest := strings.TrimPrefix(id, "jev:")
                return rest != "" && !strings.ContainsAny(rest, " \t")
        }
        return false
}

// isPlainGoTestPkg accepts any non-empty, whitespace-free rest — package
// import paths are not validated against the module (the ledger only ever
// compares against ids it observed itself).
func isPlainGoTestPkg(rest string) bool {
        return rest != "" && !strings.ContainsAny(rest, " \t")
}

// isPattern reports whether the id is a go-test package pattern.
func isPattern(id string) bool {
        if !strings.HasPrefix(id, "go-test:") {
                return false
        }
        rest := strings.TrimPrefix(id, "go-test:")
        return rest == "..." || strings.HasSuffix(rest, "/...")
}

// matchCheck reports whether ledger check `check` satisfies contract id
// `want`. Exact ids match themselves; patterns match package-level go-test
// checks under their prefix.
func matchCheck(want, check string) bool {
        if want == check {
                return true
        }
        if !isPattern(want) || !strings.HasPrefix(check, "go-test:") {
                return false
        }
        prefix := strings.TrimSuffix(strings.TrimPrefix(want, "go-test:"), "...")
        prefix = strings.TrimSuffix(prefix, "/")
        pkg := strings.TrimPrefix(check, "go-test:")
        if prefix == "" {
                return true // "go-test:..." — every observed package
        }
        return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
}

// CheckStatus judges one declared check against current ledger state. The
// id is judged as given (contract state normalizes on declare); an
// ill-formed id reads as no-evidence rather than erroring — it can never
// pass, which is the safe direction for turn-end certification.
func (l *Ledger) CheckStatus(id string) CheckStatus {
        if l == nil || !ValidCheckID(id) {
                return CheckStatus{Check: id, State: CheckNoEvidence}
        }
        l.mu.Lock()
        defer l.mu.Unlock()
        if !isPattern(id) {
                rec, ok := l.state[id]
                if !ok {
                        return CheckStatus{Check: id, State: CheckNoEvidence}
                }
                if rec.Status == Pass {
                        return CheckStatus{Check: id, State: CheckPassing, Matches: 1}
                }
                return CheckStatus{Check: id, State: CheckFailing, Detail: statusDetail(id, rec.Detail), Matches: 1}
        }
        // Pattern: every matching package check must pass, at least one must
        // exist. Sorted for a deterministic first-failure detail.
        status := CheckStatus{Check: id, State: CheckNoEvidence}
        var failing []Record
        for check, rec := range l.state {
                if !matchCheck(id, check) {
                        continue
                }
                status.Matches++
                if rec.Status == Fail {
                        failing = append(failing, rec)
                }
        }
        if status.Matches == 0 {
                return status
        }
        if len(failing) > 0 {
                sort.Slice(failing, func(i, j int) bool { return failing[i].Check < failing[j].Check })
                status.State = CheckFailing
                status.Detail = statusDetail(failing[0].Check, failing[0].Detail)
                return status
        }
        status.State = CheckPassing
        return status
}

// ContractStatus judges every declared check, sorted by check id.
func (l *Ledger) ContractStatus(checks []string) []CheckStatus {
        out := make([]CheckStatus, 0, len(checks))
        seen := make(map[string]bool, len(checks))
        for _, id := range checks {
                id = strings.TrimSpace(id)
                if id == "" || seen[id] {
                        continue
                }
                seen[id] = true
                out = append(out, l.CheckStatus(id))
        }
        sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
        return out
}

// Residual returns the non-passing subset of the contract's checks — the
// failure set that blocks turn-end certification (VGR zero-rank). Empty
// means the contract is satisfied: every declared check has current
// pass evidence.
func (l *Ledger) Residual(checks []string) []CheckStatus {
        var out []CheckStatus
        for _, st := range l.ContractStatus(checks) {
                if st.State != CheckPassing {
                        out = append(out, st)
                }
        }
        return out
}

func statusDetail(check, detail string) string {
        detail = strings.TrimSpace(detail)
        if len(detail) > 160 {
                detail = detail[:160] + "…"
        }
        if detail == "" {
                return check
        }
        return check + " — " + detail
}
