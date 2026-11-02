package evidence

import (
        "regexp"
        "strings"
)

// classify maps a completed shell command + its combined output to the
// deterministic check records it proves. Only well-understood commands
// produce records; everything else returns nil (no evidence either way).
//
// Attribution rules (conservative by design):
//
//   - go test lines are self-attributing (`ok`/`FAIL pkg`), so they are
//     parsed even from compound commands.
//   - build/vet pass results are recorded whenever such a segment ran and
//     the overall command succeeded (a `&&` chain reaching a later stage
//     proves the earlier ones passed).
//   - build/vet FAIL results are only attributed when the failing command
//     has exactly one build/vet segment and no go-test segment — otherwise
//     the failure cannot be attributed safely and nothing is recorded.
//   - a failed cargo test with zero test result lines records cargo-build
//     (the test binary never compiled).
//   - suite-level polyglot checks (js-test, mypy, ruff, eslint, clippy,
//     dotnet-test, playwright-test) follow the build/vet doctrine: passes
//     are credited whenever the segment ran and the command succeeded;
//     failures are attributed only when that suite segment is the single
//     classifiable segment of the command — a failed compound cannot be
//     pinned on one stage, so it records nothing.
func classify(command, output string, failed bool) []Record {
        segs := segments(command)
        var hasGoTest, hasGoBuild, hasGoVet, hasPytest, hasCargoTest, hasSuite bool
        for _, s := range segs {
                switch {
                case goTestRe.MatchString(s):
                        hasGoTest = true
                case goBuildRe.MatchString(s):
                        hasGoBuild = true
                case goVetRe.MatchString(s):
                        hasGoVet = true
                case pytestRe.MatchString(s):
                        hasPytest = true
                case cargoTestRe.MatchString(s):
                        hasCargoTest = true
                }
                if !hasSuite {
                        for _, sc := range suiteChecks {
                                if sc.re.MatchString(s) {
                                        hasSuite = true
                                        break
                                }
                        }
                }
        }

        var records []Record
        if hasGoTest {
                records = append(records, parseGoTest(output)...)
        }
        if hasGoBuild {
                records = append(records, buildVetRecords("go-build", hasGoTest, failed, (hasGoBuild && hasGoVet) || hasSuite, firstGoDiagnostic(output))...)
        }
        if hasGoVet {
                records = append(records, buildVetRecords("go-vet", hasGoTest, failed, (hasGoBuild && hasGoVet) || hasSuite, firstGoDiagnostic(output))...)
        }
        if hasPytest {
                records = append(records, parsePytest(output)...)
        }
        if hasCargoTest {
                records = append(records, parseCargoTest(output, failed)...)
        }
        records = append(records, classifySuites(command, output, failed)...)
        return records
}

// suiteCheck pairs a command-detection regexp with its ledger check id.
type suiteCheck struct {
        re    *regexp.Regexp
        check string
        diag  *regexp.Regexp // first attributable failure line, best-effort detail
}

