package toolguard

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- negative ----------

// Malformed and hostile argument strings must never panic the guard: they
// key by their trimmed raw text, so an identical garbage replay is still a
// loop the exact-args tier catches, while object-shaped JSON canonicalizes
// (key order, whitespace, duplicate keys) and everything else does not.
func TestMalformedHostileArgs_Negative(t *testing.T) {
	t.Run("identical garbage replays are still loops", func(t *testing.T) {
		c := New(Config{Warnings: true, HardStop: true})
		const garbage = "not json at all {{{"
		for i := 1; i <= 5; i++ {
			d, _, _ := c.Report("read", garbage, "open: no such file", fmt.Sprintf("g%d", i), true, true)
			if i == 2 {
				require.NotNil(t, d, "the second identical failure must warn")
				assert.Equal(t, "warn", d.Action)
				assert.Equal(t, "repeated_exact_failure_warning", d.Code)
				assert.Equal(t, 2, d.Count)
			}
		}
		d := c.Before("read", garbage, true)
		require.NotNil(t, d, "five identical garbage-arg failures must block")
		assert.Equal(t, "block", d.Action)
		assert.Equal(t, "repeated_exact_failure_block", d.Code)
		assert.Equal(t, 5, d.Count)
		assert.Contains(t, d.Message, "failed 5 times with identical arguments")
	})

	t.Run("non-object JSON keys raw, so spacing still mints signatures", func(t *testing.T) {
		// A truncated object and a JSON array both fail map decoding, so
		// they key by raw text: only objects are canonicalized.
		c := New(Config{Warnings: true})
		d, _, _ := c.Report("read", `{"a":`, "err", "t1", true, true)
		assert.Nil(t, d, "first failure never warns")
		d, _, _ = c.Report("read", `{"a":`, "err", "t2", true, true)
		require.NotNil(t, d, "byte-identical truncated args are the same call")
		assert.Equal(t, "repeated_exact_failure_warning", d.Code)

		c2 := New(Config{Warnings: true})
		d, _, _ = c2.Report("read", `[1,2,3]`, "err", "a1", true, true)
		assert.Nil(t, d)
		d, _, _ = c2.Report("read", `[1, 2, 3]`, "err", "a2", true, true)
		assert.Nil(t, d, "array args do not canonicalize: spacing still mints a new signature")

		// Every raw shape is still a stable key: identical repeats warn.
		for _, raw := range []string{`"quoted"`, `123`, `null`, `true`} {
			c3 := New(Config{Warnings: true})
			d, _, _ = c3.Report("read", raw, "err", "r1", true, true)
			assert.Nil(t, d, "first failure of %s never warns", raw)
			d, _, _ = c3.Report("read", raw, "err", "r2", true, true)
			require.NotNil(t, d, "identical %s args are the same call", raw)
			assert.Equal(t, "repeated_exact_failure_warning", d.Code)
		}
	})

	t.Run("empty and blank args are the same call", func(t *testing.T) {
		c := New(Config{Warnings: true})
		_, _, _ = c.Report("read", "", "err", "e1", true, true)
		d, _, _ := c.Report("read", "   ", "err", "e2", true, true)
		require.NotNil(t, d, "both canonicalize to {}")
		assert.Equal(t, "repeated_exact_failure_warning", d.Code)
		assert.Equal(t, 2, d.Count)
	})

	t.Run("duplicate keys collapse to the last value", func(t *testing.T) {
		c := New(Config{Warnings: true})
		_, _, _ = c.Report("edit", `{"a":1,"a":2}`, "err", "d1", true, true)
		d, _, _ := c.Report("edit", `{"a":2}`, "err", "d2", true, true)
		require.NotNil(t, d, "last-key-wins makes these the same signature")
		assert.Equal(t, 2, d.Count)

		c2 := New(Config{Warnings: true})
		_, _, _ = c2.Report("edit", `{"a":1,"a":2}`, "err", "d3", true, true)
		d, _, _ = c2.Report("edit", `{"a":1}`, "err", "d4", true, true)
		assert.Nil(t, d, `the collapsed signature is {"a":2}, not {"a":1}`)
	})

	t.Run("adjacent int64s stay distinct", func(t *testing.T) {
		// int64 neighbors that float64 would collide: the signature keeps
		// the literal verbatim, so they are different calls.
		c := New(Config{Warnings: true})
		_, _, _ = c.Report("read", `{"id":9223372036854775807}`, "err", "i1", true, true)
		d, _, _ := c.Report("read", `{"id":9223372036854775808}`, "err", "i2", true, true)
		assert.Nil(t, d, "adjacent int64s are distinct calls")

		c2 := New(Config{Warnings: true})
		_, _, _ = c2.Report("read", `{"id":9223372036854775807}`, "err", "j1", true, true)
		d, _, _ = c2.Report("read", `{"id":9223372036854775807}`, "err", "j2", true, true)
		require.NotNil(t, d, "the identical literal is the same call")
		assert.Equal(t, 2, d.Count)
	})
}

