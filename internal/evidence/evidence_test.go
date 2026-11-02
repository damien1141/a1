package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

const goTestOutput = `=== RUN   TestLogin
--- PASS: TestLogin (0.01s)
=== RUN   TestLogout
--- FAIL: TestLogout (0.00s)
    auth_test.go:42: session not cleared
FAIL
FAIL    github.com/damien1141/a1/internal/auth  0.103s
ok      github.com/damien1141/a1/internal/util  0.042s
?       github.com/damien1141/a1/internal/notty [no test files]
FAIL`

const authPkg = "github.com/damien1141/a1/internal/auth"

const goBuildFailOutput = `# github.com/damien1141/a1/internal/auth
auth/handler.go:12:5: undefined: Sessions
(exit error: exit status 1)`

func TestClassifyGoTestPackages(t *testing.T) {
	records := classify("go test ./...", goTestOutput, true)
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.Check] = r
	}
	assert.Equal(t, Fail, byID["go-test:github.com/damien1141/a1/internal/auth"].Status)
	assert.Equal(t, Pass, byID["go-test:github.com/damien1141/a1/internal/util"].Status)
	// "? pkg [no test files]" must never produce a record.
	assert.NotContains(t, byID, "go-test:github.com/damien1141/a1/internal/notty")
}

func TestClassifyGoTestPerTestVerbose(t *testing.T) {
	records := classify("go test -v ./internal/auth/", goTestOutput, true)
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.Check] = r
	}
	assert.Equal(t, Pass, byID["go-test#TestLogin"].Status)
	assert.Equal(t, Fail, byID["go-test#TestLogout"].Status)
}

func TestClassifyGoBuildFail(t *testing.T) {
	records := classify("go build ./...", goBuildFailOutput, true)
	require.Len(t, records, 1)
	assert.Equal(t, "go-build", records[0].Check)
	assert.Equal(t, Fail, records[0].Status)
	assert.Contains(t, records[0].Detail, "undefined: Sessions")
}

func TestClassifyGoBuildPass(t *testing.T) {
	records := classify("go build ./...", "(no output)", false)
	require.Len(t, records, 1)
	assert.Equal(t, Pass, records[0].Status)
}

func TestClassifyGoVetFail(t *testing.T) {
	out := "auth/handler.go:12:34: this value is never used\n(exit error: exit status 1)"
	records := classify("go vet ./...", out, true)
	require.Len(t, records, 1)
	assert.Equal(t, "go-vet", records[0].Check)
	assert.Equal(t, Fail, records[0].Status)
}

// TestClassifyAmbiguousCompoundFailure pins the conservative rule: a failed
// compound command whose output has no attributable lines records NOTHING.
func TestClassifyAmbiguousCompoundFailure(t *testing.T) {
	out := "auth/handler.go:12:34: some vet complaint\n(exit error: exit status 1)"
	records := classify("go vet ./... && go test ./...", out, true)
	assert.Empty(t, records, "ambiguous compound failure must not poison the ledger")
}

// TestClassifyCompoundPassRecordsAll pins that a passing compound command
// credits every segment (a && chain reaching the end proves all stages).
func TestClassifyCompoundPassRecordsAll(t *testing.T) {
	out := "ok  \tgithub.com/x/a\t0.1s"
	records := classify("go build ./... && go vet ./... && go test ./internal/a/", out, false)
	byID := map[string]Status{}
	for _, r := range records {
		byID[r.Check] = r.Status
	}
	assert.Equal(t, Pass, byID["go-build"])
	assert.Equal(t, Pass, byID["go-vet"])
	assert.Equal(t, Pass, byID["go-test:github.com/x/a"])
}

func TestClassifyUnrelatedCommand(t *testing.T) {
	assert.Nil(t, classify("ls -la", "total 0", false))
	assert.Nil(t, classify("git status", "On branch main", false))
}

func TestClassifyPytest(t *testing.T) {
	out := `tests/test_a.py::test_ok PASSED [ 20%]
tests/test_a.py::test_bad FAILED [ 40%]
=========================== short test summary info ============================
FAILED tests/test_b.py::test_other - AssertionError: expected 42
PASSED tests/test_b.py::test_fine`
	records := classify("pytest -v tests/", out, true)
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.Check] = r
	}
	assert.Equal(t, Pass, byID["pytest:tests/test_a.py::test_ok"].Status)
	assert.Equal(t, Fail, byID["pytest:tests/test_a.py::test_bad"].Status)
	assert.Equal(t, Fail, byID["pytest:tests/test_b.py::test_other"].Status)
	assert.Equal(t, "AssertionError: expected 42", byID["pytest:tests/test_b.py::test_other"].Detail)
	assert.Equal(t, Pass, byID["pytest:tests/test_b.py::test_fine"].Status)
}

func TestClassifyCargoTest(t *testing.T) {
	out := `test unit::tests::adds ... ok
test unit::tests::subs ... FAILED
test unit::tests::skipped ... ignored

failures:
    unit::tests::subs`
	records := classify("cargo test", out, true)
	byID := map[string]Record{}
	for _, r := range records {
		byID[r.Check] = r
	}
	assert.Equal(t, Pass, byID["cargo-test:unit::tests::adds"].Status)
	assert.Equal(t, Fail, byID["cargo-test:unit::tests::subs"].Status)
	assert.NotContains(t, byID, "cargo-test:unit::tests::skipped")
}

func TestClassifyCargoCompileFailure(t *testing.T) {
	out := "error[E0432]: unresolved import `crate::missing`"
	records := classify("cargo test", out, true)
	require.Len(t, records, 1)
	assert.Equal(t, "cargo-build", records[0].Check)
	assert.Equal(t, Fail, records[0].Status)
	assert.Contains(t, records[0].Detail, "E0432")
}