// suiteChecks are the polyglot suite-level checks. Each entry recognizes
// one tool family; the check id is suite-level (the whole run passed or
// failed), following the go-build/go-vet precedent rather than per-test
// ids — JS test names are prose, not stable identifiers.
var suiteChecks = []suiteCheck{
        {
                // npm/pnpm/yarn/bun test (incl. `run test` and scoped scripts like
                // `test:unit`) and direct vitest/jest (incl. npx/bunx forms).
                re:    regexp.MustCompile(`(^|[ \t])(npm|pnpm|yarn|bun)[ \t]+(run[ \t]+)?test(:[^\s]+)?([ \t]|$)|(^|[ \t])((npx|bunx)[ \t]+)?(vitest|jest)([ \t]|$)`),
                check: "js-test",
                diag:  regexp.MustCompile(`(?m)^(FAIL\b.*|✕.*|×.*|✗.*|not ok\b.*|●.*|Error:.*)`),
        },
        {
                re:    regexp.MustCompile(`(^|[ \t])((npx|bunx)[ \t]+)?playwright[ \t]+test([ \t]|$)`),
                check: "playwright-test",
                diag:  regexp.MustCompile(`(?m)^(✘.*|[ \t]+\d+\)[ \t]*\[.*)`),
        },
        {
                // mypy, directly or via python -m mypy / uv run mypy.
                re:    regexp.MustCompile(`(^|[ \t])((python[0-9.]*[ \t]+-m[ \t]+)|(uv[ \t]+run[ \t]+))?mypy([ \t]|$)`),
                check: "mypy",
                diag:  regexp.MustCompile(`(?m)^\S+\.pyi?:\d+: error: .+`),
        },
        {
                re:    regexp.MustCompile(`(^|[ \t])ruff[ \t]+check([ \t]|$)`),
                check: "ruff",
                diag:  regexp.MustCompile(`(?m)^\S+:\d+:\d+: [A-Z]\d+ .+`),
        },
        {
                re:    regexp.MustCompile(`(^|[ \t])((npx|bunx|(npm|pnpm|yarn)[ \t]+exec)[ \t]+)?eslint([ \t]|$)`),
                check: "eslint",
                diag:  regexp.MustCompile(`(?m)^[ \t]+\d+:\d+[ \t]+error[ \t]+.+`),
        },
        {
                re:    regexp.MustCompile(`(^|[ \t])cargo[ \t]+clippy([ \t]|$)`),
                check: "cargo-clippy",
                diag:  regexp.MustCompile(`(?m)^error(\[E\d+\])?: .+`),
        },
        {
                re:    regexp.MustCompile(`(^|[ \t])dotnet[ \t]+test([ \t]|$)`),
                check: "dotnet-test",
                diag:  regexp.MustCompile(`(?m)^[ \t]*Failed[ \t]+\S+.*$`),
        },
}

// classifySuites applies the suite-level polyglot checks. On success every
// recognized suite segment is credited (the `&&` chain reached the end). On
// failure a suite check is attributed only when its family is the single
// classifiable segment in the command — the same conservatism as build/vet,
// extended to every known check family (go test/pytest/cargo test included,
// since their self-attributing output cannot pin WHICH suite-stage of a
// compound failed).
func classifySuites(command, output string, failed bool) []Record {
        segs := segments(command)
        matched := make(map[string]bool, len(suiteChecks))
        others := 0 // segments from the other known check families (go, pytest, cargo test)
        for _, s := range segs {
                for _, sc := range suiteChecks {
                        if sc.re.MatchString(s) {
                                matched[sc.check] = true
                        }
                }
                if goTestRe.MatchString(s) || goBuildRe.MatchString(s) || goVetRe.MatchString(s) ||
                        pytestRe.MatchString(s) || cargoTestRe.MatchString(s) {
                        others++
                }
        }
        // A failure is attributable only when exactly one check family matched
        // and no other classifiable segment ran: a failed compound cannot be
        // pinned on one stage, so it records nothing. Non-check segments (cd,
        // ls, env setup) do not block attribution — the same tolerance the
        // build/vet rules apply.
        var families []string
        for _, sc := range suiteChecks {
                if matched[sc.check] {
                        families = append(families, sc.check)
                }
        }
        single := len(families) == 1 && others == 0

        var records []Record
        for _, sc := range suiteChecks {
                if !matched[sc.check] {
                        continue
                }
                if !failed {
                        records = append(records, Record{Check: sc.check, Status: Pass})
                        continue
                }
                if !single {
                        // Ambiguous compound failure: skip rather than poison the ledger.
                        continue
                }
                records = append(records, Record{Check: sc.check, Status: Fail, Detail: firstLineMatching(output, sc.diag)})
        }
        return records
}