// Unarmed tiers: with Warnings and HardStop both off, no Report ever yields
// a decision — but notices and stubs are context hygiene, not warnings, so
// they still fire; only the loop caps can still block in Before.
func TestUnarmedTiers_Negative(t *testing.T) {
	t.Run("zero config never decides, but still notices and stubs", func(t *testing.T) {
		c := New(Config{})
		big := strings.Repeat("x", 600)
		for i := 1; i <= 6; i++ {
			d, notice, stub := c.Report("read", `{"path":"a"}`, big, fmt.Sprintf("c%d", i), false, true)
			assert.Nil(t, d, "step %d: no tier is armed", i)
			if i >= 3 {
				assert.NotEmpty(t, notice, "step %d: the identical-call notice is not gated on Warnings", i)
			}
			if i >= 2 {
				assert.NotEmpty(t, stub, "step %d: the byte-identical stub is not gated on Warnings", i)
			}
		}
		for i := 1; i <= 10; i++ {
			d, _, _ := c.Report("search", `{"q":"x"}`, "index locked", fmt.Sprintf("f%d", i), true, true)
			assert.Nil(t, d, "failure %d: no tier is armed", i)
		}
		for i := 1; i <= 20; i++ {
			assert.Nil(t, c.Before("search", `{"q":"x"}`, true), "check %d: only loop caps block when disarmed", i)
		}
	})

	t.Run("warnings-only never blocks", func(t *testing.T) {
		c := New(Config{Warnings: true})
		for i := 1; i <= 100; i++ {
			d, _, _ := c.Report("read", `{"path":"a"}`, "err", fmt.Sprintf("w%d", i), true, true)
			if i >= 2 {
				require.NotNil(t, d, "step %d: warnings still fire", i)
				assert.Equal(t, "warn", d.Action)
			}
		}
		assert.Nil(t, c.Before("read", `{"path":"a"}`, true), "the exact-args block is HardStop-gated")
	})

	t.Run("hard-stop-only never warns, but the streak breaker still halts", func(t *testing.T) {
		c := New(Config{HardStop: true})
		for i := 1; i <= 4; i++ {
			d, _, _ := c.Report("read", `{"path":"a"}`, "err", fmt.Sprintf("h%d", i), true, true)
			assert.Nil(t, d, "step %d: warnings are off and no halt threshold is reached", i)
		}
		// The 5th identical call crosses the streak breaker's threshold:
		// no warning tier fired, yet the turn still halts.
		d, _, _ := c.Report("read", `{"path":"a"}`, "err", "h5", true, true)
		require.NotNil(t, d)
		assert.Equal(t, "halt", d.Action)
		assert.Equal(t, "identical_call_streak_halt", d.Code)
		assert.Equal(t, 5, d.Count)

		bd := c.Before("read", `{"path":"a"}`, true)
		require.NotNil(t, bd, "blocks still fire with HardStop armed")
		assert.Equal(t, "repeated_exact_failure_block", bd.Code)
		assert.Equal(t, 5, bd.Count)
	})
}

