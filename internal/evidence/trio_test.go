package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var trioAt = time.Date(2026, 11, 2, 9, 30, 0, 0, time.UTC)

func trioChecks(recs []Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Check)
	}
	return out
}

// ---------- differential ----------

// naiveGoTestRecords re-derives go test records with strings.Split and
// strings.Fields instead of the package's regexps: package summary lines
// ("ok"/"FAIL <pkg>") and verbose per-test lines ("--- PASS:/FAIL: <name>"),
// first verdict per check id wins, and a package token must contain "/" or
// "." to count as an import path.
func naiveGoTestRecords(out string) map[string]Status {
	records := map[string]Status{}
	set := func(check string, st Status) {
		if _, ok := records[check]; !ok {
			records[check] = st
		}
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "ok" || fields[0] == "FAIL") &&
			strings.ContainsAny(fields[1], "./") {
			st := Pass
			if fields[0] == "FAIL" {
				st = Fail
			}
			set("go-test:"+fields[1], st)
			continue
		}
		if len(fields) >= 3 && fields[0] == "---" && (fields[1] == "PASS:" || fields[1] == "FAIL:") {
			st := Pass
			if fields[1] == "FAIL:" {
				st = Fail
			}
			set("go-test#"+fields[2], st)
		}
	}
	return records
}

// oracle: naive strings reference — a line scanner built on strings.Split
// and strings.Fields re-derives the package and per-test records from the
// same outputs (tabs, spaces, CRLF, flaky reruns, path-less packages).
func TestClassifyGoTest_Differential(t *testing.T) {
	fixtures := []struct{ name, out string }{
		{
			name: "canonical mixed run",
			out: `=== RUN   TestLogin
--- PASS: TestLogin (0.01s)
=== RUN   TestLogout
--- FAIL: TestLogout (0.00s)
    auth_test.go:42: session not cleared
FAIL
FAIL	example.com/a/internal/auth	0.103s
ok  	example.com/a/internal/util	0.042s
?   	example.com/a/internal/notty [no test files]
FAIL`,
		},
		{
			name: "spaces instead of tabs",
			out:  "ok      example.com/a/b  0.5s\nFAIL    example.com/a/c [build failed]\n",
		},
		{
			// A token with no "/" or "." is not an import path; the guard
			// also swallows a bare duration, which contains a dot.
			name: "path-less tokens are ignored",
			out:  "ok      localpkg  0.5s\nFAIL    3.2s\n",
		},
		{
			name: "flaky rerun keeps the first verdict",
			out:  "--- PASS: TestFlaky (0.01s)\n--- FAIL: TestFlaky (0.00s)\nok  example.com/a/d  0.1s\nok  example.com/a/d  0.2s\n",
		},
		{
			name: "crlf line endings",
			out:  "ok\texample.com/a/e\t0.1s\r\n--- FAIL: TestCrash (0.5s)\r\n",
		},
		{
			name: "subtests and non-summary noise",
			out: `=== RUN   TestTop
=== RUN   TestTop/sub
--- PASS: TestTop/sub (0.00s)
--- PASS: TestTop (0.01s)
    parser.go:9: noise that is not a summary
ok 	example.com/a/f	0.2s`,
		},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			got := map[string]Status{}
			for _, r := range classify("go test ./...", fx.out, true) {
				if strings.HasPrefix(r.Check, "go-test") {
					got[r.Check] = r.Status
				}
			}
			assert.Equal(t, naiveGoTestRecords(fx.out), got)
		})
	}
}

