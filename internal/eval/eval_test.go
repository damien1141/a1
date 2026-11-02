package eval

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/evidence"
	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
	"github.com/damien1141/a1/internal/skillstats"
	"github.com/damien1141/a1/internal/telemetry"
)

// writeSession builds a hermetic session on disk and returns its manager.
// Entries land in the JSONL file exactly like a live run would flush them.
func writeSession(t *testing.T, entries ...llm.Message) *session.Manager {
	t.Helper()
	dir := t.TempDir()
	m, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
	require.NoError(t, err)
	for _, msg := range entries {
		_, err := m.Append(msg)
		require.NoError(t, err)
	}
	return m
}

// appendReceipts appends evidence receipts next to a session file, one JSON
// line per record — the on-disk format the engine's file-backed ledger
// writes.
func appendReceipts(t *testing.T, sessionFile string, records ...evidence.Record) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(filepath.Dir(sessionFile), "evidence.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, rec := range records {
		require.NoError(t, enc.Encode(rec))
	}
}

func contractCall(id string, checks ...string) llm.Message {
	args, err := json.Marshal(map[string]any{"checks": checks})
	if err != nil {
		panic(err)
	}
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: llm.Function{Name: "contract", Arguments: string(args)},
		}},
	}
}

func readCall(id, path string) llm.Message {
	args, err := json.Marshal(map[string]any{"path": path})
	if err != nil {
		panic(err)
	}
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:       id,
			Type:     "function",
			Function: llm.Function{Name: "read", Arguments: string(args)},
		}},
	}
}

func assistantWithUsage(prompt, completion, total int) llm.Message {
	return llm.Message{
		Role:    llm.RoleAssistant,
		Content: "done",
		Usage: llm.Usage{
			PromptTokens:     prompt,
			CompletionTokens: completion,
			TotalTokens:      total,
		},
	}
}

const testPkg = "go-test:example.com/golden/greeter"

func TestScoreSessionRoundsToGreen(t *testing.T) {
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "make the greeter tests pass"},
		contractCall("c1", "go-build", testPkg),
	)
	appendReceipts(t, m.File(),
		// Round 1: everything failing.
		evidence.Record{Check: "go-build", Status: evidence.Fail, At: base, Detail: "undefined: Hello", Command: "go build ./..."},
		evidence.Record{Check: testPkg, Status: evidence.Fail, At: base, Detail: "Hello empty", Command: "go build ./..."},
		// Round 2: build green, tests still red.
		evidence.Record{Check: "go-build", Status: evidence.Pass, At: base.Add(time.Minute), Command: "go build ./..."},
		evidence.Record{Check: testPkg, Status: evidence.Fail, At: base.Add(time.Minute), Detail: "still failing", Command: "go test ./..."},
		// Round 3: all green.
		evidence.Record{Check: "go-build", Status: evidence.Pass, At: base.Add(2 * time.Minute), Command: "go build ./..."},
		evidence.Record{Check: testPkg, Status: evidence.Pass, At: base.Add(2 * time.Minute), Command: "go test ./..."},
	)

	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.True(t, res.Green, "contract satisfied after round 3")
	assert.Equal(t, 3, res.RoundsToGreen, "green on the third verification round")
	assert.Equal(t, 3, res.VerificationRounds)
	assert.Equal(t, []string{"go-build", testPkg}, res.Contract)
	assert.Equal(t, 1, res.Turns, "the one user prompt")
	assert.Equal(t, float64(0), res.RollbackRate)
}

func TestScoreSessionNeverGreen(t *testing.T) {
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "fix the build"},
		contractCall("c1", "go-build"),
	)
	appendReceipts(t, m.File(),
		evidence.Record{Check: "go-build", Status: evidence.Fail, At: base, Command: "go build ./..."},
		evidence.Record{Check: "go-build", Status: evidence.Fail, At: base.Add(time.Minute), Command: "go build ./..."},
	)

	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.False(t, res.Green)
	assert.Equal(t, 2, res.VerificationRounds)
	assert.Equal(t, 2, res.RoundsToGreen, "never green reports the total")
}