func TestRatchetVerdictMatrix(t *testing.T) {
	l := NewLedger("")

	// Build passes: new protected evidence.
	v := l.Observe("go build ./...", "", false, now)
	assert.Empty(t, v.Regressed)
	assert.Len(t, v.Improved, 1)

	// Build passes again: neutral.
	v = l.Observe("go build ./...", "", false, now)
	assert.Empty(t, v.Regressed)
	assert.Empty(t, v.Improved)
	assert.Equal(t, 1, v.Neutral)

	// Test fails first time: new information, not a regression.
	v = l.Observe("go test ./internal/auth/", "FAIL\t"+authPkg+"\t0.1s\nFAIL", true, now)
	assert.Empty(t, v.Regressed)

	// Test passes: strict improvement, now protected.
	v = l.Observe("go test ./internal/auth/", "ok  \t"+authPkg+"\t0.1s", false, now)
	assert.Empty(t, v.Regressed)
	assert.Len(t, v.Improved, 1)

	// Test fails again: PROTECTED REGRESSION.
	v = l.Observe("go test ./internal/auth/", "FAIL\t"+authPkg+"\t0.1s\nFAIL", true, now)
	require.Len(t, v.Regressed, 1)
	assert.Equal(t, "go-test:"+authPkg, v.Regressed[0].Check)

	// Repeated failure stays neutral (no repeat nagging).
	v = l.Observe("go test ./internal/auth/", "FAIL\t"+authPkg+"\t0.1s\nFAIL", true, now)
	assert.Empty(t, v.Regressed)

	// Regressions() reports the outstanding failure; recovery clears it.
	require.Len(t, l.Regressions(), 1)
	assert.Equal(t, 2, l.ProtectedCount())
	v = l.Observe("go test ./internal/auth/", "ok  \t"+authPkg+"\t0.1s", false, now)
	assert.Len(t, v.Improved, 1)
	assert.Empty(t, l.Regressions())
}

func TestLedgerPersistenceReplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.jsonl")

	l1 := NewLedger(path)
	l1.Observe("go build ./...", "", false, now)
	l1.Observe("go test ./pkg/", "ok  \tgithub.com/x/pkg\t0.1s", false, now)
	l1.Observe("go test ./pkg/", "FAIL\tgithub.com/x/pkg\t0.1s\nFAIL", true, now)
	require.Len(t, l1.Regressions(), 1)

	// A fresh ledger replays receipts and reconstructs identical state.
	l2 := NewLedger(path)
	require.NoError(t, l2.Load())
	require.Len(t, l2.Regressions(), 1)
	assert.Equal(t, "go-test:github.com/x/pkg", l2.Regressions()[0].Check)
	assert.Equal(t, 2, l2.ProtectedCount())

	// The replayed ledger still enforces the ratchet: the currently failing
	// check failing AGAIN is not a new regression (state was replayed).
	v := l2.Observe("go test ./pkg/", "FAIL\tgithub.com/x/pkg\t0.1s\nFAIL", true, now)
	assert.Empty(t, v.Regressed)

	// Receipts are valid JSONL.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var rec Record
	assert.NoError(t, json.Unmarshal(firstLineBytes(data), &rec))
}

func TestLedgerSkipsCorruptReceipts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.jsonl")
	good := `{"check":"go-build","status":"pass","at":"2026-10-29T12:00:00Z"}`
	require.NoError(t, os.WriteFile(path, []byte(good+"\n{corrupt}\n\n"), 0o644))

	l := NewLedger(path)
	require.NoError(t, l.Load())
	assert.Equal(t, 1, l.ProtectedCount())
}

func TestRegressionNoteFormatting(t *testing.T) {
	note := RegressionNote([]Record{
		{Check: "go-test:auth", Status: Fail, Detail: "session not cleared"},
		{Check: "go-vet", Status: Fail},
	})
	assert.Contains(t, note, "[evidence] PROTECTED REGRESSION")
	assert.Contains(t, note, "go-test:auth — session not cleared")
	assert.Contains(t, note, "go-vet\n") // no trailing dash when no detail
	assert.Contains(t, note, "Do not delete, skip, or weaken")
}

func TestSummary(t *testing.T) {
	empty := NewLedger("")
	assert.Contains(t, empty.Summary(), "no checks")

	l := NewLedger("")
	l.Observe("go test ./a/", "ok  \tgithub.com/x/a\t0.1s", false, now)
	assert.Contains(t, l.Summary(), "1 protected")
	l.Observe("go test ./a/", "FAIL\tgithub.com/x/a\t0.1s\nFAIL", true, now)
	assert.Contains(t, l.Summary(), "FAILING")
}

func TestNilLedgerSafety(t *testing.T) {
	var l *Ledger
	assert.NotPanics(t, func() {
		v := l.Observe("go test ./...", "ok", false, now)
		assert.Empty(t, v.Regressed)
		assert.Nil(t, l.State())
		assert.Equal(t, 0, l.ProtectedCount())
		assert.Empty(t, l.Regressions())
		assert.NotNil(t, l.Summary())
	})
}

// TestLongDetailTruncation keeps ledger details bounded.
func TestLongDetailTruncation(t *testing.T) {
	long := strings.Repeat("x", 500)
	records := classify("go build ./...", "pkg/a.go:1:1: "+long+"\n(exit error: exit status 1)", true)
	require.Len(t, records, 1)
	assert.LessOrEqual(t, len(records[0].Detail), 204) // 200 bytes + ellipsis rune (3 bytes)
}

func firstLineBytes(b []byte) []byte {
	for i, c := range b {
		if c == '\n' {
			return b[:i]
		}
	}
	return b
}
