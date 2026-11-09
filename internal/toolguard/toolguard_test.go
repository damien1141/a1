package toolguard

import (
        "fmt"
        "strings"
        "testing"
)

func TestExactArgsFailureTiers(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        const args = `{"path":"/etc/hosts"}`
        // Two failures with identical args: warning territory.
        for i := 1; i <= 2; i++ {
                d, _, _ := c.Report("read", args, "open /etc/hosts: no such file", fmt.Sprintf("call-%d", i), true, true)
                if i == 2 && d == nil {
                        t.Fatalf("second identical failure must warn")
                }
                if i < 2 && d != nil {
                        t.Fatalf("first failure must not warn yet, got %+v", d)
                }
        }
        // Before blocks only from the 5th identical failure onward.
        for i := 3; i <= 5; i++ {
                if _, _, _ = c.Report("read", args, "open /etc/hosts: no such file", fmt.Sprintf("call-%d", i), true, true); i < 5 {
                        if d := c.Before("read", args, true); d != nil {
                                t.Fatalf("blocked before the 5th failure (at %d): %+v", i, d)
                        }
                }
        }
        d := c.Before("read", args, true)
        if d == nil || d.Action != "block" {
                t.Fatalf("5th identical failure must block, got %+v", d)
        }
}

func TestProgressResetMakesRetryANewExperiment(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        const args = `{"path":"main.go"}`
        for i := 0; i < 4; i++ {
                _, _, _ = c.Report("read", args, "no such file", fmt.Sprintf("r%d", i), true, true)
        }
        // A successful mutation lands: the next failure of the same signature
        // restarts the streak instead of feeding the block.
        _, _, _ = c.Report("write", `{"path":"main.go"}`, "wrote 12 bytes", "w0", false, false)
        if d := c.Before("read", args, true); d != nil {
                t.Fatalf("a mutation since the last failure resets the exact-args streak: %+v", d)
        }
        // Count continues from the reset (1 after mutation), not from 5.
        d, _, _ := c.Report("read", args, "no such file", "r4", true, true)
        if d != nil {
                t.Fatalf("first failure after a mutation is a new experiment (count 1), got %+v", d)
        }
}

func TestSameToolFailureHaltsNonTolerantToolsOnly(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        // bash: failure-tolerant — 8 distinct-command failures warn but never
        // halt.
        var halted *Decision
        for i := 0; i < 10; i++ {
                d, _, _ := c.Report("bash", fmt.Sprintf(`{"command":"try %d"}`, i), "exit 1", fmt.Sprintf("b%d", i), true, false)
                if d != nil && d.Action == "halt" {
                        halted = d
                }
        }
        if halted != nil {
                t.Fatalf("bash is failure-tolerant: same-tool failures must not halt, got %+v", halted)
        }
        // A non-tolerant tool with varied args halts at the 8th failure.
        c.Reset()
        for i := 0; i < 7; i++ {
                d, _, _ := c.Report("search", fmt.Sprintf(`{"query":"q%d"}`, i), "index locked", fmt.Sprintf("s%d", i), true, true)
                if d != nil && d.Action == "halt" {
                        t.Fatalf("halted at failure %d, before the threshold", i+1)
                }
        }
        d, _, _ := c.Report("search", `{"query":"q7"}`, "index locked", "s7", true, true)
        if d == nil || d.Action != "halt" || d.Code != "same_tool_failure_halt" {
                t.Fatalf("8th same-tool failure must halt, got %+v", d)
        }
        // The halt is once per turn: further failures return nil.
        d, _, _ = c.Report("search", `{"query":"q8"}`, "index locked", "s8", true, true)
        if d != nil && d.Action == "halt" {
                t.Fatalf("halt must fire once per turn")
        }
}

func TestNoProgressWarnsOnIdenticalReadResults(t *testing.T) {
        c := New(Config{Warnings: true})
        const args = `{"path":"go.mod"}`
        var warned bool
        for i := 1; i <= 2; i++ {
                d, _, _ := c.Report("read", args, "module a1\n", fmt.Sprintf("c%d", i), false, true)
                if i == 2 {
                        if warned = d != nil; !warned {
                                t.Fatalf("second identical read result must warn")
                        } else if d.Code != "idempotent_no_progress_warning" {
                                t.Fatalf("wrong code: %s", d.Code)
                        }
                }
        }
        // A different result resets the repeat count.
        d, _, _ := c.Report("read", args, "module a1 // edited\n", "c3", false, true)
        if d != nil {
                t.Fatalf("changed result must reset the no-progress count")
        }
}

