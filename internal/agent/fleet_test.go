package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// SeedTaskContract + applyContractSeed (Bet 04 task contracts).

func TestSeedTaskContractValidatesAndRatchets(t *testing.T) {
	engine := newContractTestEngine(t)

	engine.SeedTaskContract([]string{"go-build", " not-a-check ", "go-test:pkg"}, true)

	// Malformed ids are dropped at seed time; valid ones survive.
	assert.Equal(t, []string{"go-build", "go-test:pkg"}, engine.contractSeed)
	assert.True(t, engine.contractSeedEnforce)

	// The seed also enters the task contract — spawns inherit it.
	assert.Equal(t, []string{"go-build", "go-test:pkg"}, engine.TaskContract())

	// Empty / all-malformed seeds are no-ops (pre-Bet-04 engines unchanged).
	e2 := newContractTestEngine(t)
	e2.SeedTaskContract([]string{"  ", "bogus"}, true)
	assert.Empty(t, e2.contractSeed)
	assert.Empty(t, e2.TaskContract())
}

func TestApplyContractSeedAfterReset(t *testing.T) {
	engine := newContractTestEngine(t)
	engine.SeedTaskContract([]string{"go-build"}, true)

	// A per-Loop reset wipes the turn contract; the seed re-enters it.
	engine.turnContract.reset()
	engine.applyContractSeed()
	assert.Equal(t, []string{"go-build"}, engine.TurnContract())

	// Watch-only mode (explore/review children): watches armed, turn
	// contract NOT held to the inherited finish line.
	e2 := newContractTestEngine(t)
	e2.SeedTaskContract([]string{"go-build"}, false)
	e2.turnContract.reset()
	e2.applyContractSeed()
	assert.Empty(t, e2.TurnContract(), "explore/review children must not be held to the parent's finish line")
	assert.True(t, e2.governed.Load(), "seeding still arms the ledger (evidence flows to the handoff)")
}

func TestContractToolAccumulatesTaskContract(t *testing.T) {
	engine := newContractTestEngine(t)

	msg := runToolCall(t, engine.executor, "contract", `{"checks":["go-build"]}`)
	assert.Contains(t, msg.Content, "Contract held by the harness: 1 check")
	assert.Equal(t, []string{"go-build"}, engine.TaskContract())

	// A fresh turn resets the turn contract but the task contract persists.
	engine.turnContract.reset()
	assert.Empty(t, engine.TurnContract())
	assert.Equal(t, []string{"go-build"}, engine.TaskContract(), "task contract is session-scoped, never reset")

	// Declarations accumulate into the task contract across turns.
	msg = runToolCall(t, engine.executor, "contract", `{"checks":["go-vet"]}`)
	require.Contains(t, msg.Content, "Contract held by the harness: 1 check")
	assert.Equal(t, []string{"go-build", "go-vet"}, engine.TaskContract())
}

// buildHandoff (Bet 04 structured handoff).

func TestBuildHandoffFilesAndChecks(t *testing.T) {
	engine := newContractTestEngine(t)

	// Record evidence for two checks: one passing, one failing.
	engine.evidence.ObserveExit("go test ./...", "ok  \tgithub.com/x/auth\t0.1s\nPASS", 0, false, time.Now())
	engine.evidence.ObserveExit("go build ./...", "undefined: Foo", 1, true, time.Now())

	// Simulate the child's tool transcript: read a.go, write b.go, edit a.go.
	require.NoError(t, engine.session.Append(llmAssistantWithCalls("read", `{"path":"a.go"}`)))
	require.NoError(t, engine.session.Append(llmAssistantWithCalls("write", `{"path":"b.go"}`)))
	require.NoError(t, engine.session.Append(llmAssistantWithCalls("edit", `{"path":"a.go"}`)))

	h := buildHandoff(engine, "did the thing")
	assert.Equal(t, "did the thing", h.Summary)
	assert.Equal(t, []string{"a.go", "b.go"}, h.FilesModified, "edited∪written = modified, sorted")
	assert.Empty(t, h.FilesRead, "a.go was read then modified — modified wins")
	require.Len(t, h.Checks, 2)
	assert.Equal(t, "go-build", h.Checks[0].Check)
	assert.Equal(t, "fail", h.Checks[0].Status)
	assert.Equal(t, "go-test:github.com/x/auth", h.Checks[1].Check)
	assert.Equal(t, "pass", h.Checks[1].Status)
}

func TestBuildHandoffNilSafe(t *testing.T) {
	h := buildHandoff(nil, "plain")
	assert.Equal(t, "plain", h.Summary)
	assert.Empty(t, h.FilesRead)
	assert.Empty(t, h.Checks)

	engine := newContractTestEngine(t) // no evidence, no file ops
	h = buildHandoff(engine, "chat only")
	assert.Empty(t, h.FilesRead)
	assert.Empty(t, h.FilesModified)
	assert.Empty(t, h.Checks)
}

func TestHandoffDetailTruncated(t *testing.T) {
	long := make([]byte, 400)
	for i := range long {
		long[i] = 'x'
	}
	got := truncateHandoffDetail(string(long))
	assert.Len(t, got, 163) // 160 bytes + 3-byte ellipsis rune
	assert.Equal(t, capStringList([]string{"a", "b"}, 1), []string{"a"})
}

// llmAssistantWithCalls builds one assistant message carrying a single
// tool call — the shape compaction's fileop extraction consumes.
func llmAssistantWithCalls(name, args string) llm.Message {
	return llm.Message{
		Role: llm.RoleAssistant,
		ToolCalls: []llm.ToolCall{{
			ID:   "t1",
			Type: "function",
			Function: llm.Function{
				Name:      name,
				Arguments: args,
			},
		}},
	}
}