// The throttle exemption only reads the first 500 runes of a FAILED result
// and only applies to failures: a marker past the head builds the exact-args
// streak, and a successful result that mentions rate limits is not exempt.
func TestThrottleDetectionBoundary_Negative(t *testing.T) {
	t.Run("marker inside the 500-rune head exempts the exact streak", func(t *testing.T) {
		c := New(Config{Warnings: true})
		within := strings.Repeat("x", 497) + "429" // runes 498-500
		for i := 1; i <= 4; i++ {
			d, _, _ := c.Report("fetch", `{"url":"https://a"}`, within, fmt.Sprintf("t%d", i), true, true)
			if d != nil {
				assert.NotEqual(t, "repeated_exact_failure_warning", d.Code,
					"a throttled retry must not carry the exact-args warning")
				assert.Equal(t, "same_tool_failure_warning", d.Code, "the per-tool count still grows")
			}
		}
	})

	t.Run("marker beyond the head is not a throttle", func(t *testing.T) {
		c := New(Config{Warnings: true})
		beyond := strings.Repeat("x", 500) + "too many requests"
		_, _, _ = c.Report("fetch", `{"url":"https://a"}`, beyond, "b1", true, true)
		d, _, _ := c.Report("fetch", `{"url":"https://a"}`, beyond, "b2", true, true)
		require.NotNil(t, d, "the 501st rune is past the head: this is a plain replay")
		assert.Equal(t, "repeated_exact_failure_warning", d.Code)
		assert.Equal(t, 2, d.Count)
	})

	t.Run("detection is case-insensitive across markers", func(t *testing.T) {
		for _, marker := range []string{"RATE LIMIT", "Too Many Requests", "Throttled by provider", "HTTP 429"} {
			c := New(Config{Warnings: true})
			res := "prefix " + marker + " suffix"
			_, _, _ = c.Report("fetch", `{"url":"https://a"}`, res, "m1", true, true)
			d, _, _ := c.Report("fetch", `{"url":"https://a"}`, res, "m2", true, true)
			assert.Nil(t, d, "marker %q exempts the exact-args streak", marker)
		}
	})

	t.Run("successful rate-limit chatter is not exempt", func(t *testing.T) {
		c := New(Config{Warnings: true})
		const res = "rate limit policy: 100 requests per minute"
		_, _, _ = c.Report("read", `{"path":"pricing"}`, res, "s1", false, true)
		d, _, _ := c.Report("read", `{"path":"pricing"}`, res, "s2", false, true)
		require.NotNil(t, d, "throttling only masks failed calls")
		assert.Equal(t, "idempotent_no_progress_warning", d.Code)
	})
}

// The dedup stub cuts in at exactly 512 result BYTES on the second identical
// result, previews 120 RUNES of the canonical args without splitting a
// rune, and never stubs a failed call.
func TestStubBoundary_Negative(t *testing.T) {
	report := func(c *Controller, args, result string) string {
		_, _, stub := c.Report("read", args, result, "id", false, true)
		return stub
	}

	t.Run("512-byte floor is bytes, not runes", func(t *testing.T) {
		c := New(Config{})
		assert.Empty(t, report(c, `{"path":"x"}`, strings.Repeat("x", 511)), "511 bytes: under the floor")
		assert.Empty(t, report(c, `{"path":"x"}`, strings.Repeat("x", 511)), "second 511-byte copy: still under")

		c2 := New(Config{})
		assert.Empty(t, report(c2, `{"path":"x"}`, strings.Repeat("x", 512)), "first call is never stubbed")
		assert.NotEmpty(t, report(c2, `{"path":"x"}`, strings.Repeat("x", 512)), "512 bytes: at the floor")

		c3 := New(Config{})
		report(c3, `{"path":"x"}`, strings.Repeat("日", 170))
		assert.Empty(t, report(c3, `{"path":"x"}`, strings.Repeat("日", 170)), "170 CJK runes = 510 bytes: under")
		c4 := New(Config{})
		report(c4, `{"path":"x"}`, strings.Repeat("日", 171))
		assert.NotEmpty(t, report(c4, `{"path":"x"}`, strings.Repeat("日", 171)), "171 CJK runes = 513 bytes: over")
	})

	t.Run("args preview truncates to 120 runes without splitting runes", func(t *testing.T) {
		c := New(Config{})
		args := `{"path":"` + strings.Repeat("日", 112) + `"}` // canonical preview is 123 runes
		big := strings.Repeat("y", 600)
		_, _, _ = c.Report("read", args, big, "id0", false, true)
		_, _, stub := c.Report("read", args, big, "id1", false, true)
		want := `[a1 note: this result is byte-identical to the read result earlier this turn (call id0). ` +
			`Refer to that result; it has not changed. Args: {"path":"` + strings.Repeat("日", 111) + `…]`
		assert.Equal(t, want, stub)
		assert.True(t, utf8.ValidString(stub), "rune-truncation must not split a CJK rune")
	})

	t.Run("failed calls are never stubbed", func(t *testing.T) {
		c := New(Config{})
		for i := 1; i <= 6; i++ {
			_, _, stub := c.Report("read", `{"path":"x"}`, strings.Repeat("e", 600), fmt.Sprintf("f%d", i), true, true)
			assert.Empty(t, stub, "failed result %d must pass through", i)
		}
	})
}

