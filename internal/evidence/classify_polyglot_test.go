package evidence

import (
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// ---------- polyglot suite classification ----------

func TestClassifyJsTestNpmPass(t *testing.T) {
        records := classify("npm test", "", false)
        require.Len(t, records, 1)
        assert.Equal(t, "js-test", records[0].Check)
        assert.Equal(t, Pass, records[0].Status)
}

func TestClassifyJsTestYarnScopedScript(t *testing.T) {
        assert.Equal(t, Pass, classify("yarn run test:unit", "", false)[0].Status)
        assert.Equal(t, Pass, classify("pnpm test", "", false)[0].Status)
        assert.Equal(t, Pass, classify("bun test", "", false)[0].Status)
}

func TestClassifyJsTestVitestDirectFail(t *testing.T) {
        out := `❯ src/calc.test.ts (2 tests | 1 failed) 5ms
× src/calc.test.ts > add > adds numbers 3ms
⎯ Failed Tests 1
Tests  1 failed | 1 passed (2)`
        records := classify("vitest run", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "js-test", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Equal(t, "× src/calc.test.ts > add > adds numbers 3ms", records[0].Detail)
}

func TestClassifyJsTestJestFail(t *testing.T) {
        out := `PASS __tests__/ok.test.js
FAIL __tests__/bad.test.js
  ● Test suite failed to run
    Cannot find module 'left-pad'
Tests:       1 failed, 1 passed, 2 total`
        records := classify("jest", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, Fail, records[0].Status)
        assert.Equal(t, "FAIL __tests__/bad.test.js", records[0].Detail)
}

func TestClassifyJsTestTAP(t *testing.T) {
        out := `TAP version 13
ok 1 - works
not ok 2 - breaks
# fail 1
# pass 1`
        records := classify("npm test --reporter=tap", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, Fail, records[0].Status)
        assert.Equal(t, "not ok 2 - breaks", records[0].Detail)
}

func TestClassifyPlaywrightFail(t *testing.T) {
        out := `✘  2 [chromium] › login.spec.ts:14:5 › logs in (5ms)
  1 failed (1.2s)`
        records := classify("npx playwright test", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "playwright-test", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Contains(t, records[0].Detail, "login.spec.ts")
}

func TestClassifyMypy(t *testing.T) {
        assert.Equal(t, Pass, classify("mypy src/", "Success: no issues found in 3 source files", false)[0].Status)

        out := "src/app.py:12: error: Incompatible types in assignment [assignment]\nFound 1 error in 1 file (checked 4 source files)"
        records := classify("mypy src/", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "mypy", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Contains(t, records[0].Detail, "src/app.py:12: error:")

        // python -m mypy and uv run mypy invocations classify too.
        assert.Equal(t, Pass, classify("python -m mypy .", "Success: no issues found in 1 source file", false)[0].Status)
        assert.Equal(t, Pass, classify("uv run mypy .", "Success: no issues found in 1 source file", false)[0].Status)
}

func TestClassifyRuff(t *testing.T) {
        assert.Equal(t, Pass, classify("ruff check .", "All checks passed!", false)[0].Status)

        out := "foo.py:1:1: F401 'os' imported but unused\nFound 1 error."
        records := classify("ruff check .", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "ruff", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Equal(t, "foo.py:1:1: F401 'os' imported but unused", records[0].Detail)
}

func TestClassifyEslint(t *testing.T) {
        assert.Equal(t, Pass, classify("eslint src/", "", false)[0].Status)
        assert.Equal(t, Pass, classify("npx eslint .", "", false)[0].Status)

        out := `/home/x/src/a.js
  3:10  error  'unused' is defined but never used  no-unused-vars

✖ 1 problem (1 error, 0 warnings)`
        records := classify("eslint src/", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "eslint", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Contains(t, records[0].Detail, "no-unused-vars")
}

func TestClassifyClippy(t *testing.T) {
        assert.Equal(t, Pass, classify("cargo clippy", "Finished dev", false)[0].Status)

        out := "error: this expression creates a temporary value\n --> src/main.rs:5:13\nerror: could not compile"
        records := classify("cargo clippy -- -D warnings", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "cargo-clippy", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Contains(t, records[0].Detail, "temporary value")
}

func TestClassifyDotnetTest(t *testing.T) {
        out := "  Passed UnitTests.Works [12 ms]\n  Failed UnitTests.Breaks [15 ms]\nFailed!  - Failed: 1, Passed: 1, Skipped: 0, Total: 2"
        records := classify("dotnet test", out, true)
        require.Len(t, records, 1)
        assert.Equal(t, "dotnet-test", records[0].Check)
        assert.Equal(t, Fail, records[0].Status)
        assert.Equal(t, "Failed UnitTests.Breaks [15 ms]", records[0].Detail)

        assert.Equal(t, Pass, classify("dotnet test", "Passed!  - Failed: 0, Passed: 3, Skipped: 0, Total: 3", false)[0].Status)
}

// TestClassifySuiteCompoundPass pins the credit-all doctrine for passing
// compounds: every recognized suite segment is credited.
func TestClassifySuiteCompoundPass(t *testing.T) {
        records := classify("ruff check . && mypy src/ && pytest -q tests/", "All checks passed!\n3 passed", false)
        byID := map[string]Status{}
        for _, r := range records {
                byID[r.Check] = r.Status
        }
        assert.Equal(t, Pass, byID["ruff"])
        assert.Equal(t, Pass, byID["mypy"])
}

// TestClassifySuiteCompoundFailRecordsNothing pins the ambiguity rule: a
// failed compound of two check families attributes nothing.
func TestClassifySuiteCompoundFailRecordsNothing(t *testing.T) {
        out := "foo.py:1:1: F401 'os' imported but unused"
        assert.Empty(t, classify("ruff check . && mypy src/", out, true))
        // A go-family segment in the compound blocks js attribution too.
        assert.Empty(t, classify("go build ./... && npm test", "build error", true))
}

// TestClassifySuiteNonCheckSegmentsDoNotBlock pins that setup segments (cd,
// env) neither classify nor block attribution — the build/vet tolerance.
func TestClassifySuiteNonCheckSegmentsDoNotBlock(t *testing.T) {
        records := classify("cd pkg && npm test", "FAIL tests/a.test.js\nTests: 1 failed", true)
        require.Len(t, records, 1)
        assert.Equal(t, Fail, records[0].Status)
}

func TestClassifySuiteUnrelatedStillNil(t *testing.T) {
        assert.Nil(t, classify("node server.js", "listening", false))
        assert.Nil(t, classify("cargo build --release", "Finished", false))
        // `jest ... || true` IS a jest run that the shell reports as passing;
        // classification sees the segment and credits it — same as `go test || true`.
        assert.Equal(t, Pass, classify("jest --config jest.config.js || true", "", false)[0].Status)
}

// ---------- watched checks ----------

func TestExitWatchRecordsAndRatchets(t *testing.T) {
        l := newLedgerEvery("", 0)
        require.True(t, l.Watch("exit:0:make lint"))
        require.False(t, l.Watch("exit:0:")) // malformed never reaches here (ValidCheckID first), but Watch is defensive

        // Unwatched commands record nothing even when they match nothing else.
        assert.False(t, l.ObserveExit("make lint-thing", "", 0, false, now).Any())

        v := l.ObserveExit("make lint", "all good", 0, false, now)
        require.Len(t, v.Improved, 1)
        assert.Equal(t, "exit:0:make lint", v.Improved[0].Check)

        // Later failure is a protected regression with the exit code as detail.
        v = l.ObserveExit("make lint", "Makefile:4: *** missing separator", 2, true, now)
        require.Len(t, v.Regressed, 1)
        assert.Equal(t, "exit:0:make lint", v.Regressed[0].Check)
        assert.Equal(t, "exit 2", v.Regressed[0].Detail)

        st := l.CheckStatus("exit:0:make lint")
        assert.Equal(t, CheckFailing, st.State)
}

func TestExitWatchNonZeroExpectation(t *testing.T) {
        l := newLedgerEvery("", 0)
        require.True(t, l.Watch("exit:1:./scripts/fail-fast.sh"))

        v := l.ObserveExit("./scripts/fail-fast.sh", "expected failure", 1, true, now)
        require.Len(t, v.Improved, 1)
        assert.Equal(t, "exit:1:./scripts/fail-fast.sh", v.Improved[0].Check)

        // Succeeding now regresses the expected-failure check.
        v = l.ObserveExit("./scripts/fail-fast.sh", "", 0, false, now)
        require.Len(t, v.Regressed, 1)
        assert.Equal(t, "exit 0", v.Regressed[0].Detail)
}

func TestExitWatchUnknownCodeFailsZeroExpectation(t *testing.T) {
        l := newLedgerEvery("", 0)
        l.Watch("exit:0:make lint")
        // exitCode unknown (-1), failed known true → not zero → fail. A
        // first-ever fail is a neutral observation (nothing regressed).
        v := l.ObserveExit("make lint", "boom\n(exit error: exit status 1)", -1, true, now)
        assert.Equal(t, 1, v.Neutral)
        assert.Len(t, v.Improved, 0)
        st := l.CheckStatus("exit:0:make lint")
        assert.Equal(t, CheckFailing, st.State)
        assert.Contains(t, st.Detail, "non-zero exit")
}

func TestHTTPWatchViaCurl(t *testing.T) {
        l := newLedgerEvery("", 0)
        require.True(t, l.Watch("http:200:localhost:3000/health"))

        // Unwatched URLs record nothing.
        out := "HTTP/1.1 200 OK\ncontent-type: text/html"
        assert.False(t, l.ObserveExit("curl -sI http://example.com/", out, 0, false, now).Any())

        v := l.ObserveExit("curl -sI http://localhost:3000/health", out, 0, false, now)
        require.Len(t, v.Improved, 1)
        assert.Equal(t, "http:200:localhost:3000/health", v.Improved[0].Check)

        // A 500 later is a regression carrying the observed code.
        out500 := "HTTP/1.1 500 Internal Server Error"
        v = l.ObserveExit("curl -sI http://localhost:3000/health", out500, 0, false, now)
        require.Len(t, v.Regressed, 1)
        assert.Equal(t, "got 500", v.Regressed[0].Detail)
}

func TestHTTPWatchWriteOutCode(t *testing.T) {
        l := newLedgerEvery("", 0)
        l.Watch("http:200:localhost:3000/")
        v := l.ObserveExit("curl -s -o /dev/null -w '%{http_code}' http://localhost:3000/", "200", 0, false, now)
        require.Len(t, v.Improved, 1)
}

func TestPortWatchViaNC(t *testing.T) {
        l := newLedgerEvery("", 0)
        require.True(t, l.Watch("port:localhost:5432"))

        assert.False(t, l.ObserveExit("nc -z localhost 6379", "", 0, false, now).Any())

        v := l.ObserveExit("nc -z -w 2 localhost 5432", "", 0, false, now)
        require.Len(t, v.Improved, 1)
        assert.Equal(t, "port:localhost:5432", v.Improved[0].Check)

        v = l.ObserveExit("nc -z localhost 5432", "nc: connect refused", 1, true, now)
        require.Len(t, v.Regressed, 1)
        assert.Equal(t, "connection refused", v.Regressed[0].Detail)
}

func TestConfigDefinedChecks(t *testing.T) {
        l := newLedgerEvery("", 0)
        err := l.SetChecks([]CheckDef{
                {Name: "lint", Command: "make lint", Exit: 0},
                {Name: "health", Command: "curl -s localhost:3000/health", Pattern: `"status":"ok"`},
        })
        require.NoError(t, err)

        // Exact command match records the named check.
        v := l.ObserveExit("make lint", "clean", 0, false, now)
        require.Len(t, v.Improved, 1)
        assert.Equal(t, "check:lint", v.Improved[0].Check)

        // Pattern match gate: exit 0 but body mismatch fails. A first-ever
        // fail is neutral (nothing regressed), so read state instead.
        out := `{"status":"degraded"}`
        v = l.ObserveExit("curl -s localhost:3000/health", out, 0, false, now)
        assert.Equal(t, 1, v.Neutral)
        assert.Len(t, v.Improved, 0)
        st := l.CheckStatus("check:health")
        assert.Equal(t, CheckFailing, st.State)
        assert.Contains(t, st.Detail, "success pattern")

        // Near-miss commands record nothing.
        assert.False(t, l.ObserveExit("make lint-all", "", 0, false, now).Any())

        // Bad pattern fails loudly at install time.
        err = l.SetChecks([]CheckDef{{Name: "x", Command: "x", Pattern: "["}})
        assert.Error(t, err)
}

func TestWatchedCheckIDGrammar(t *testing.T) {
        for _, id := range []string{
                "exit:0:make lint", "exit:1:./scripts/fail.sh", "exit:130:make check",
                "http:200:localhost:3000/health", "http:301:example.com/x?y=1",
                "port:localhost:5432", "port:db.internal:5432",
                "check:lint", "js-test", "playwright-test", "mypy", "ruff", "eslint",
                "cargo-clippy", "dotnet-test",
        } {
                assert.True(t, ValidCheckID(id), id)
        }
        for _, id := range []string{
                "exit:make lint", "exit:-1:x", "exit:0:", "exit:x:y",
                "http:200:", "http:99:x", "http:600:x", "port:", "port:x:", "port:x:abc",
                "check:", "check:has space", "js-Tests", "npm-test",
        } {
                assert.False(t, ValidCheckID(id), id)
        }
}

// TestWatchContractFlow pins the declare → run → judge loop end-to-end at
// the ledger level, including ContractStatus on watched ids.
func TestWatchContractFlow(t *testing.T) {
        l := newLedgerEvery("", 0)
        assert.Equal(t, CheckNoEvidence, l.CheckStatus("exit:0:make check").State)
        l.Watch("exit:0:make check")
        l.ObserveExit("make check", "ok", 0, false, now)
        assert.Equal(t, CheckPassing, l.CheckStatus("exit:0:make check").State)

        residual := l.Residual([]string{"exit:0:make check", "js-test"})
        // js-test has no evidence: residual blocks. exit watch passes.
        require.Len(t, residual, 1)
        assert.Equal(t, "js-test", residual[0].Check)
}
