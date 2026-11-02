package evidence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidCheckID(t *testing.T) {
	valid := []string{
		"go-build", "go-vet", "cargo-build",
		"go-test:github.com/damien1141/a1/internal/auth",
		"go-test#TestLogin",
		"go-test:github.com/damien1141/a1/internal/...",
		"go-test:...", // whole module
		"pytest:tests/test_a.py::test_b",
		"pytest:tests/test_a.py::test_x[param with space]",
		"cargo-test:unit::tests::thing",
	}
	for _, id := range valid {
		assert.True(t, ValidCheckID(id), id)
	}

	invalid := []string{
		"", "   ", "go-build ", "gobuild",
		"go-test:", "go-test:  ", "go-test:pkg with space",
		"go-test#", "go-test#Name With Space",
		"pytest:", "cargo-test:", "cargo-test:name with space",
		"make:", "npm-test:foo", "go-fmt",
	}
	for _, id := range invalid {
		assert.False(t, ValidCheckID(id), id)
	}
}

// seedContractLedger builds a ledger with a known multi-package state:
// auth passes, util passes, store fails; go-build passes.
func seedContractLedger(t *testing.T) *Ledger {
	t.Helper()
	l := NewLedger("")
	observeAt(l, "go test ./...", "ok\tgithub.com/x/auth\t0.1s\nok\tgithub.com/x/util\t0.1s\nFAIL\tgithub.com/x/store\t0.1s\nFAIL", true)
	observeAt(l, "go build ./...", "", false)
	return l
}

func TestCheckStatusExact(t *testing.T) {
	l := seedContractLedger(t)

	assert.Equal(t, CheckPassing, l.CheckStatus("go-build").State)
	assert.Equal(t, CheckPassing, l.CheckStatus("go-test:github.com/x/auth").State)
	assert.Equal(t, CheckFailing, l.CheckStatus("go-test:github.com/x/store").State)
	st := l.CheckStatus("go-test:github.com/x/store")
	assert.Contains(t, st.Detail, "go-test:github.com/x/store")
	assert.Equal(t, CheckNoEvidence, l.CheckStatus("go-test:github.com/x/neverrun").State)
	assert.Equal(t, CheckNoEvidence, l.CheckStatus("go-vet").State)
}

func TestCheckStatusPatterns(t *testing.T) {
	l := seedContractLedger(t)

	// Prefix covering auth + util only: all pass.
	st := l.CheckStatus("go-test:github.com/x/auth/...")
	assert.Equal(t, CheckPassing, st.State)
	assert.Equal(t, 1, st.Matches)

	// Prefix covering all three: store fails → pattern fails, detail names it.
	st = l.CheckStatus("go-test:github.com/x/...")
	assert.Equal(t, CheckFailing, st.State)
	assert.Equal(t, 3, st.Matches)
	assert.Contains(t, st.Detail, "go-test:github.com/x/store")

	// Bare whole-module form matches every package check.
	st = l.CheckStatus("go-test:...")
	assert.Equal(t, CheckFailing, st.State)
	assert.Equal(t, 3, st.Matches)

	// Prefix matching nothing: NO EVIDENCE — never a vacuous pass.
	st = l.CheckStatus("go-test:github.com/nope/...")
	assert.Equal(t, CheckNoEvidence, st.State)
	assert.Equal(t, 0, st.Matches)

	// Patterns never match per-test checks or build/vet checks.
	st = l.CheckStatus("go-test:go-build")
	assert.Equal(t, CheckNoEvidence, st.State, "pattern prefix cannot capture non-package ids")
}

func TestCheckStatusRegressedContractCheckFails(t *testing.T) {
	// A contract check that passed and later regressed reads failing —
	// last-known evidence, no freshness fabrication.
	l := NewLedger("")
	observeAt(l, "go build ./...", "", false)
	assert.Equal(t, CheckPassing, l.CheckStatus("go-build").State)
	observeAt(l, "go build ./...", "auth/handler.go:1:1: broken", true)
	assert.Equal(t, CheckFailing, l.CheckStatus("go-build").State)
}

func TestContractStatusAndResidual(t *testing.T) {
	l := seedContractLedger(t)
	contract := []string{
		"go-test:github.com/x/auth",
		"go-test:github.com/x/store",
		"go-test:github.com/x/neverrun",
		"go-build",
		"go-build", // duplicate collapses
		"  ",       // blank collapses
	}

	all := l.ContractStatus(contract)
	assert.Len(t, all, 4, "duplicates and blanks collapse")
	assert.Equal(t, "go-build", all[0].Check, "sorted by check id")

	residual := l.Residual(contract)
	assert.Len(t, residual, 2)
	assert.Equal(t, "go-test:github.com/x/neverrun", residual[0].Check)
	assert.Equal(t, CheckNoEvidence, residual[0].State)
	assert.Equal(t, "go-test:github.com/x/store", residual[1].Check)
	assert.Equal(t, CheckFailing, residual[1].State)

	// A fully green contract has an empty residual — the zero-rank set.
	assert.Empty(t, l.Residual([]string{"go-build", "go-test:github.com/x/auth"}))
}