// The per-turn loop caps count Before() checks, not completed calls: a
// runner that reports fetches without pre-checking them never feeds the
// cap. Pinned as the actual behavior.
func TestLoopCapCountsBeforeChecks_Negative(t *testing.T) {
	t.Run("reports alone never trip the cap", func(t *testing.T) {
		c := New(Config{})
		for i := 0; i < 60; i++ {
			_, _, _ = c.Report("fetch", fmt.Sprintf(`{"url":"https://x/%d"}`, i), "ok", fmt.Sprintf("r%d", i), false, true)
		}
		for i := 1; i <= defaultLoopCap; i++ {
			assert.Nil(t, c.Before("fetch", fmt.Sprintf(`{"url":"https://x/b%d"}`, i), true),
				"cap must not fire before the limit (check %d)", i)
		}
		d := c.Before("fetch", `{"url":"https://x/over"}`, true)
		require.NotNil(t, d)
		assert.Equal(t, "block", d.Action)
		assert.Equal(t, "loop_fetch_cap", d.Code)
		assert.Equal(t, defaultLoopCap, d.Count)
		assert.Contains(t, d.Message, "already made 50 web fetches")
	})

	t.Run("reset clears the counters", func(t *testing.T) {
		c := New(Config{})
		for i := 1; i <= defaultLoopCap+1; i++ {
			_ = c.Before("agent_spawn", fmt.Sprintf(`{"prompt":"p%d"}`, i), false)
		}
		require.NotNil(t, c.Before("agent_spawn", `{"prompt":"again"}`, false), "still over the cap")
		c.Reset()
		assert.Nil(t, c.Before("agent_spawn", `{"prompt":"fresh"}`, false), "reset clears the spawn counter")
	})
}

// ---------- differential ----------

// refCall is one completed call as the naive reference evaluator sees it.
type refCall struct {
	tool     string
	args     string
	result   string
	failed   bool
	readable bool
}

type refNoProg struct {
	res     string
	repeats int
}

type refEntry struct {
	key        string
	res        string
	repeatable bool
}

// refGuard is a naive from-scratch evaluator of the guardrail spec: plain
// maps keyed by canonical strings, no hashing, cycle detection by string
// composition instead of index arithmetic.
type refGuard struct {
	warnings bool
	hardStop bool

	exact    map[string]int
	progress map[string]bool
	toolFail map[string]int
	noProg   map[string]refNoProg

	streakKey   string
	streakRes   string
	streakCount int

	history []refEntry
	halted  bool
}

var (
	refRepeatable = map[string]bool{"agent_wait": true, "agent_list": true}
	refTolerant   = map[string]bool{"bash": true, "fetch": true}
)

// refKey builds a call identity with the stdlib alone: objects canonicalize
// through decode+marshal (sorted keys), everything else keys by trimmed raw
// text. The matrix deliberately avoids numbers beyond float64's exact range
// and "5.0"-style literals, where the implementation's verbatim json.Number
// rule is intentionally stricter.
func refKey(tool, args string) string {
	trimmed := strings.TrimSpace(args)
	if trimmed == "" {
		return tool + "\x00{}"
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		return tool + "\x00raw:" + trimmed
	}
	b, err := json.Marshal(m)
	if err != nil {
		return tool + "\x00raw:" + trimmed
	}
	return tool + "\x00" + string(b)
}

