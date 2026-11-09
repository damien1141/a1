package tokentool

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// refWeight restates the allocator's starting table as literal data, so the
// reference shares no code with buildWeights.
type refWeight struct {
	tool      string
	weight    float64
	reasoning string
}

var refBaseWeights = []refWeight{
	{"grep", 22, "cheap high-recall search; default first pass"},
	{"find", 16, "locates files before reading"},
	{"read", 24, "reads highest-value files"},
	{"bash", 14, "runs cheap verification commands"},
	{"stack", 10, "traces call sites cheaply"},
	{"impact", 8, "checks blast radius after reading"},
	{"context", 6, "fold-health check late in pass"},
}

// refAdjust is one task-keyword tweak: boost an existing tool (creating it if
// absent), cut an existing tool down, or splice in an extra tool.
type refAdjust struct {
	kind      string // "boost" | "cut" | "extra"
	tool      string
	delta     float64
	reasoning string
}

// refTaskRules restates the keyword→adjustment table in the order the
// implementation's switch checks them, so first-match precedence is pinned.
var refTaskRules = []struct {
	keywords []string
	adjusts  []refAdjust
}{
	{[]string{"rust", "borrow", "lifetime"}, []refAdjust{
		{"boost", "bash", 18, "runs cargo check for rust diagnostics"},
		{"boost", "read", 12, "reads Cargo.toml and failing modules"},
		{"cut", "context", 4, "deferred after cheaper passes"},
	}},
	{[]string{"python", "type", "import"}, []refAdjust{
		{"boost", "bash", 12, "runs pyright or mypy"},
		{"boost", "grep", 8, "finds imports and type aliases"},
	}},
	{[]string{"build", "cmake", "make"}, []refAdjust{
		{"boost", "bash", 20, "runs build-system commands"},
		{"boost", "read", 6, "reads build files for targets"},
		{"extra", "build", 18, "build-system semantics pass"},
	}},
	{[]string{"error", "diagnostic", "stack"}, []refAdjust{
		{"boost", "stack", 14, "traces error origin"},
		{"boost", "read", 6, "reads failing code paths"},
	}},
	{[]string{"security", "vuln", "injection"}, []refAdjust{
		{"boost", "grep", 10, "finds risky API usage"},
		{"extra", "vuln", 20, "security-focused scan"},
		{"cut", "context", 2, "deferred after cheaper passes"},
	}},
	{[]string{"test", "coverage"}, []refAdjust{
		{"boost", "bash", 10, "runs tests and coverage"},
		{"extra", "coverage", 14, "coverage mapping"},
		{"extra", "test", 12, "test-output interpretation"},
	}},
	{[]string{"doc", "api"}, []refAdjust{
		{"boost", "grep", 8, "finds exported symbols"},
		{"extra", "apidoc", 18, "API doc stubs"},
		{"cut", "impact", 3, "deferred after cheaper passes"},
	}},
	{[]string{"git", "blame", "history"}, []refAdjust{
		{"boost", "grep", 6, "finds changed paths"},
		{"extra", "git", 18, "git intelligence pass"},
	}},
	{[]string{"refactor", "migration"}, []refAdjust{
		{"boost", "impact", 14, "checks blast radius"},
		{"boost", "stack", 8, "finds callers"},
		{"extra", "migration", 12, "multi-file coordination"},
	}},
}

