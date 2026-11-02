package agent

// Chat-mode ergonomics of the evidence surface: a session becomes a coding
// session when the model declares its first contract, and the declaration is
// the classifier — no user toggle, no intent heuristic, no workspace
// sniffing (chatting inside a repo is the common case and must not govern).
// Until then the evidence surfaces are inert: an incidental build command
// in a chat turn leaves no trace and cannot arm regression notes for later
// casual turns in the same session.

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/permission"
	"github.com/damien1141/a1/internal/tools"
)

// newGovernedEngine builds an engine on a fresh in-memory session with a
// bash tool wired (no LLM server: these tests drive the executor directly).
func newGovernedEngine(t *testing.T, bash tools.Tool, opts ...EngineOption) *Engine {
	t.Helper()
	sess, err := NewSession(WithCwd(t.TempDir()))
	require.NoError(t, err)
	all := append([]EngineOption{
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{bash}),
	}, opts...)
	engine, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess, all...)
	require.NoError(t, err)
	return engine
}

// contextContains reports whether any message in the session context carries
// the marker — tool-result notes included, which is where regression notes
// ride (userNotes only sees RoleUser system-note injections).
func contextContains(engine *Engine, marker string) bool {
	for _, msg := range engine.session.BuildContext() {
		if strings.Contains(msg.Content, marker) {
			return true
		}
	}
	return false
}

// TestUngovernedSessionLeavesNoTrace is the core inertness pin: a fresh
// session runs build/test commands — even failing ones — and the ledger
// stays empty, no verdicts collect, no note rides, no baseline is pinned,
// and the compaction evidence record renders empty.
func TestUngovernedSessionLeavesNoTrace(t *testing.T) {
	var scripted string
	engine := newGovernedEngine(t, stubBashTool(func() string { return scripted }))
	require.False(t, engine.IsGoverned(), "a fresh session starts ungoverned")

	// A passing build in a chat-flavored turn: nothing recorded.
	scripted = evidenceGoTestPass
	msg := runToolCall(t, engine.executor, "bash", `{"command":"go test ./internal/auth/"}`)
	assert.NotContains(t, msg.Content, "[evidence]")
	assert.Empty(t, engine.EvidenceLedger().State(), "ungoverned: the ledger stays empty")
	assert.Zero(t, engine.EvidenceLedger().ProtectedCount())

	// The same check now fails: still no note, still no ledger, no verdicts.
	scripted = evidenceGoTestFail + "\n(exit error: exit status 1)"
	msg = runToolCall(t, engine.executor, "bash", `{"command":"go test ./internal/auth/"}`)
	assert.NotContains(t, msg.Content, "[evidence]", "no regression note in an ungoverned session")
	assert.Empty(t, engine.EvidenceLedger().State())
	assert.Empty(t, engine.EvidenceLedger().Regressions())
	assert.Empty(t, engine.executor.DrainEvidenceVerdicts(), "no verdicts collect, so the verdict gate sees no candidate")

	// Chat-inert compaction record and an inert verdict gate.
	assert.Empty(t, engine.renderEvidenceRecord(), "no evidence record without observations")
	snaps := &fakeSnapshots{}
	engine.setEvidenceSnapshots(snaps)
	engine.newEvidenceGate().beforeBatch(t.Context(), true)
	assert.Empty(t, snaps.created, "no protected checks, so no baseline is pinned")
}

// TestChatTurnWithIncidentalBuildStaysChat: end-to-end through the loop —
// a chat turn that happens to run a build command grows no ledger, emits no
// system notes, and stays byte-identical to plain chat behavior.
func TestChatTurnWithIncidentalBuildStaysChat(t *testing.T) {
	server, requests := scriptedLLMServer([]scriptedRound{
		{Tool: "bash", Args: `{"command":"go build ./..."}`},
		{Text: "it builds fine — anyway, as I was saying"},
	})
	defer server.Close()

	engine := newZeroRankEngine(t, server.URL, sequencedBashTool(evidenceGoTestPass))
	require.NoError(t, drainLoop(t, engine, "what does this codebase do? (run a build to be sure)"))

	assert.False(t, engine.IsGoverned())
	assert.Empty(t, engine.EvidenceLedger().State(), "incidental build in a chat turn: nothing recorded")
	assert.Empty(t, userNotes(engine, "[evidence]"), "no evidence notes in a chat turn")
	assert.Empty(t, userNotes(engine, "[contract]"), "no contract was declared, none invented")
	assert.Empty(t, engine.renderEvidenceRecord())
	assert.Equal(t, int32(2), requests.Load())
}