// buildVetRecords applies the pass/fail attribution rules to one build or
// vet check. ambiguous means the failure cannot be pinned on this check
// alone: build and vet both appear in the same command, or a polyglot
// suite segment ran alongside it.
func buildVetRecords(check string, hasGoTest, failed, compound bool, detail string) []Record {
        if !failed {
                // A passing command proves every segment passed, including this one.
                return []Record{{Check: check, Status: Pass}}
        }
        if hasGoTest || compound {
                // Failure attribution is ambiguous: skip rather than poison the
                // ledger with a possibly-passing check marked failed.
                return nil
        }
        return []Record{{Check: check, Status: Fail, Detail: detail}}
}

// segments splits a shell command into classifiable segments on &&, ||, ;,
// |, and newlines. This is intentionally not a full shell parser: it is
// enough to recognize which tools a command invoked.
func segments(command string) []string {
        split := strings.FieldsFunc(command, func(r rune) bool {
                return r == '\n' || r == ';' || r == '|' || r == '&'
        })
        out := make([]string, 0, len(split))
        for _, s := range split {
                if s = strings.TrimSpace(s); s != "" {
                        out = append(out, s)
                }
        }
        return out
}

var (
        goTestRe  = regexp.MustCompile(`(^|[ \t|;&])go[ \t]+test([ \t]|$)`)
        goBuildRe = regexp.MustCompile(`(^|[ \t|;&])go[ \t]+build([ \t]|$)`)
        goVetRe   = regexp.MustCompile(`(^|[ \t|;&])go[ \t]+vet([ \t]|$)`)
        // pytest may be invoked directly, via python -m pytest, or via uv run
        // pytest; the segment start carries the invocation.
        pytestRe    = regexp.MustCompile(`(^|[ \t|;&])(python[0-9.]*[ \t]+-m[ \t]+)?pytest([ \t]|$)`)
        cargoTestRe = regexp.MustCompile(`(^|[ \t|;&])cargo[ \t]+test([ \t]|$)`)
)

var (
        // goPackageRe matches `go test` package summary lines:
        //   ok         github.com/x/y  0.042s
        //   FAIL       github.com/x/y [build failed]
        //   FAIL       github.com/x/y  0.103s
        // [ \t] not \s: \s crosses lines in multiline mode, so the standalone
        // trailing "FAIL" summary line would swallow the next line's package token.
        goPackageRe = regexp.MustCompile(`(?m)^(ok|FAIL)[ \t]+([^\s]+)`)
        // goTestLineRe matches verbose per-test lines:
        //   --- PASS: TestThing (0.00s)
        //   --- FAIL: TestThing/sub_case (0.01s)
        goTestLineRe = regexp.MustCompile(`(?m)^--- (PASS|FAIL): ([^\s(]+)`)
        // goDiagRe captures a compile/vet diagnostic line for failure details.
        goDiagRe = regexp.MustCompile(`(?m)^.+\.(go):\d+:\d+: .+`)
        // pytestShortRe matches the short test summary section:
        //   FAILED tests/test_a.py::test_thing - AssertionError: ...
        //   PASSED tests/test_a.py::test_other
        pytestShortRe = regexp.MustCompile(`(?m)^(PASSED|FAILED|ERROR)[ \t]+(\S+)(?:[ \t]+-[ \t]+(.+))?`)
        // pytestVerboseRe matches -v progress lines:
        //   tests/test_a.py::test_thing PASSED [ 12%]
        pytestVerboseRe = regexp.MustCompile(`(?m)^(\S+::\S+)[ \t]+(PASSED|FAILED|ERROR)`)
        // cargoTestLineRe matches cargo test result lines:
        //   test unit::tests::thing ... ok
        //   test unit::tests::other ... FAILED
        cargoTestLineRe = regexp.MustCompile(`(?m)^test[ \t]+(\S+)[ \t]+\.\.\.[ \t]+(ok|FAILED|ignored)`)
)