func TestIdenticalStreakNoticeAndStub(t *testing.T) {
        c := New(Config{Warnings: true})
        const args = `{"path":"big.txt"}`
        big := strings.Repeat("line of payload\n", 100) // > 512 chars
        // First call: no notice, no stub.
        d, notice, stub := c.Report("read", args, big, "id1", false, true)
        if d != nil || notice != "" || stub != "" {
                t.Fatalf("first call must pass clean: %+v %q %q", d, notice, stub)
        }
        // Second identical call: stub replaces the ≥512-char duplicate.
        _, notice, stub = c.Report("read", args, big, "id2", false, true)
        if notice != "" {
                t.Fatalf("notice fires at the 3rd, not the 2nd: %q", notice)
        }
        if stub == "" || !strings.Contains(stub, "byte-identical") || !strings.Contains(stub, "id1") {
                t.Fatalf("second byte-identical large result must stub, got %q", stub)
        }
        // Third: notice fires.
        _, notice, _ = c.Report("read", args, big, "id3", false, true)
        if !strings.Contains(notice, "3rd consecutive identical call") {
                t.Fatalf("third identical call must notice, got %q", notice)
        }
        // A different result resets the streak.
        _, notice, _ = c.Report("read", args, big+"changed", "id4", false, true)
        if notice != "" {
                t.Fatalf("changed result must reset the streak")
        }
}

func TestStreakExemptForPollersButStubStillApplies(t *testing.T) {
        c := New(Config{Warnings: true})
        const args = `{"id":"job-7"}`
        big := strings.Repeat("status: running\n", 80)
        for i := 1; i <= 5; i++ {
                _, notice, stub := c.Report("agent_wait", args, big, fmt.Sprintf("p%d", i), false, true)
                if notice != "" {
                        t.Fatalf("pollers are exempt from the identical-call notice: %q", notice)
                }
                if i >= 2 && stub == "" {
                        t.Fatalf("poller results still dedupe via the stub (call %d)", i)
                }
        }
}

func TestBatchCycleDetection(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        // A,B,A,B,… with identical args AND results: the consecutive streak
        // resets on every alternation, only the cycle breaker catches it.
        const (
                argsA = `{"path":"a"}`
                argsB = `{"path":"b"}`
        )
        resA, resB := "content a", "content b"
        var sawCycleNotice, sawCycleHalt bool
        for lap := 0; lap < 6; lap++ {
                d, noticeA, _ := c.Report("read", argsA, resA, fmt.Sprintf("a%d", lap), false, true)
                if strings.Contains(noticeA, "cycle") {
                        sawCycleNotice = true
                }
                if d != nil && d.Action == "halt" {
                        sawCycleHalt = true
                }
                d, noticeB, _ := c.Report("read", argsB, resB, fmt.Sprintf("b%d", lap), false, true)
                if strings.Contains(noticeB, "cycle") {
                        sawCycleNotice = true
                }
                if d != nil && d.Action == "halt" {
                        sawCycleHalt = true
                }
        }
        if !sawCycleNotice {
                t.Fatalf("a 3-lap identical A,B cycle must fire the cycle notice")
        }
        if !sawCycleHalt {
                t.Fatalf("with hard stops armed, a 5-lap cycle must halt")
        }
}

func TestSuccessfulIdenticalStreakHaltsWithHardStop(t *testing.T) {
        // The widening (#89069/#100849): replaying a SUCCESSFUL call never
        // touches the failure tiers — only the streak breaker can stop it.
        c := New(Config{Warnings: true, HardStop: true})
        const args = `{"command":"git status"}`
        var halted *Decision
        for i := 1; i <= 6; i++ {
                d, _, _ := c.Report("bash", args, "nothing to commit", fmt.Sprintf("g%d", i), false, false)
                if d != nil && d.Action == "halt" {
                        halted = d
                }
        }
        if halted == nil {
                t.Fatalf("5 identical successful bash calls must halt with hard stops armed")
        }
        if halted.Code != "identical_call_streak_halt" {
                t.Fatalf("wrong halt code: %s", halted.Code)
        }
}

