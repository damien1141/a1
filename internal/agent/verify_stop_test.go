package agent

import (
        "context"
        "encoding/json"
        "errors"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/permission"
        "github.com/damien1141/a1/internal/tools"
)

// --- mutation facts (executor level) --------------------------------------

// stubWriteTool is a write-shaped mutating tool: not read-only, with a
// "path" argument — the shape recordMutation keys on.
func stubWriteTool() tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "write"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: "ok"}, nil
                },
        }
}

// failingWriteTool reports failure: a failed write mutated nothing.
func failingWriteTool() tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "write"},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{}, errors.New("disk full")
                },
        }
}

// stubReadTool is read-only: never a mutation, whatever its arguments.
func stubReadTool() tools.Tool {
        return tools.Tool{
                Definition: llm.ToolDefinition{Name: "read", Readable: true},
                Run: func(context.Context, json.RawMessage) (tools.Result, error) {
                        return tools.Result{Content: "contents"}, nil
                },
        }
}

func TestRecordMutationCollectsFileEdits(t *testing.T) {
        engine := newContractTestEngine(t, WithTools([]tools.Tool{stubWriteTool(), stubReadTool()}))

        // A successful write with a path argument is a mutation fact.
        runToolCall(t, engine.executor, "write", `{"path": " main.go "}`)
        facts := engine.executor.DrainMutations()
        require.Len(t, facts, 1)
        assert.Equal(t, "write", facts[0].Tool)
        assert.Equal(t, "main.go", facts[0].Path, "the path is trimmed")
        assert.False(t, facts[0].At.IsZero())
        assert.Empty(t, engine.executor.DrainMutations(), "drain clears")

        // Read-only tools never mutate.
        runToolCall(t, engine.executor, "read", `{"path": "main.go"}`)
        assert.Empty(t, engine.executor.DrainMutations())

        // A mutating tool without a path argument (bash-shaped) is
        // invisible: its file effects cannot be attributed without
        // semantic analysis, and counting every such run would fire the
        // gate on read-only turns.
        runToolCall(t, engine.executor, "write", `{"content": "x"}`)
        assert.Empty(t, engine.executor.DrainMutations())

        // Malformed arguments never ran, so they mutated nothing.
        runToolCall(t, engine.executor, "write", `{invalid`)
        assert.Empty(t, engine.executor.DrainMutations())
}

func TestRecordMutationIgnoresFailures(t *testing.T) {
        engine := newContractTestEngine(t, WithTools([]tools.Tool{failingWriteTool()}))
        runToolCall(t, engine.executor, "write", `{"path": "main.go"}`)
        assert.Empty(t, engine.executor.DrainMutations(), "a failed write mutated nothing")
}

// --- turn state and the prose exemption -----------------------------------

func TestTurnVerifyStateFoldAndCodePaths(t *testing.T) {
        var tv turnVerifyState
        t1 := time.Now()
        t2 := t1.Add(time.Second)

        tv.fold([]MutationFact{{Tool: "write", Path: "main.go", At: t1}})
        tv.fold([]MutationFact{
                {Tool: "write", Path: "README.md", At: t2},
                {Tool: "edit", Path: "main.go", At: t2},
        })

        assert.Equal(t, t2, tv.lastAt, "the newest mutation stamps the clock")
        assert.Equal(t, []string{"main.go"}, tv.codePaths(),
                "prose paths are exempt; code paths are sorted and deduplicated")

        tv.reset()
        assert.Empty(t, tv.codePaths())
        assert.True(t, tv.lastAt.IsZero())
}

func TestIsNonCodePath(t *testing.T) {
        for _, p := range []string{
                "SKILL.md", "docs/README.markdown", "NOTES.TXT", "notes.text",
                "spec.rst", "doc.adoc", "book.asciidoc", "notes.org",
                "run.log", "table.csv", "data.tsv", "post.mdx",
        } {
                assert.True(t, isNonCodePath(p), "%s is prose/data", p)
        }
        for _, p := range []string{
                "LICENSE", "LICENCE", "NOTICE", "AUTHORS", "CONTRIBUTORS",
                "CHANGELOG", "sub/dir/CODEOWNERS",
        } {
                assert.True(t, isNonCodePath(p), "%s is an extension-less prose filename", p)
        }
        for _, p := range []string{
                "main.go", "component.tsx", "Makefile", "scripts/run.sh",
                "style.css", "go.mod",
        } {
                assert.False(t, isNonCodePath(p), "%s is code", p)
        }
}

// --- the gate (loop level) --------------------------------------------------

// verifyGoTestPass is a classifiable passing `go test` output.
const verifyGoTestPass = "ok  \tgithub.com/x/y\t0.001s\n"

// verifyGoTestFail is a classifiable failing one.
const verifyGoTestFail = "FAIL\tgithub.com/x/y [build failed]\n(exit error: exit status 1)"

// newVerifyEngine builds an engine with a full tool set against a scripted
// server, mirroring newZeroRankEngine.
func newVerifyEngine(t *testing.T, serverURL string, ts []tools.Tool, opts ...EngineOption) *Engine {
        t.Helper()
        sess, err := NewSession(WithCwd(t.TempDir()))
        require.NoError(t, err)
        all := append([]EngineOption{
                WithGate(permission.AllowAll{}),
                WithTools(ts),
        }, opts...)
        engine, err := NewEngine(llm.ModelConfig{Name: "fake", BaseURL: serverURL, APIKey: "x"}, sess, all...)
        require.NoError(t, err)
        return engine
}