// refAllocate re-derives the whole pipeline with its own data and loops:
// keyword adjustments (first matching rule only), weights clamped to 1,
// proportional tokens rounded with a 1-token floor, descending order with
// a tool-name tie-break, the limit cut, and the final re-sum adjustment
// (including its observed quirks: the negative branch never touches the
// last entry, and the positive branch recomputes the head's share as a
// fraction, not a percentage).
func refAllocate(task string, total, limit int) (lines map[string]string, order []string, rawTied bool) {
	if total <= 0 {
		total = tokenDefaultTotal
	}
	if limit <= 0 {
		limit = tokenDefaultLimit
	}

	byTool := map[string]refWeight{}
	for _, w := range refBaseWeights {
		byTool[w.tool] = w
	}
	t := strings.ToLower(strings.TrimSpace(task))
	if t == "" {
		t = "general"
	}
rules:
	for _, r := range refTaskRules {
		for _, k := range r.keywords {
			if !strings.Contains(t, k) {
				continue
			}
			for _, a := range r.adjusts {
				w, ok := byTool[a.tool]
				switch a.kind {
				case "boost":
					if ok {
						w.weight += a.delta
						w.reasoning = a.reasoning
					} else {
						w = refWeight{tool: a.tool, weight: a.delta, reasoning: a.reasoning}
					}
					byTool[a.tool] = w
				case "cut":
					if !ok {
						continue
					}
					w.weight -= a.delta
					if w.weight < 1 {
						w.weight = 1
					}
					w.reasoning = "deferred after cheaper passes"
					byTool[a.tool] = w
				case "extra":
					byTool[a.tool] = refWeight{tool: a.tool, weight: a.delta, reasoning: a.reasoning}
				}
			}
			break rules
		}
	}

	// Deterministic iteration: base order first, extras sorted by name.
	var ws []refWeight
	seen := map[string]bool{}
	for _, w := range refBaseWeights {
		ws = append(ws, byTool[w.tool])
		seen[w.tool] = true
	}
	var extras []string
	for tool := range byTool {
		if !seen[tool] {
			extras = append(extras, tool)
		}
	}
	sort.Strings(extras)
	for _, tool := range extras {
		ws = append(ws, byTool[tool])
	}

	weightSum := 0.0
	for _, w := range ws {
		weightSum += w.weight
	}

	type refEntry struct {
		tool      string
		tokens    int
		share     float64
		reasoning string
	}
	var entries []refEntry
	for _, w := range ws {
		if w.weight < 1 {
			w.weight = 1
		}
		raw := int(math.Round(float64(total) * (w.weight / weightSum)))
		if raw < 1 {
			raw = 1
		}
		entries = append(entries, refEntry{
			tool:      w.tool,
			tokens:    raw,
			share:     math.Round(float64(raw)/float64(total)*10000) / 100,
			reasoning: w.reasoning,
		})
	}
	// Raw-count ties (equal weights rounding to equal tokens) flag the
	// tie-bearing fixtures: there the sort's name tie-break decides the
	// table, and callers use the flag to prove a fixture really does tie
	// before normalize makes the counts distinct.
	seenRaw := map[int]bool{}
	for _, e := range entries {
		if seenRaw[e.tokens] {
			rawTied = true
		}
		seenRaw[e.tokens] = true
	}

	// Tool names are unique, so (tokens, name) totally orders the entries —
	// the same contract the implementation's sort must honor for tied
	// counts to resolve identically every run.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].tokens != entries[j].tokens {
			return entries[i].tokens > entries[j].tokens
		}
		return entries[i].tool < entries[j].tool
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}

	allocated := 0
	for _, e := range entries {
		allocated += e.tokens
	}
	diff := total - allocated
	if diff > 0 {
		entries[0].tokens += diff
		entries[0].share = math.Round(float64(entries[0].tokens)/float64(total)*100) / 100
	} else if diff < 0 {
		for i := 0; i < len(entries)-1; i++ {
			if entries[i].tokens+diff < 1 {
				continue
			}
			entries[i].tokens += diff
			break
		}
	}

	lines = map[string]string{}
	for _, e := range entries {
		lines[e.tool] = fmt.Sprintf("%d\t%.1f%%\t%s\t%s", e.tokens, e.share, e.tool, e.reasoning)
		order = append(order, e.tool)
	}
	return lines, order, rawTied
}

var tokenLineRe = regexp.MustCompile(`^(\d+)\t([0-9.]+)%\t(\w+)\t(.*)$`)

// parseTokenContent splits the rendered output into per-tool lines (the
// comparison key), the tool order, and the token counts.
func parseTokenContent(t *testing.T, content string) (lines map[string]string, order []string, tokens []int) {
	t.Helper()
	lines = map[string]string{}
	for _, l := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		if l == "" {
			continue
		}
		m := tokenLineRe.FindStringSubmatch(l)
		require.NotNil(t, m, "unparseable allocation line %q", l)
		n, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		lines[m[3]] = l
		order = append(order, m[3])
		tokens = append(tokens, n)
	}
	return lines, order, tokens
}