// report folds one call into the reference and returns the verdict the
// implementation must agree with: decision action/code/count, notice kind
// ("" / "streak" / "cycle"), and stub presence.
func (r *refGuard) report(c refCall) (action, code string, count int, notice string, stub bool) {
	key := refKey(c.tool, c.args)
	res := c.result // matrix results are plain text: identity is the hash
	action, code, count = "", "", 0

	if c.failed {
		if r.progress[key] {
			delete(r.exact, key)
			delete(r.progress, key)
		}
		if r.exact == nil {
			r.exact = make(map[string]int)
		}
		r.exact[key]++
		exactCnt := r.exact[key]
		if r.toolFail == nil {
			r.toolFail = make(map[string]int)
		}
		r.toolFail[c.tool]++
		sameCnt := r.toolFail[c.tool]
		delete(r.noProg, key)
		switch {
		case r.hardStop && !refTolerant[c.tool] && sameCnt >= 8 && !r.halted:
			r.halted = true
			action, code, count = "halt", "same_tool_failure_halt", sameCnt
		case r.warnings && exactCnt >= 2:
			action, code, count = "warn", "repeated_exact_failure_warning", exactCnt
		case r.warnings && sameCnt >= 3:
			action, code, count = "warn", "same_tool_failure_warning", sameCnt
		}
	} else {
		delete(r.exact, key)
		delete(r.toolFail, c.tool)
		if !c.readable {
			if r.progress == nil {
				r.progress = make(map[string]bool)
			}
			for k := range r.exact {
				r.progress[k] = true
			}
			r.toolFail = nil
		} else {
			repeats := 1
			if rec, ok := r.noProg[key]; ok && rec.res == res {
				repeats = rec.repeats + 1
			}
			if r.noProg == nil {
				r.noProg = make(map[string]refNoProg)
			}
			r.noProg[key] = refNoProg{res: res, repeats: repeats}
			if r.warnings && repeats >= 2 {
				action, code, count = "warn", "idempotent_no_progress_warning", repeats
			}
		}
	}

	// Consecutive identical (call, result) streak.
	if r.streakKey == key && r.streakRes == res {
		r.streakCount++
	} else {
		r.streakKey, r.streakRes, r.streakCount = key, res, 1
	}
	notice = ""
	if !refRepeatable[c.tool] && r.streakCount >= 3 {
		notice = "streak"
	}
	r.history = append(r.history, refEntry{key: key, res: res, repeatable: refRepeatable[c.tool]})
	if len(r.history) > 64 {
		r.history = r.history[len(r.history)-64:]
	}

	if notice == "" {
		if _, laps, ok := r.detectCycle(); ok {
			notice = "cycle"
			if r.hardStop && laps >= 5 && !r.halted {
				r.halted = true
				action, code, count = "halt", "identical_cycle_halt", laps
			}
		}
	}
	if r.hardStop && r.streakCount >= 5 && !r.halted {
		r.halted = true
		action, code, count = "halt", "identical_call_streak_halt", r.streakCount
	}
	stub = r.streakCount >= 2 && !c.failed && len(c.result) >= 512
	return action, code, count, notice, stub
}

// before re-derives the pre-call gate from the reference state. The
// matrix's probe tools are not loop-capped, so that leg is nil on both
// sides by construction.
func (r *refGuard) before(tool, args string, readable bool) (action, code string, count int) {
	key := refKey(tool, args)
	if !r.hardStop {
		return "", "", 0
	}
	cnt := 0
	if !r.progress[key] {
		cnt = r.exact[key]
	}
	if cnt >= 5 {
		return "block", "repeated_exact_failure_block", cnt
	}
	if readable {
		if rec, ok := r.noProg[key]; ok && rec.repeats >= 5 {
			return "block", "idempotent_no_progress_block", rec.repeats
		}
	}
	return "", "", 0
}

// detectCycle re-derives the batch-cycle rule by rendering the history as
// text: a period-p cycle with L laps means the trailing p*L entries are the
// last p entries repeated L times, with at least one non-poller in the
// block. Period 1 is the consecutive streak's job.
func (r *refGuard) detectCycle() (period, laps int, ok bool) {
	parts := make([]string, len(r.history))
	for i, e := range r.history {
		parts[i] = e.key + "\x00" + e.res
	}
	for p := 2; p <= 4; p++ {
		if len(parts) < p*3 {
			continue
		}
		block := strings.Join(parts[len(parts)-p:], "\x01")
		laps := 1
		for {
			n := laps + 1
			if p*n > len(parts) {
				break
			}
			want := block
			for i := 1; i < n; i++ {
				want += "\x01" + block
			}
			if strings.Join(parts[len(parts)-p*n:], "\x01") != want {
				break
			}
			laps++
		}
		if laps >= 3 {
			allRepeatable := true
			for _, e := range r.history[len(r.history)-p:] {
				if !e.repeatable {
					allRepeatable = false
					break
				}
			}
			if !allRepeatable {
				return p, laps, true
			}
		}
	}
	return 0, 0, false
}

// noticeKind classifies an implementation notice by its documented wording,
// so the comparison is content-level, not byte-level.
func noticeKind(notice string) string {
	switch {
	case notice == "":
		return ""
	case strings.Contains(notice, "consecutive identical call"):
		return "streak"
	case strings.Contains(notice, "cycle"):
		return "cycle"
	default:
		return "other"
	}
}