// parseGoTest extracts package-level and (verbose) per-test results.
// Package checks are keyed "go-test:<pkg>"; per-test checks
// "go-test#<TestName>" (go test interleaves packages in verbose output, so
// per-test keys cannot carry the package without -json).
func parseGoTest(output string) []Record {
        var records []Record
        seen := make(map[string]bool)
        add := func(check string, status Status, detail string) {
                if seen[check] {
                        return
                }
                seen[check] = true
                records = append(records, Record{Check: check, Status: status, Detail: detail})
        }

        for _, m := range goPackageRe.FindAllStringSubmatch(output, -1) {
                pkg := m[2]
                if !strings.ContainsAny(pkg, "./") {
                        // Guard against stray matches; real import paths contain a
                        // slash or a dot.
                        continue
                }
                if m[1] == "ok" {
                        add("go-test:"+pkg, Pass, "")
                } else {
                        add("go-test:"+pkg, Fail, "")
                }
        }
        for _, m := range goTestLineRe.FindAllStringSubmatch(output, -1) {
                if m[1] == "PASS" {
                        add("go-test#"+m[2], Pass, "")
                } else {
                        add("go-test#"+m[2], Fail, "")
                }
        }
        return records
}

// isLikelyPackage is retained for tests and future heuristics.
func isLikelyPackage(s string) bool {
        return strings.ContainsAny(s, "./")
}

// parsePytest extracts per-test results from either the short summary
// section or -v progress lines. Checks are keyed "pytest:<nodeid>".
func parsePytest(output string) []Record {
        var records []Record
        seen := make(map[string]bool)
        add := func(nodeid string, status Status, detail string) {
                if seen[nodeid] {
                        return
                }
                seen[nodeid] = true
                records = append(records, Record{Check: "pytest:" + nodeid, Status: status, Detail: detail})
        }
        for _, m := range pytestVerboseRe.FindAllStringSubmatch(output, -1) {
                add(m[1], pytestStatus(m[2]), "")
        }
        for _, m := range pytestShortRe.FindAllStringSubmatch(output, -1) {
                detail := ""
                if m[3] != "" {
                        detail = firstLine(m[3])
                }
                add(m[2], pytestStatus(m[1]), detail)
        }
        return records
}

func pytestStatus(s string) Status {
        if s == "PASSED" {
                return Pass
        }
        return Fail // FAILED and ERROR both block verification
}

// parseCargoTest extracts per-test results keyed "cargo-test:<name>". A
// failed run with zero result lines means the test binary never compiled:
// record cargo-build as failed instead.
func parseCargoTest(output string, failed bool) []Record {
        var records []Record
        seen := make(map[string]bool)
        for _, m := range cargoTestLineRe.FindAllStringSubmatch(output, -1) {
                if m[2] == "ignored" {
                        continue
                }
                status := Pass
                if m[2] == "FAILED" {
                        status = Fail
                }
                if seen[m[1]] {
                        continue
                }
                seen[m[1]] = true
                records = append(records, Record{Check: "cargo-test:" + m[1], Status: status})
        }
        if len(records) == 0 && failed {
                if diag := firstLineMatching(output, cargoCompileErrRe); diag != "" {
                        records = append(records, Record{Check: "cargo-build", Status: Fail, Detail: diag})
                }
        }
        return records
}

var cargoCompileErrRe = regexp.MustCompile(`(?m)^error(\[E\d+\])?: .+`)

// firstGoDiagnostic returns the first `.go:line:col:` diagnostic line,
// truncated for the ledger Detail field.
func firstGoDiagnostic(output string) string {
        m := goDiagRe.FindString(output)
        return truncateDetail(m)
}

func firstLineMatching(output string, re *regexp.Regexp) string {
        return truncateDetail(re.FindString(output))
}

func firstLine(s string) string {
        s = strings.TrimSpace(s)
        if i := strings.IndexByte(s, '\n'); i >= 0 {
                s = s[:i]
        }
        return truncateDetail(s)
}

func truncateDetail(s string) string {
        s = strings.TrimSpace(s)
        const max = 200
        if len(s) > max {
                return s[:max] + "…"
        }
        return s
}