func TestScoreSessionRollbackRate(t *testing.T) {
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "fix it"},
		contractCall("c1", "go-build", "go-vet"),
	)
	appendReceipts(t, m.File(),
		// Round 1: build green, vet still red — not green yet.
		evidence.Record{Check: "go-build", Status: evidence.Pass, At: base, Command: "go build ./..."},
		evidence.Record{Check: "go-vet", Status: evidence.Fail, At: base, Detail: "unused", Command: "go vet ./..."},
		// Round 2: vet goes green — first green round.
		evidence.Record{Check: "go-vet", Status: evidence.Pass, At: base.Add(time.Minute), Command: "go vet ./..."},
		// Round 3: vet regresses (it was protected), then the harness rolls
		// the candidate back and reverts the ledger — the receipt shape
		// RevertTo writes.
		evidence.Record{Check: "go-vet", Status: evidence.Fail, At: base.Add(2 * time.Minute), Command: "go vet ./..."},
		evidence.Record{Check: "go-vet", Status: evidence.Pass, At: base.Add(3 * time.Minute), Command: "harness:candidate-rollback"},
		// Round 4: green again after the redo.
		evidence.Record{Check: "go-vet", Status: evidence.Pass, At: base.Add(4 * time.Minute), Command: "go vet ./..."},
	)

	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.Equal(t, 1, res.Rollbacks, "one harness reversion receipt")
	assert.Equal(t, 4, res.VerificationRounds)
	assert.InDelta(t, 0.25, res.RollbackRate, 1e-9)
	assert.True(t, res.Green)
	assert.Equal(t, 2, res.RoundsToGreen, "first green round is 2; later thrash does not rewind it")
}

func TestScoreSessionStallAndGovernedTurns(t *testing.T) {
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "first prompt"},
		contractCall("c1", "go-build"),
		llm.Message{Role: llm.RoleUser, Content: "System note: [contract] the turn cannot end yet — declared done-criteria are not all green (verification round 1 of 2)"},
		// Chat turn that is NOT governed (before the contract)... it is
		// second in the tree, so it counts: governed latch fired in turn 1.
		llm.Message{Role: llm.RoleUser, Content: "System note: [evidence] stopping this turn — 3 consecutive verification rounds without improvement."},
	)
	appendReceipts(t, m.File(),
		evidence.Record{Check: "go-build", Status: evidence.Fail, At: base, Command: "go build ./..."},
	)

	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.Equal(t, 1, res.StallStops, "one [evidence] stop note")
	assert.Equal(t, 1, res.Turns, "system notes are not turns")
	assert.Equal(t, 1, res.GovernedTurns, "the latch fired during turn 1")
	assert.InDelta(t, 1.0, res.StallRate, 1e-9)
}

func TestScoreSessionTokensAndCompactions(t *testing.T) {
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "do the thing"},
		assistantWithUsage(100, 50, 150),
		assistantWithUsage(200, 80, 280),
	)
	_, err := m.AppendCompaction(session.Compaction{Summary: "compacted", TokensBefore: 1000})
	require.NoError(t, err)
	_, err = m.AppendCompaction(session.Compaction{Summary: "again", TokensBefore: 900})
	require.NoError(t, err)

	res, err := ScoreSession(m, "test-model")
	require.NoError(t, err)
	assert.Equal(t, int64(430), res.TotalTokens, "150 + 280")
	assert.Equal(t, map[string]int64{"test-model": 430}, res.TokensByModel)
	assert.Equal(t, 2, res.Compactions)
	require.Len(t, res.TurnRows(), 2, "one turn row per reported usage")
	assert.Equal(t, "test-model", res.TurnRows()[0].Model)
	assert.Equal(t, 150, res.TurnRows()[0].TotalTokens)
}

func TestScoreSessionTokensDisjointBuckets(t *testing.T) {
	// Some providers report buckets without a total; the scorer sums the
	// disjoint buckets instead of reading zero.
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "hi"},
		llm.Message{
			Role:    llm.RoleAssistant,
			Content: "done",
			Usage: llm.Usage{
				PromptTokens:        7,
				CompletionTokens:    3,
				PromptTokensDetails: &llm.PromptTokensDetails{CachedTokens: 5},
			},
		},
	)
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.Equal(t, int64(15), res.TotalTokens, "7 uncached + 5 cached + 3 completion")
}

func TestScoreSessionSkillsPicked(t *testing.T) {
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "use the go-test skill"},
		readCall("r1", "/home/u/.a1/skills/go-test/SKILL.md"),
		readCall("r2", "/home/u/.a1/skills/go-test/SKILL.md"), // dedup
		readCall("r3", "/home/u/project/main.go"),             // not a skill
	)
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"go-test"}, res.SkillsPicked)
}

func TestScoreSessionIgnoresInvalidContractIds(t *testing.T) {
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "task"},
		contractCall("c1", "go-build", "not a check id", "make-it-pass"),
	)
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"go-build"}, res.Contract, "malformed ids are refused at declare time")
}

func TestScoreSessionNoReceipts(t *testing.T) {
	m := writeSession(t, llm.Message{Role: llm.RoleUser, Content: "just chatting"})
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	assert.False(t, res.Green)
	assert.Equal(t, 0, res.VerificationRounds)
	assert.Equal(t, float64(0), res.RollbackRate)
	assert.Equal(t, float64(0), res.StallRate, "ungoverned sessions have no stall denominator")
}