// TestEmptyOrRefusedDeclarationDoesNotArm: only a real declaration flips the
// latch — an empty or all-refused one explains the grammar and changes
// nothing, and a bash run after it still records nothing.
func TestEmptyOrRefusedDeclarationDoesNotArm(t *testing.T) {
	engine := newGovernedEngine(t, sequencedBashTool(evidenceGoTestPass, evidenceGoTestFail+"\n(exit error: exit status 1)"))

	msg := runToolCall(t, engine.executor, "contract", `{"checks":[]}`)
	assert.Contains(t, msg.Content, "No checks declared")
	assert.False(t, engine.IsGoverned(), "an empty declaration arms nothing")

	msg = runToolCall(t, engine.executor, "contract", `{"checks":["make-it-pass"]}`)
	assert.Contains(t, msg.Content, "Refused")
	assert.False(t, engine.IsGoverned(), "an all-refused declaration arms nothing")

	// And the session still records nothing afterwards.
	runToolCall(t, engine.executor, "bash", `{"command":"go test ./internal/auth/"}`)
	assert.Empty(t, engine.EvidenceLedger().State(), "still ungoverned, still inert")
}

// TestDeclarationArmsAndSubsequentRunsRecord: the declaration IS the
// classifier — the moment a real contract is held, the session is governed
// and the very next bash run lands in the ledger, regression notes included.
func TestDeclarationArmsAndSubsequentRunsRecord(t *testing.T) {
	engine := newGovernedEngine(t, sequencedBashTool(evidenceGoTestPass, evidenceGoTestFail+"\n(exit error: exit status 1)"))

	runToolCall(t, engine.executor, "contract", `{"checks":["go-build"]}`)
	assert.True(t, engine.IsGoverned(), "a real declaration flips the latch immediately")

	// The very next bash run records — the latch covers the
	// declared-but-never-run window the live ledger check cannot.
	msg := runToolCall(t, engine.executor, "bash", `{"command":"go build ./..."}`)
	assert.NotContains(t, msg.Content, "[evidence]")
	require.Len(t, engine.EvidenceLedger().State(), 1)
	assert.Equal(t, 1, engine.EvidenceLedger().ProtectedCount())

	// Now it regresses: the note rides the tool result.
	msg = runToolCall(t, engine.executor, "bash", `{"command":"go build ./..."}`)
	assert.Contains(t, msg.Content, "[evidence] PROTECTED REGRESSION")
	assert.Contains(t, msg.Content, "go-build")
	require.Len(t, engine.EvidenceLedger().Regressions(), 1)
}

// TestGovernedStickyAcrossTurns: the latch survives the per-turn contract
// reset — turn two declares nothing, yet its bash run is still recorded and
// its regression still notes. That is where the ratchet's value lives.
func TestGovernedStickyAcrossTurns(t *testing.T) {
	server, requests := scriptedLLMServer([]scriptedRound{
		// Turn 1: declare, pass, stop — certified.
		{Tool: "contract", Args: `{"checks":["go-build"]}`},
		{Tool: "bash", Args: `{"command":"go build ./..."}`},
		{Text: "done"},
		// Turn 2 (fresh prompt, no declaration): break it casually.
		{Tool: "bash", Args: `{"command":"go build ./..."}`},
		{Text: "whatever, moving on"},
	})
	defer server.Close()

	engine := newZeroRankEngine(t, server.URL,
		sequencedBashTool(evidenceGoTestPass, evidenceGoTestFail+"\n(exit error: exit status 1)"))

	require.NoError(t, drainLoop(t, engine, "make it build"))
	assert.True(t, engine.IsGoverned())
	require.Equal(t, 1, engine.EvidenceLedger().ProtectedCount())
	require.Len(t, userNotes(engine, "turn certified complete"), 1, "turn 1 certifies")

	require.NoError(t, drainLoop(t, engine, "now casually change whatever you like"))
	// Turn 2 had no contract (reset per turn) and no declaration — yet the
	// regression was recorded and the note rode the tool result.
	require.Len(t, engine.EvidenceLedger().Regressions(), 1, "sticky: turn 2 still records")
	assert.True(t, contextContains(engine, "[evidence] PROTECTED REGRESSION"), "the note rode the tool message")
	assert.True(t, engine.IsGoverned(), "the latch outlives the turn")
	assert.Equal(t, int32(5), requests.Load(), "3 rounds turn 1 + 2 rounds turn 2")
}

// TestNonEmptyLedgerGovernsWithoutDeclaration: the live half of the
// derivation — a ledger with records reads as governed with no declaration.
// This is what makes resume work without a persisted flag, and what lets
// tests (and any direct seeding path) arm the surfaces.
func TestNonEmptyLedgerGovernsWithoutDeclaration(t *testing.T) {
	engine := newGovernedEngine(t, sequencedBashTool(evidenceGoTestFail+"\n(exit error: exit status 1)"))
	require.False(t, engine.IsGoverned())

	engine.EvidenceLedger().Observe("go test ./internal/auth/", evidenceGoTestPass, false, time.Now())
	assert.True(t, engine.IsGoverned(), "a non-empty ledger governs without a declaration")

	// Recording is live: the next bash run lands, note and all.
	msg := runToolCall(t, engine.executor, "bash", `{"command":"go test ./internal/auth/"}`)
	assert.Contains(t, msg.Content, "[evidence] PROTECTED REGRESSION")
	assert.Len(t, engine.EvidenceLedger().Regressions(), 1)
}