// oracle: naive reference implementation in this test — the allocation
// pipeline is re-derived from the literal weight table and keyword rules
// above with independent loops, then compared line for line across task
// kinds (including first-rule precedence and injected extra tools), limit
// cuts, off-round totals that exercise both re-sum branches, and a tiny
// budget that floors every share at one token. Token counts must also come
// out non-increasing. The table is fully deterministic — weights emit in
// base-table order (extras by name) and the sort tie-breaks equal counts by
// tool name — so even tie-bearing tasks must match the reference exactly,
// in order.
func TestRunToken_AllocationVsNaiveReference_Differential(t *testing.T) {
	cases := []struct {
		name string
		in   tokenInput
	}{
		{"defaults", tokenInput{}},
		{"unknown task keeps base weights", tokenInput{Task: "quantum flux capacitor"}},
		{"rust borrow", tokenInput{Task: "rust borrow-checker failure", Total: 2000, Limit: 5}},
		{"python rule wins over error rule", tokenInput{Task: "python type error", Total: 3000}},
		{"build system adds a tool", tokenInput{Task: "build system cmake", Total: 2000, Limit: 5}},
		{"error diagnostics boost stack", tokenInput{Task: "diagnostic stack trace", Total: 1500, Limit: 4}},
		{"security adds a scan tool", tokenInput{Task: "security vuln scan", Total: 5000}},
		{"test coverage ties at the top", tokenInput{Task: "test coverage run", Total: 2500}},
		{"doc api cuts impact", tokenInput{Task: "api doc pass", Total: 800, Limit: 3}},
		{"git blame history", tokenInput{Task: "git blame history", Total: 1234}},
		{"refactor migration", tokenInput{Task: "refactor migration", Total: 777, Limit: 9}},
		{"rounding deficit absorbed by head", tokenInput{Task: "", Total: 999}},
		{"rounding surplus absorbed by head", tokenInput{Task: "", Total: 1001}},
		{"tiny budget floors every share", tokenInput{Task: "", Total: 3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.in)
			require.NoError(t, err)
			out, err := runToken(t.Context(), raw)
			require.NoError(t, err)

			wantLines, wantOrder, _ := refAllocate(tc.in.Task, tc.in.Total, tc.in.Limit)
			require.NotEmpty(t, wantLines)

			gotLines, gotOrder, gotTokens := parseTokenContent(t, out.Content)

			// Deterministic contract: the base-order emission plus
			// the sort's name tie-break fix the table even when raw
			// counts tie, so every case — tie-bearing ones included —
			// must match the reference line for line and in order.
			assert.Equal(t, wantLines, gotLines, "every allocation line must match the naive reference")
			assert.Equal(t, wantOrder, gotOrder, "counts descending, ties broken by tool name: the order is deterministic")

			for i := 1; i < len(gotTokens); i++ {
				assert.GreaterOrEqual(t, gotTokens[i-1], gotTokens[i], "allocations render in descending token order")
			}

			assert.Equal(t, fmt.Sprintf("%d allocations", len(wantLines)), out.Detail)
			assert.Equal(t, out.Content, out.Output)
		})
	}
}

// oracle: hand-written golden literals — the detail phrase renders the
// trimmed task, falls back to "general" for empty or junk input, and never
// errors.
func TestTokenTool_DetailFromArgs_Differential(t *testing.T) {
	tool := TokenTool()
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"empty object", `{}`, "tokenbudget general"},
		{"empty task", `{"task":""}`, "tokenbudget general"},
		{"task", `{"task":"rust borrow"}`, "tokenbudget rust borrow"},
		{"task padded", `{"task":"  padded  "}`, "tokenbudget padded"},
		{"multibyte task", `{"task":"検索"}`, "tokenbudget 検索"},
		{"non-string task", `{"task":17}`, "tokenbudget general"},
		{"not json", `nope`, "tokenbudget general"},
		{"json array", `[]`, "tokenbudget general"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}

// oracle: hand-written golden table — the tie-bearing "test coverage" task
// must render exactly this allocation on every run. bash and read tie at
// 441 raw tokens; the name tie-break puts bash first, so the +1 head
// rounding remainder attaches to bash (442), and the normalize quirk
// recomputes its share as a fraction (0.18 → "0.2%"), not a percentage.
func TestRunToken_TiedWeights_Golden(t *testing.T) {
	// The reference flags duplicate raw counts, proving the fixture
	// really does tie before normalize makes the counts distinct.
	_, _, rawTied := refAllocate("test coverage run", 2500, 20)
	require.True(t, rawTied, "the golden fixture must bear a raw-count tie")

	raw, err := json.Marshal(tokenInput{Task: "test coverage run", Total: 2500})
	require.NoError(t, err)
	out, err := runToken(t.Context(), raw)
	require.NoError(t, err)

	want := []string{
		"442\t0.2%\tbash\truns tests and coverage",
		"441\t17.6%\tread\treads highest-value files",
		"404\t16.2%\tgrep\tcheap high-recall search; default first pass",
		"294\t11.8%\tfind\tlocates files before reading",
		"257\t10.3%\tcoverage\tcoverage mapping",
		"221\t8.8%\ttest\ttest-output interpretation",
		"184\t7.4%\tstack\ttraces call sites cheaply",
		"147\t5.9%\timpact\tchecks blast radius after reading",
		"110\t4.4%\tcontext\tfold-health check late in pass",
	}
	assert.Equal(t, strings.Join(want, "\n")+"\n", out.Content)
	assert.Equal(t, "9 allocations", out.Detail)
}

// Determinism pin: the same tie-bearing input must render byte-identical
// output across repeated calls — the weight table emits in a fixed order
// and the sort's name tie-break fixes tied counts, so the head rounding
// remainder attaches to the same entry on every call.
func TestRunToken_TiedWeights_RepeatedCallsDeterministic(t *testing.T) {
	raw, err := json.Marshal(tokenInput{Task: "test coverage run", Total: 2500})
	require.NoError(t, err)

	var first string
	for i := 0; i < 5; i++ {
		out, err := runToken(t.Context(), raw)
		require.NoError(t, err)
		if i == 0 {
			first = out.Content
			require.NotEmpty(t, first)
			continue
		}
		assert.Equal(t, first, out.Content, "call %d: tied allocations must render identically", i)
		assert.Equal(t, "9 allocations", out.Detail, "call %d: detail must stay stable", i)
	}
}