// oracle: hand-written golden JSONL lines — struct field order, omitempty
// drops, and the RFC3339 timestamp are stated here, then cross-checked by
// decoding each line back through encoding/json.
func TestReceiptLines_Differential_Golden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.jsonl")
	l := NewLedger(path)

	require.Len(t, l.Observe("go build ./...", "", false, trioAt).Improved, 1)
	require.Equal(t, 1, l.Observe("go vet ./...",
		"v.go:1:2: unused variable x\n(exit error: exit status 1)", true, trioAt).Neutral,
		"a first-time vet failure is new information, not a regression")

	want := `{"check":"go-build","status":"pass","at":"2026-11-02T09:30:00Z","command":"go build ./..."}` + "\n" +
		`{"check":"go-vet","status":"fail","at":"2026-11-02T09:30:00Z","detail":"v.go:1:2: unused variable x","command":"go vet ./..."}` + "\n"
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(data))

	// Round-trip: the golden lines decode into the record shape the ledger
	// itself replays, with the same field values.
	lines := strings.Split(strings.TrimRight(want, "\n"), "\n")
	var build, vet Record
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &build))
	assert.Equal(t, "go-build", build.Check)
	assert.Equal(t, Pass, build.Status)
	assert.Equal(t, "", build.Detail, "passes carry no detail")
	assert.Equal(t, "go build ./...", build.Command)
	assert.True(t, build.At.Equal(trioAt))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &vet))
	assert.Equal(t, "go-vet", vet.Check)
	assert.Equal(t, Fail, vet.Status)
	assert.Equal(t, "v.go:1:2: unused variable x", vet.Detail)
	assert.Equal(t, "go vet ./...", vet.Command)
}

// oracle: golden literal for the note's exact text; the 160-byte detail cut
// is re-derived by an independent truncation below it.
func TestRegressionNote_Differential(t *testing.T) {
	got := RegressionNote([]Record{
		{Check: "go-test:auth", Status: Fail, Detail: "  session not cleared  "},
		{Check: "go-vet", Status: Fail},
	})
	want := "[evidence] PROTECTED REGRESSION — previously passing check(s) now failing:\n" +
		"- go-test:auth — session not cleared\n" +
		"- go-vet\n" +
		"Fix the regression before reporting completion. Do not delete, skip, or weaken the failing checks to go green."
	assert.Equal(t, want, got)

	// The detail cut: 160 bytes, then the ellipsis rune.
	long := RegressionNote([]Record{{Check: "cargo-build", Status: Fail, Detail: strings.Repeat("e", 300)}})
	lines := strings.Split(long, "\n")
	require.Len(t, lines, 3)
	assert.Equal(t, "- cargo-build — "+strings.Repeat("e", 160)+"…", lines[1])
}

// trioNaiveLedger is the naive ratchet reference: last record per check in a
// linear-scanned slice, ever-passed checks in a string slice. It encodes the
// documented rules only — the protected set never shrinks, a pass after a
// fail (or a first pass) improves, a fail after a pass regresses.
type trioNaiveLedger struct {
	state     []Record
	protected []string
}

func (n *trioNaiveLedger) find(check string) (Record, bool) {
	for _, r := range n.state {
		if r.Check == check {
			return r, true
		}
	}
	return Record{}, false
}

func (n *trioNaiveLedger) isProtected(check string) bool {
	for _, c := range n.protected {
		if c == check {
			return true
		}
	}
	return false
}

// observe folds one classified record and returns the verdict kind:
// "improved", "regressed", or "neutral".
func (n *trioNaiveLedger) observe(rec Record) string {
	prev, existed := n.find(rec.Check)
	replaced := false
	for i := range n.state {
		if n.state[i].Check == rec.Check {
			n.state[i] = rec
			replaced = true
		}
	}
	if !replaced {
		n.state = append(n.state, rec)
	}
	if rec.Status == Pass {
		wasProtected := n.isProtected(rec.Check)
		if !wasProtected {
			n.protected = append(n.protected, rec.Check)
		}
		switch {
		case existed && prev.Status == Fail:
			return "improved"
		case !wasProtected:
			return "improved"
		}
		return "neutral"
	}
	if existed && prev.Status == Pass {
		return "regressed"
	}
	return "neutral"
}

func (n *trioNaiveLedger) sortedState() []Record {
	out := append([]Record(nil), n.state...)
	sort.Slice(out, func(i, j int) bool { return out[i].Check < out[j].Check })
	return out
}

func (n *trioNaiveLedger) regressions() []string {
	var out []string
	for _, r := range n.state {
		if r.Status == Fail && n.isProtected(r.Check) {
			out = append(out, r.Check)
		}
	}
	sort.Strings(out)
	return out
}

func (n *trioNaiveLedger) summary() string {
	s := fmt.Sprintf("evidence: %d protected", len(n.protected))
	if regs := n.regressions(); len(regs) > 0 {
		s += fmt.Sprintf(", %d FAILING", len(regs))
	}
	return s
}

