package evidence

import (
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
)

// TestJevCheckID pins the slug grammar: deterministic across runs of the
// same command, whitespace-free (ValidCheckID), and bounded.
func TestJevCheckID(t *testing.T) {
        cases := []struct {
                command string
                want    string
        }{
                {"make check", "jev:make-check"},
                {"  make check  ", "jev:make-check"},
                {"go test ./a b", "jev:go-test-./a-b"},
                {"just verify --fast", "jev:just-verify---fast"},
                {"./ci.sh", "jev:./ci.sh"},
                {"", "jev:unknown"},
                {"   ", "jev:unknown"},
        }
        for _, tc := range cases {
                assert.Equal(t, tc.want, JevCheckID(tc.command), "command %q", tc.command)
        }
}

// TestJevCheckIDStableAndBounded: repeated runs agree; a pathological
// command cannot forge an unbounded id.
func TestJevCheckIDStableAndBounded(t *testing.T) {
        cmd := "make check"
        assert.Equal(t, JevCheckID(cmd), JevCheckID(cmd))

        giant := JevCheckID(strings.Repeat("run with flags ", 100))
        assert.LessOrEqual(t, len(giant), len("jev:")+jevCheckIDCap)
}

// TestValidCheckIDJevGrammar: jev ids are declarable as contract checks.
func TestValidCheckIDJevGrammar(t *testing.T) {
        assert.True(t, ValidCheckID("jev:make-check"))
        assert.True(t, ValidCheckID("jev:./ci.sh"))
        assert.False(t, ValidCheckID("jev:"))
        assert.False(t, ValidCheckID("jev:has space"))
        assert.False(t, ValidCheckID("jev"))
}

// TestObserveJevRatchetsLikeDeterministicEvidence: a JEV pass protects,
// and a later fail regresses exactly like a parser-admitted record.
func TestObserveJevRatchetsLikeDeterministicEvidence(t *testing.T) {
        l := NewLedger("")
        at := time.Now()

        // Fail first: new information, no regression.
        v := l.ObserveJev("make check", Fail, "error: tests failed [jev verdict, confidence 0.91]", at)
        assert.True(t, v.Any())
        assert.False(t, v.Regression())

        // Pass: improvement.
        v = l.ObserveJev("make check", Pass, "", at)
        assert.True(t, v.Any())
        assert.Len(t, v.Improved, 1)

        // Fail again: protected regression.
        v = l.ObserveJev("make check", Fail, "error: tests failed [jev verdict, confidence 0.88]", at)
        assert.True(t, v.Regression())
        assert.Equal(t, []Record{{
                Check:   "jev:make-check",
                Status:  Fail,
                At:      at,
                Detail:  "error: tests failed [jev verdict, confidence 0.88]",
                Command: "make check",
        }}, v.Regressed)

        // The regression note renders with the jev id.
        note := RegressionNote(v.Regressed)
        assert.Contains(t, note, "jev:make-check")
}