func TestLoopCapsFireRegardlessOfHardStop(t *testing.T) {
        c := New(Config{Warnings: false, HardStop: false})
        for i := 1; i <= defaultLoopCap; i++ {
                if d := c.Before("fetch", fmt.Sprintf(`{"url":"https://x/%d"}`, i), true); d != nil {
                        t.Fatalf("cap must not fire before the limit (call %d)", i)
                }
        }
        d := c.Before("fetch", `{"url":"https://x/51"}`, true)
        if d == nil || d.Action != "block" || d.Code != "loop_fetch_cap" {
                t.Fatalf("the 51st fetch must be blocked even warn-only, got %+v", d)
        }
        // agent_spawn cap, same rule.
        c.Reset()
        for i := 1; i <= defaultLoopCap; i++ {
                if d := c.Before("agent_spawn", fmt.Sprintf(`{"prompt":"p%d"}`, i), false); d != nil {
                        t.Fatalf("spawn cap must not fire before the limit (call %d)", i)
                }
        }
        if d := c.Before("agent_spawn", `{"prompt":"overflow"}`, false); d == nil || d.Code != "loop_subagent_cap" {
                t.Fatalf("the 51st spawn must be blocked, got %+v", d)
        }
}

func TestThrottledRetryFeedsNoExactStreak(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        const args = `{"url":"https://api"}`
        // One ordinary failure, then three throttled failures with the SAME
        // args: the throttled retries must not build the exact-args streak.
        _, _, _ = c.Report("fetch", args, "boom", "f0", true, true)
        for i := 1; i <= 3; i++ {
                d, _, _ := c.Report("fetch", args, "429 too many requests", fmt.Sprintf("f%d", i), true, true)
                if d != nil && d.Code == "repeated_exact_failure_warning" {
                        t.Fatalf("a throttled retry must not carry the exact-args streak warning")
                }
        }
        // But the per-tool count still grows: the same-tool tier can bound a
        // dead endpoint. (fetch is failure-tolerant, so no halt — a warning is
        // the ceiling here.)
        d, _, _ := c.Report("fetch", args, "429 too many requests", "f4", true, true)
        if d == nil {
                t.Fatalf("same-tool warning should eventually fire for a dead endpoint")
        }
}

func TestCanonicalArgsKeyOrderAndNumberFidelity(t *testing.T) {
        a := canonicalArgs(`{"b":1,"a":2}`)
        b := canonicalArgs(`{"a":2,  "b":1}`)
        if a != b {
                t.Fatalf("key order must not change the signature: %q vs %q", a, b)
        }
        // Large integers must survive verbatim (no float rounding).
        big := `{"id":9223372036854775807}`
        if canonicalArgs(big) != big {
                t.Fatalf("large integer mangled: %s", canonicalArgs(big))
        }
}

func TestResetClearsPerTurnState(t *testing.T) {
        c := New(Config{Warnings: true, HardStop: true})
        for i := 0; i < 5; i++ {
                _, _, _ = c.Report("read", `{"path":"x"}`, "err", fmt.Sprintf("c%d", i), true, true)
        }
        if d := c.Before("read", `{"path":"x"}`, true); d == nil {
                t.Fatalf("streak should block before reset")
        }
        c.Reset()
        if d := c.Before("read", `{"path":"x"}`, true); d != nil {
                t.Fatalf("reset must clear the streak: %+v", d)
        }
}

func TestResultHashCanonicalizesJSON(t *testing.T) {
        a := resultHash(`{"ok":true,"items":[1,2]}`)
        b := resultHash(`{ "items": [1, 2], "ok": true }`)
        if a != b {
                t.Fatalf("JSON key order must not change the result hash")
        }
        if resultHash("plain text") != resultHash("plain text") {
                t.Fatalf("raw results must hash stably")
        }
        if resultHash("plain text") == resultHash("plain texs") {
                t.Fatalf("different results must hash differently")
        }
}