func repeatCalls(n int, mk func(i int) refCall) []refCall {
	out := make([]refCall, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, mk(i))
	}
	return out
}

// randomCalls builds a mixed log from a fixed-seed LCG — no time, no
// platform entropy, fully reproducible.
func randomCalls(n int) []refCall {
	const mul, inc, mod = 1103515245, 12345, 1 << 31
	state := uint32(20261103)
	next := func() uint32 { state = (state*mul + inc) % mod; return state }
	tools := []string{"read", "bash", "search", "agent_wait", "write"}
	argsTmpl := []string{
		`{"path":"/etc/hosts"}`, `{"path":"/tmp/scratch"}`, `{"q":"deadline"}`,
		`{"a":1,"b":2}`, `{"b":2,"a":1}`, // canonical-equivalent pair
	}
	results := []string{"ok", "exit 1", "not found", "wrote 12 bytes", "denied"}
	calls := make([]refCall, 0, n)
	for i := 0; i < n; i++ {
		tool := tools[next()%uint32(len(tools))]
		args := argsTmpl[next()%uint32(len(argsTmpl))]
		if next()%7 == 0 {
			args = fmt.Sprintf(`{"n":%d}`, i) // unique arg: pattern breaker
		}
		result := results[next()%uint32(len(results))]
		if next()%5 == 0 {
			result = fmt.Sprintf("out-%d", i) // unique result: streak breaker
		}
		calls = append(calls, refCall{
			tool:     tool,
			args:     args,
			result:   result,
			failed:   next()%2 == 0,
			readable: next()%2 == 0,
		})
	}
	return calls
}

var scenarios = []struct {
	name  string
	calls []refCall
}{
	{"exact replay of a failing read", repeatCalls(10, func(i int) refCall {
		return refCall{tool: "read", args: `{"path":"/etc/hosts"}`, result: "open /etc/hosts: no such file", failed: true, readable: true}
	})},
	{"same tool, varied args", repeatCalls(10, func(i int) refCall {
		return refCall{tool: "read", args: fmt.Sprintf(`{"path":"/missing/%d"}`, i), result: "no such file", failed: true, readable: true}
	})},
	{"failure-tolerant bash with varied commands", repeatCalls(10, func(i int) refCall {
		return refCall{tool: "bash", args: fmt.Sprintf(`{"command":"make check %d"}`, i), result: "exit 1", failed: true, readable: false}
	})},
	{"read-only no-progress", repeatCalls(10, func(i int) refCall {
		return refCall{tool: "read", args: `{"path":"go.mod"}`, result: "module a1", failed: false, readable: true}
	})},
	{"no-progress with alternating results", repeatCalls(8, func(i int) refCall {
		res := "content v1"
		if i/2%2 == 1 {
			res = "content v2"
		}
		return refCall{tool: "read", args: `{"path":"go.mod"}`, result: res, failed: false, readable: true}
	})},
	{"two signatures alternating (batch cycle)", repeatCalls(12, func(i int) refCall {
		args := `{"path":"a"}`
		if i%2 == 1 {
			args = `{"path":"b"}`
		}
		return refCall{tool: "read", args: args, result: "content", failed: true, readable: true}
	})},
	// Same A,B cycle on a failure-tolerant tool: no same-tool halt can
	// preempt it, so only the cycle breaker stops the batch.
	{"failure-tolerant batch cycle (cycle halt)", repeatCalls(12, func(i int) refCall {
		args := `{"command":"make a"}`
		if i%2 == 1 {
			args = `{"command":"make b"}`
		}
		return refCall{tool: "bash", args: args, result: "exit 1", failed: true, readable: false}
	})},
	{"failures interleaved with mutations", repeatCalls(16, func(i int) refCall {
		if i%2 == 0 {
			return refCall{tool: "read", args: `{"path":"main.go"}`, result: "no such file", failed: true, readable: true}
		}
		return refCall{tool: "write", args: fmt.Sprintf(`{"path":"main.go","n":%d}`, i/2), result: "wrote 12 bytes", failed: false, readable: false}
	})},
	{"poller with large stable results", repeatCalls(6, func(i int) refCall {
		return refCall{tool: "agent_wait", args: `{"id":"job-7"}`, result: strings.Repeat("status: running\n", 80), failed: false, readable: true}
	})},
	{"mutation resets a varied-args streak", func() []refCall {
		calls := repeatCalls(3, func(i int) refCall {
			return refCall{tool: "bash", args: fmt.Sprintf(`{"command":"try %d"}`, i), result: "exit 1", failed: true, readable: false}
		})
		calls = append(calls, refCall{tool: "write", args: `{"path":"x"}`, result: "ok", failed: false, readable: false})
		return append(calls, repeatCalls(2, func(i int) refCall {
			return refCall{tool: "bash", args: `{"command":"try 0"}`, result: "exit 1", failed: true, readable: false}
		})...)
	}()},
	{"deterministic pseudo-random mix", randomCalls(40)},
}