// oracle: naive ratchet reference (above) — a slice-based reimplementation
// of the documented rules driven by the same scripted observations; every
// verdict, the final state, the protected count, the regressions, and the
// Summary line (rendered here with fmt, against the ledger's hand-rolled
// itoa) must agree.
func TestLedgerRatchet_Differential(t *testing.T) {
	steps := []struct {
		name    string
		command string
		output  string
		failed  bool
		rec     Record // hand-derived classification of the step
	}{
		{"first build", "go build ./...", "", false, Record{Check: "go-build", Status: Pass}},
		{"build again", "go build ./...", "", false, Record{Check: "go-build", Status: Pass}},
		{"vet passes", "go vet ./...", "", false, Record{Check: "go-vet", Status: Pass}},
		{"tests pass", "go test ./pkg/", "ok  \tgithub.com/x/pkg\t0.1s", false,
			Record{Check: "go-test:github.com/x/pkg", Status: Pass}},
		{"tests regress", "go test ./pkg/", "FAIL\tgithub.com/x/pkg\t0.1s\nFAIL", true,
			Record{Check: "go-test:github.com/x/pkg", Status: Fail}},
		{"still failing", "go test ./pkg/", "FAIL\tgithub.com/x/pkg\t0.1s\nFAIL", true,
			Record{Check: "go-test:github.com/x/pkg", Status: Fail}},
		{"vet regresses", "go vet ./...", "v.go:1:2: boom\n(exit error: exit status 1)", true,
			Record{Check: "go-vet", Status: Fail}},
		{"build still green", "go build ./...", "", false, Record{Check: "go-build", Status: Pass}},
		{"tests recover", "go test ./pkg/", "ok  \tgithub.com/x/pkg\t0.1s", false,
			Record{Check: "go-test:github.com/x/pkg", Status: Pass}},
		{"vet recovers", "go vet ./...", "", false, Record{Check: "go-vet", Status: Pass}},
	}

	l := NewLedger("")
	naive := &trioNaiveLedger{}
	for i, step := range steps {
		at := trioAt.Add(time.Duration(i) * time.Minute)
		rec := Record{Check: step.rec.Check, Status: step.rec.Status, At: at, Command: strings.TrimSpace(step.command)}
		want := naive.observe(rec)
		v := l.Observe(step.command, step.output, step.failed, at)

		switch want {
		case "improved":
			assert.Empty(t, v.Regressed, "%s: naive says improved", step.name)
			assert.Equal(t, []string{step.rec.Check}, trioChecks(v.Improved), "%s: naive says improved", step.name)
			assert.Equal(t, 0, v.Neutral, step.name)
		case "regressed":
			assert.Empty(t, v.Improved, "%s: naive says regressed", step.name)
			assert.Equal(t, []string{step.rec.Check}, trioChecks(v.Regressed), "%s: naive says regressed", step.name)
			assert.Equal(t, 0, v.Neutral, step.name)
		default:
			assert.Empty(t, v.Regressed, "%s: naive says neutral", step.name)
			assert.Empty(t, v.Improved, "%s: naive says neutral", step.name)
			assert.Equal(t, 1, v.Neutral, step.name)
		}

		if step.name == "vet regresses" {
			// Mid-script snapshot: two protected checks failing at once.
			regs := naive.regressions()
			assert.Equal(t, []string{"go-test:github.com/x/pkg", "go-vet"}, regs)
			assert.Equal(t, regs, trioChecks(l.Regressions()))
			assert.Equal(t, naive.summary(), l.Summary())
			assert.Contains(t, l.Summary(), "2 FAILING")
		}
	}

	assert.Equal(t, len(naive.protected), l.ProtectedCount())
	assert.Empty(t, naive.regressions())
	assert.Empty(t, l.Regressions())
	assert.Equal(t, naive.summary(), l.Summary())

	want := naive.sortedState()
	got := l.State()
	require.Len(t, got, len(want))
	for i := range want {
		assert.Equal(t, want[i].Check, got[i].Check)
		assert.Equal(t, want[i].Status, got[i].Status)
		assert.True(t, got[i].At.Equal(want[i].At), "%s: At", want[i].Check)
		assert.Equal(t, want[i].Command, got[i].Command)
	}
}