// TestPersistMaterializesTurnsAndSkillOutcomes covers the scorer's write
// step against a real SQLite handle (the same driver the vault uses).
func TestPersistMaterializesTurnsAndSkillOutcomes(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, telemetry.EnsureSchema(db))
	require.NoError(t, skillstats.EnsureSchema(db))

	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "make it pass"},
		readCall("r1", "/skills/go-test/SKILL.md"),
		contractCall("c1", "go-build"),
		assistantWithUsage(100, 50, 150),
	)
	appendReceipts(t, m.File(),
		evidence.Record{Check: "go-build", Status: evidence.Pass, At: base, Command: "go build ./..."},
	)

	res, err := ScoreSession(m, "test-model")
	require.NoError(t, err)
	require.True(t, res.Green)
	require.NoError(t, Persist(res, db))

	// Turns materialized and idempotent under a re-run; the re-run must
	// also not double-credit the skill outcome (the idempotency guard
	// keys on the materialized turn rows).
	turns, err := telemetry.TurnSummary(db)
	require.NoError(t, err)
	require.Len(t, turns, 1)
	assert.Equal(t, 150, int(turns[0].TotalTok))
	require.NoError(t, Persist(res, db))
	turns, err = telemetry.TurnSummary(db)
	require.NoError(t, err)
	assert.Len(t, turns, 1, "re-materializing replaces, never duplicates")

	// Green session with a picked skill credits the bandit a win.
	rows, err := skillstats.Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "go-test", rows[0].Skill)
	assert.Equal(t, 1, rows[0].Wins)
	assert.Equal(t, 1, rows[0].Picks, "outcome without a live pick still keeps picks >= wins+losses")
}

func TestPersistNotGreenCreditsLoss(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer db.Close()

	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "fail politely"},
		readCall("r1", "/skills/go-test/SKILL.md"),
		contractCall("c1", "go-build"),
	)
	appendReceipts(t, m.File(),
		evidence.Record{Check: "go-build", Status: evidence.Fail, At: base, Command: "go build ./..."},
	)
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	require.False(t, res.Green)
	require.NoError(t, Persist(res, db))

	rows, err := skillstats.Stats(db)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 1, rows[0].Losses)
}

func TestPersistUngovernedCreditsNothing(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "t.db"))
	require.NoError(t, err)
	defer db.Close()

	m := writeSession(t,
		llm.Message{Role: llm.RoleUser, Content: "read the docs"},
		readCall("r1", "/skills/go-test/SKILL.md"),
	)
	res, err := ScoreSession(m, "")
	require.NoError(t, err)
	require.NoError(t, Persist(res, db))

	rows, err := skillstats.Stats(db)
	require.NoError(t, err)
	assert.Empty(t, rows, "no finish line: nothing to credit or blame")
}

func TestGroupReceiptsZeroTimestamps(t *testing.T) {
	// Hand-written fixtures with no At are indistinguishable from each
	// other, so they group together — one round, not one mega-round per
	// receipt and not one receipt per round.
	records := []evidence.Record{
		{Check: "a", Status: evidence.Pass},
		{Check: "b", Status: evidence.Pass},
		{Check: "a", Status: evidence.Fail},
	}
	groups := groupReceipts(records)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0].records, 3)

	// Distinct At values split rounds; equal At values share one.
	base := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	groups = groupReceipts([]evidence.Record{
		{Check: "a", Status: evidence.Pass, At: base},
		{Check: "b", Status: evidence.Pass, At: base},
		{Check: "a", Status: evidence.Fail, At: base.Add(time.Minute)},
	})
	require.Len(t, groups, 2, "an At change is a round boundary")
	assert.Len(t, groups[0].records, 2)
	assert.Len(t, groups[1].records, 1)
}

func TestSelectBuiltinTools(t *testing.T) {
	all, err := SelectBuiltinTools("")
	require.NoError(t, err)
	assert.Empty(t, all, "empty list = engine defaults")

	subset, err := SelectBuiltinTools("bash, read")
	require.NoError(t, err)
	require.Len(t, subset, 2)
	assert.Equal(t, "bash", subset[0].Definition.Name)
	assert.Equal(t, "read", subset[1].Definition.Name)

	_, err = SelectBuiltinTools("bash, no-such-tool")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-tool")
}

func TestSummarize(t *testing.T) {
	totals := Summarize([]EvalResult{
		{Green: true, RoundsToGreen: 2, VerificationRounds: 2, TotalTokens: 100},
		{Green: false, VerificationRounds: 3, Rollbacks: 1, TotalTokens: 50},
	})
	assert.Equal(t, 2, totals.Sessions)
	assert.Equal(t, 1, totals.Green)
	assert.Equal(t, 5, totals.VerificationRounds)
	assert.Equal(t, 2, totals.RoundsToGreen)
	assert.InDelta(t, 2.0, totals.MeanRoundsToGreen, 1e-9)
	assert.Equal(t, int64(150), totals.TotalTokens)
}