// oracle: naive reference policy evaluator — a from-scratch re-derivation
// of the guardrail spec (exact-args / same-tool / no-progress tiers, streak
// and cycle breakers, throttle-free matrix) drives the same call logs, and
// every verdict must agree: decision action/code/count/tool, notice kind,
// stub presence, and the Before block gate.
func TestGuardVerdictsVsNaiveEvaluator_Differential(t *testing.T) {
	configs := []Config{
		{Warnings: true, HardStop: true},
		{Warnings: true},
		{}, // fully disarmed: only notices and stubs remain
	}
	for _, cfg := range configs {
		// seen/stubs guard against vacuous agreement: the matrix must
		// actually provoke every verdict class the config allows.
		seen := map[string]int{}
		stubs := 0
		for _, sc := range scenarios {
			t.Run(fmt.Sprintf("(w=%v,h=%v)/%s", cfg.Warnings, cfg.HardStop, sc.name), func(t *testing.T) {
				c := New(cfg)
				ref := &refGuard{warnings: cfg.Warnings, hardStop: cfg.HardStop}
				for i, call := range sc.calls {
					d, notice, stub := c.Report(call.tool, call.args, call.result,
						fmt.Sprintf("c%d", i), call.failed, call.readable)
					wantAction, wantCode, wantCount, wantNotice, wantStub := ref.report(call)
					if wantStub {
						stubs++
					}

					if wantAction == "" {
						assert.Nil(t, d, "step %d (%s %s): expected no decision", i, call.tool, call.args)
					} else {
						seen[wantCode]++
						require.NotNil(t, d, "step %d (%s %s): expected %s %s", i, call.tool, call.args, wantAction, wantCode)
						assert.Equal(t, wantAction, d.Action, "step %d action", i)
						assert.Equal(t, wantCode, d.Code, "step %d code", i)
						assert.Equal(t, wantCount, d.Count, "step %d count", i)
						assert.Equal(t, call.tool, d.Tool, "step %d tool", i)
					}
					assert.Equal(t, wantNotice, noticeKind(notice), "step %d notice kind", i)
					assert.Equal(t, wantStub, stub != "", "step %d stub presence", i)

					bd := c.Before(call.tool, call.args, call.readable)
					wantBlock, wantBlockCode, wantBlockCount := ref.before(call.tool, call.args, call.readable)
					if wantBlock == "" {
						assert.Nil(t, bd, "step %d Before: expected no block", i)
					} else {
						seen["before:"+wantBlockCode]++
						require.NotNil(t, bd, "step %d Before: expected %s", i, wantBlockCode)
						assert.Equal(t, wantBlock, bd.Action, "step %d Before action", i)
						assert.Equal(t, wantBlockCode, bd.Code, "step %d Before code", i)
						assert.Equal(t, wantBlockCount, bd.Count, "step %d Before count", i)
					}
				}
			})
		}
		assert.NotZero(t, stubs, "the matrix must exercise stubbing")
		switch {
		case cfg.Warnings && cfg.HardStop:
			for _, code := range []string{
				"repeated_exact_failure_warning", "same_tool_failure_warning",
				"idempotent_no_progress_warning", "same_tool_failure_halt",
				"identical_call_streak_halt", "identical_cycle_halt",
				"before:repeated_exact_failure_block", "before:idempotent_no_progress_block",
			} {
				assert.NotZero(t, seen[code], "the matrix must exercise %s under w+h", code)
			}
		case cfg.Warnings:
			for _, code := range []string{
				"repeated_exact_failure_warning", "same_tool_failure_warning",
				"idempotent_no_progress_warning",
			} {
				assert.NotZero(t, seen[code], "the matrix must exercise %s under warnings-only", code)
			}
		default:
			assert.Empty(t, seen, "a fully disarmed guard never decides")
		}
	}
}