// TestVerifyStopBlocksUntestedEdit is the centerpiece: a governed session,
// a code edit, an attempted finish — the harness turns the answer into an
// interim row and nudges a verification round; the model verifies, stops
// again, and the finish stands.
func TestVerifyStopBlocksUntestedEdit(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Text: "done"},                                       // blocked: nudge 1
                {Tool: "bash", Args: `{"command": "go test ./..."}`}, // verifies
                {Text: "done"},                                       // fresh: ends
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestPass), stubWriteTool()})
        // A prior green run governs the session — and goes stale the moment
        // this turn edits code.
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "ship it"))

        notes := userNotes(engine, "[verify] the turn cannot end yet")
        require.Len(t, notes, 1, "exactly one verify nudge")
        assert.Contains(t, notes[0], "verification nudge 1 of 2")
        assert.Contains(t, notes[0], "no check has passed since the edits")
        assert.Contains(t, notes[0], "main.go")
        assert.Equal(t, int32(4), requests.Load(),
                "four rounds: edit, blocked stop, verify, clean stop")
}

// TestVerifyStopFreshEvidenceEndsFreely: a check that ran AFTER the edits
// is fresh evidence — the finish stands without a nudge.
func TestVerifyStopFreshEvidenceEndsFreely(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Tool: "bash", Args: `{"command": "go test ./..."}`}, // passes after the edit
                {Text: "done"},
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestPass), stubWriteTool()})
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "ship it"))
        assert.Empty(t, userNotes(engine, "[verify]"),
                "a check passed after the edits — the finish stands")
        assert.Equal(t, int32(3), requests.Load())
}

// TestVerifyStopNudgeCapLetsTheAnswerStand: a model that twice declines to
// verify keeps its answer — the gate is a brake, not a wall.
func TestVerifyStopNudgeCapLetsTheAnswerStand(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Text: "done"}, // nudge 1
                {Text: "done"}, // nudge 2
                {Text: "done"}, // cap: the answer stands
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestPass), stubWriteTool()})
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "ship it"))

        notes := userNotes(engine, "[verify] the turn cannot end yet")
        require.Len(t, notes, 2, "the budget is two nudges, then the answer stands")
        assert.Contains(t, notes[0], "verification nudge 1 of 2")
        assert.Contains(t, notes[1], "verification nudge 2 of 2")
        assert.Equal(t, int32(4), requests.Load())
}

// TestVerifyStopProseOnlyTurnExempt: a turn that touched only markdown has
// nothing to verify.
func TestVerifyStopProseOnlyTurnExempt(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "README.md"}`},
                {Text: "done"},
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestPass), stubWriteTool()})
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "document it"))
        assert.Empty(t, userNotes(engine, "[verify]"), "prose-only turns are exempt")
        assert.Equal(t, int32(2), requests.Load())
}

// TestVerifyStopUngovernedSessionExempt: no contract, empty ledger — the
// chat-mode ergonomics latch keeps the gate silent for casual sessions.
func TestVerifyStopUngovernedSessionExempt(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Text: "done"},
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestPass), stubWriteTool()})
        require.False(t, engine.IsGoverned(), "a fresh session starts ungoverned")

        require.NoError(t, drainLoop(t, engine, "fix the typo"))
        assert.Empty(t, userNotes(engine, "[verify]"), "ungoverned sessions get no verification nudges")
        assert.Equal(t, int32(2), requests.Load())
}

// TestVerifyStopContractDeclaredDefersToZeroRank: a declared finish line is
// the zero-rank stop's to judge; the verify gate stays silent so the model
// is never nagged by both.
func TestVerifyStopContractDeclaredDefersToZeroRank(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "contract", Args: `{"checks":["go-build"]}`},
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Text: "done"},                                      // zero-rank residual
                {Tool: "bash", Args: `{"command":"go build ./..."}`}, // passes
                {Text: "done"},                                      // certified
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(""), stubWriteTool()})

        require.NoError(t, drainLoop(t, engine, "make it build"))

        assert.Empty(t, userNotes(engine, "[verify]"),
                "the declared contract owns the finish line")
        require.Len(t, userNotes(engine, "[contract] turn certified complete"), 1)
        assert.Equal(t, int32(5), requests.Load())
}

// TestVerifyStopFailingCheckFiresWithResidualList: a currently-failing
// check is stronger than staleness — the nudge lists it.
func TestVerifyStopFailingCheckFiresWithResidualList(t *testing.T) {
        server, requests := scriptedLLMServer([]scriptedRound{
                {Tool: "write", Args: `{"path": "main.go"}`},
                {Text: "done"},                                      // nudge 1: stale
                {Tool: "bash", Args: `{"command":"go test ./..."}`}, // fails
                {Text: "done"},                                      // nudge 2: failing
                {Text: "done"},                                      // cap: stands
        })
        defer server.Close()

        engine := newVerifyEngine(t, server.URL,
                []tools.Tool{sequencedBashTool(verifyGoTestFail), stubWriteTool()})
        engine.EvidenceLedger().Observe("go build ./...", "", false, time.Now())

        require.NoError(t, drainLoop(t, engine, "ship it"))

        notes := userNotes(engine, "[verify] the turn cannot end yet")
        require.Len(t, notes, 2)
        assert.Contains(t, notes[0], "no check has passed since the edits")
        assert.Contains(t, notes[1], "Currently failing checks")
        assert.Contains(t, notes[1], "go-test:github.com/x/y")
        assert.Equal(t, int32(5), requests.Load())
}