// TestResumeDerivesGovernedFromReceipts: a real resume — receipts replayed
// by Ledger.Load govern the new engine from the first turn, no declaration
// needed. The ratchet survives the process boundary.
func TestResumeDerivesGovernedFromReceipts(t *testing.T) {
	dir := t.TempDir()
	sess1, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
	require.NoError(t, err)

	// Turn 1: declare + pass, so receipts land on disk next to the session.
	server, requests := scriptedLLMServer([]scriptedRound{
		{Tool: "contract", Args: `{"checks":["go-build"]}`},
		{Tool: "bash", Args: `{"command":"go build ./..."}`},
		{Text: "done"},
	})
	defer server.Close()

	bash := sequencedBashTool(evidenceGoTestPass)
	engine1, err := NewEngine(llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"}, sess1,
		WithGate(permission.AllowAll{}), WithTools([]tools.Tool{bash}))
	require.NoError(t, err)
	require.NoError(t, drainLoop(t, engine1, "make it build"))
	require.Equal(t, int32(3), requests.Load())
	require.Len(t, userNotes(engine1, "turn certified complete"), 1)
	require.Equal(t, 1, engine1.EvidenceLedger().ProtectedCount())

	// Resume the same session: a fresh engine whose ledger replays the
	// receipts from evidence.jsonl.
	sess2, err := NewSession(WithResumePath(engine1.SessionFile()))
	require.NoError(t, err)
	engine2, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess2,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{sequencedBashTool(evidenceGoTestFail + "\n(exit error: exit status 1)")}))
	require.NoError(t, err)

	assert.Equal(t, engine1.EvidenceLedger().Path(), engine2.EvidenceLedger().Path(), "same session dir, same receipts file")
	assert.True(t, engine2.IsGoverned(), "replayed receipts govern the resumed engine")
	assert.Equal(t, 1, engine2.EvidenceLedger().ProtectedCount(), "the ratchet crossed the process boundary")

	// And recording is live on the resumed engine without any declaration.
	msg := runToolCall(t, engine2.executor, "bash", `{"command":"go build ./..."}`)
	assert.Contains(t, msg.Content, "[evidence] PROTECTED REGRESSION", "resumed session records immediately")
	assert.Len(t, engine2.EvidenceLedger().Regressions(), 1)
}

// TestDeclaredNeverRunResumesUngoverned pins the one accepted thin edge: a
// session that declared a contract but never ran a single check resumes
// ungoverned (the latch lives in engine memory, the receipts file is empty).
// It fails in the safe direction — under-govern, never over-govern: the
// resumed session records nothing until it declares again.
func TestDeclaredNeverRunResumesUngoverned(t *testing.T) {
	dir := t.TempDir()
	sess1, err := NewSession(WithCwd(dir), WithSessionDir(dir), WithPersist(true))
	require.NoError(t, err)

	// Zero-rank disabled so the "done" stands immediately; the turn is a
	// declaration and a stop, nothing ever runs.
	server, requests := scriptedLLMServer([]scriptedRound{
		{Tool: "contract", Args: `{"checks":["go-build"]}`},
		{Text: "done"},
	})
	defer server.Close()

	engine1, err := NewEngine(llm.ModelConfig{Name: "fake", BaseURL: server.URL, APIKey: "x"}, sess1,
		WithGate(permission.AllowAll{}), WithTools([]tools.Tool{sequencedBashTool(evidenceGoTestPass)}),
		WithZeroRankBudget(0))
	require.NoError(t, err)
	require.NoError(t, drainLoop(t, engine1, "just set up the contract"))
	require.Equal(t, int32(2), requests.Load())

	assert.True(t, engine1.IsGoverned(), "in-process the latch holds")
	assert.Empty(t, engine1.EvidenceLedger().State(), "declared but never run: no receipts on disk")

	sess2, err := NewSession(WithResumePath(engine1.SessionFile()))
	require.NoError(t, err)
	engine2, err := NewEngine(llm.ModelConfig{Name: "fake", APIKey: "x"}, sess2,
		WithGate(permission.AllowAll{}),
		WithTools([]tools.Tool{sequencedBashTool(evidenceGoTestPass, evidenceGoTestFail+"\n(exit error: exit status 1)")}))
	require.NoError(t, err)

	assert.False(t, engine2.IsGoverned(), "the thin edge: resumed ungoverned — safe direction")

	// And it stays inert until the session declares again.
	runToolCall(t, engine2.executor, "bash", `{"command":"go build ./..."}`)
	assert.Empty(t, engine2.EvidenceLedger().State(), "resumed session records nothing without a declaration")
}
