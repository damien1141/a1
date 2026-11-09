package sessiontool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
	"github.com/damien1141/a1/internal/session"
)

// ---------- differential ----------

// naiveCIIndex finds the first case-insensitive occurrence of needle with a
// plain EqualFold scan over fixed-length byte windows — no ToLower fast
// path, no shared code with ciIndex.
func naiveCIIndex(content, needle string) int {
	if needle == "" {
		return -1 // documented guard: an empty needle is not a hit
	}
	n := len(needle)
	for i := 0; i+n <= len(content); i++ {
		if strings.EqualFold(content[i:i+n], needle) {
			return i
		}
	}
	return -1
}

// oracle: naive reference implementation in this test — an EqualFold
// byte-window scan compared with the ToLower fast path over ASCII, CJK,
// Greek, and length-changing-locale inputs where both must agree.
func TestCIIndexVsEqualFoldWindows_Differential(t *testing.T) {
	cases := []struct {
		name            string
		content, needle string
	}{
		{"ascii mixed case", "has Needle inside", "needle"},
		{"ascii upper content", "NEEDLE first", "needle"},
		{"ascii upper needle", "needle last", "NEEDLE"},
		{"cjk content", "日本語にNEEDLEあり", "needle"},
		{"cjk needle", "日本語の検索です", "検索"},
		{"greek around match", "Οι NEEDLE εδώ", "needle"},
		{"no match", "nothing to see", "needle"},
		{"empty needle", "anything", ""},
		{"turkish capital needle", "ascii only", "İ"},
		{"turkish capital content", "İstanbul corner", "i"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveCIIndex(tc.content, tc.needle), ciIndex(tc.content, tc.needle))
		})
	}
}

// oracle: hand-written golden literal — the snippet window arithmetic (110
// bytes before, 170 after, UTF-8 alignment, »« marks, ellipses, whitespace
// collapse) is pinned by inputs whose expected windows were derived by hand.
func TestSnippetAroundVsGolden_Differential(t *testing.T) {
	t.Run("short content is shown whole", func(t *testing.T) {
		const content = "abc needle def"
		i := strings.Index(content, "needle")
		require.GreaterOrEqual(t, i, 0)
		assert.Equal(t, "abc »needle« def", snippetAround(content, i, len("needle")))
	})

	t.Run("long content windows to 110 before and 170 after", func(t *testing.T) {
		content := strings.Repeat("x", 150) + "NEEDLE" + strings.Repeat("y", 200)
		i := strings.Index(content, "NEEDLE")
		want := "…" + strings.Repeat("x", 110) + "»NEEDLE«" + strings.Repeat("y", 170) + "…"
		assert.Equal(t, want, snippetAround(content, i, len("NEEDLE")))
	})

	t.Run("whitespace runs collapse to single spaces", func(t *testing.T) {
		const content = "a\nb\t\tc needle d\ne"
		i := strings.Index(content, "needle")
		require.GreaterOrEqual(t, i, 0)
		assert.Equal(t, "a b c »needle« d e", snippetAround(content, i, len("needle")))
	})

	t.Run("multibyte boundaries align outward", func(t *testing.T) {
		// 40 CJK runes (120 bytes) before the needle, 100 after. The raw
		// window edges (bytes 10 and 296) land mid-rune and must round out:
		// 37 漢 before (bytes 9..119) and 57 字 after (bytes 126..296).
		content := strings.Repeat("漢", 40) + "needle" + strings.Repeat("字", 100)
		i := strings.Index(content, "needle")
		require.Equal(t, 120, i)
		want := "…" + strings.Repeat("漢", 37) + "»needle«" + strings.Repeat("字", 57) + "…"
		assert.Equal(t, want, snippetAround(content, i, len("needle")))
	})
}

// naiveHit is one reference hit: the role, the HH:MM render of the entry
// timestamp, and the »marked« span in the message's own casing.
type naiveHit struct {
	role   string
	hhmm   string
	marked string
}

// naiveSession is one session file with its reference hits.
type naiveSession struct {
	name  string
	mtime time.Time
	hits  []naiveHit
}

// naiveTranscriptScan re-derives the search result independently of the
// tool: os.ReadDir + mtime sort for the newest-first listing, a generic
// map-based JSON decode per JSONL line (not the session package's typed
// structs), and the EqualFold window scan for matching.
func naiveTranscriptScan(t *testing.T, dir, query string) []naiveSession {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var sessions []naiveSession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, ierr := e.Info()
		require.NoError(t, ierr)
		sessions = append(sessions, naiveSession{name: e.Name(), mtime: info.ModTime()})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].mtime.After(sessions[j].mtime) })

	for si := range sessions {
		data, rerr := os.ReadFile(filepath.Join(dir, sessions[si].name))
		require.NoError(t, rerr)
		for _, line := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var m map[string]any
			if jerr := json.Unmarshal([]byte(line), &m); jerr != nil {
				continue
			}
			var text, role string
			switch m["type"] {
			case "EntryMessage":
				msg, _ := m["message"].(map[string]any)
				if msg == nil {
					continue
				}
				r, _ := msg["role"].(string)
				switch r {
				case "user", "assistant", "tool":
				default:
					continue // system framing is noise
				}
				c, _ := msg["content"].(string)
				text, role = c, r
			case "EntryCompaction":
				comp, _ := m["compaction"].(map[string]any)
				if comp == nil {
					continue
				}
				s, _ := comp["summary"].(string)
				text, role = s, "summary"
			default:
				continue
			}
			if text == "" {
				continue
			}
			if i := naiveCIIndex(text, query); i >= 0 {
				tsRaw, _ := m["timestamp"].(string)
				ts, _ := time.Parse(time.RFC3339, tsRaw)
				sessions[si].hits = append(sessions[si].hits, naiveHit{
					role:   role,
					hhmm:   ts.Format("15:04"),
					marked: "»" + text[i:i+len(query)] + "«",
				})
			}
		}
	}
	return sessions
}

var (
	searchHeaderRe = regexp.MustCompile(`^Searched (\d+) past session\(s\) for ".*" — (\d+) matching message\(s\) in (\d+) session\(s\), newest first\.$`)
	blockHeaderRe  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} · session \S+ · (\d+) match(?:es)?(?: \((\d+) of \d+ shown\))?$`)
	hitRowRe       = regexp.MustCompile(`^  (\d{2}:\d{2}) (user|assistant|tool|summary)\s+(.*)$`)
)

// oracle: naive reference implementation in this test — the same session
// files are re-scanned through os.ReadDir + a generic map JSON decode + an
// EqualFold matcher, and the tool's rendered blocks (counts, order, roles,
// timestamps, marked spans) are compared against that reference.
func TestSessionSearchVsNaiveTranscriptScan_Differential(t *testing.T) {
	dir := t.TempDir()
	older := writeSession(t, dir, "/w", []llm.Message{
		{Role: llm.RoleUser, Content: "the goroutine leak is back"},
		{Role: llm.RoleAssistant, Content: "fixed the timer"},
	})
	newer, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
	require.NoError(t, err)
	_, err = newer.Append(llm.Message{Role: llm.RoleUser, Content: "The Goroutine LEAK appeared twice"})
	require.NoError(t, err)
	_, err = newer.Append(llm.Message{Role: llm.RoleAssistant, Content: "stopped the workers"})
	require.NoError(t, err)
	_, err = newer.Append(llm.Message{Role: llm.RoleTool, ToolCallID: "1", Content: "worker 3 hit a goroutine leak"})
	require.NoError(t, err)
	_, err = newer.AppendCompaction(session.Compaction{Summary: "recap: the goroutine leak was chased twice"})
	require.NoError(t, err)
	require.FileExists(t, newer.File())

	// Pin listing order so "newest first" is asserted, not inherited from
	// test speed.
	backOld := time.Now().Add(-3 * time.Hour)
	require.NoError(t, os.Chtimes(older.File(), backOld, backOld))
	backNew := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(newer.File(), backNew, backNew))

	const query = "goroutine leak"
	want := naiveTranscriptScan(t, dir, query)
	withHits := 0
	totalWant := 0
	for _, s := range want {
		if len(s.hits) > 0 {
			withHits++
			totalWant += len(s.hits)
		}
	}
	require.Equal(t, 2, withHits, "reference expects two matching sessions")
	require.Equal(t, 4, totalWant, "reference expects four matching messages")

	res, err := runTool(t, SessionSearchTool(dir, ""), map[string]any{"query": query})
	require.NoError(t, err)

	// Header line: scanned sessions, total matches, matching sessions.
	hm := searchHeaderRe.FindStringSubmatch(strings.SplitN(res.Content, "\n", 2)[0])
	require.NotNil(t, hm, "content must open with the summary header: %s", res.Content)
	assert.Equal(t, "2", hm[1])
	assert.Equal(t, fmt.Sprint(totalWant), hm[2])
	assert.Equal(t, fmt.Sprint(withHits), hm[3])

	// Blocks appear newest-first, one per matching session; rows carry the
	// role, HH:MM timestamp, and the marked span of every hit (≤ the
	// per-session cap, which these fixtures stay under).
	lines := strings.Split(res.Content, "\n")
	var blockSessions []naiveSession
	for _, s := range want {
		if len(s.hits) > 0 {
			blockSessions = append(blockSessions, s)
		}
	}
	bi := -1
	ri := 0
	for _, line := range lines {
		if bm := blockHeaderRe.FindStringSubmatch(line); bm != nil {
			bi++
			require.Less(t, bi, len(blockSessions), "more blocks than matching sessions")
			ri = 0
			total := fmt.Sprint(len(blockSessions[bi].hits))
			assert.Equal(t, total, bm[1], "block %d total matches", bi)
			assert.Empty(t, bm[2], "no (n of m shown) below the per-session cap")
			continue
		}
		if rm := hitRowRe.FindStringSubmatch(line); rm != nil {
			require.GreaterOrEqual(t, bi, 0, "hit row before any block header")
			require.Less(t, ri, len(blockSessions[bi].hits), "more rows than reference hits in block %d", bi)
			h := blockSessions[bi].hits[ri]
			assert.Equal(t, h.hhmm, rm[1], "block %d row %d time", bi, ri)
			assert.Equal(t, h.role, rm[2], "block %d row %d role", bi, ri)
			assert.Contains(t, rm[3], h.marked, "block %d row %d marked span", bi, ri)
			ri++
		}
	}
	assert.Equal(t, len(blockSessions), bi+1, "one block per matching session")
	assert.Equal(t, totalWant, countRows(t, res.Content), "every reference hit is rendered")
}

func countRows(t *testing.T, content string) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(content, "\n") {
		if hitRowRe.MatchString(line) {
			n++
		}
	}
	return n
}

// naiveSessionSearchDetail re-derives DetailFromArgs independently: a
// map-based JSON decode, the blank-query guard, the 40-byte cut, and %q.
func naiveSessionSearchDetail(input json.RawMessage) string {
	var m map[string]any
	_ = json.Unmarshal(input, &m)
	q, _ := m["query"].(string)
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	if len(q) > 40 {
		q = q[:40] + "..."
	}
	return fmt.Sprintf("session_search %q", q)
}

// oracle: naive reference implementation in this test — a map-based JSON
// decode plus hand-rolled truncation and formatting, compared over normal,
// edge, and hostile inputs.
func TestDetailFromArgs_Differential(t *testing.T) {
	long := strings.Repeat("needle", 10)
	unicode := strings.Repeat("過去のセッションを検索", 8)

	cases := []struct {
		name  string
		input string
	}{
		{"query plain", `{"query":"goroutine leak"}`},
		{"query padded", `{"query":"  goroutine leak  "}`},
		{"query long", `{"query":"` + long + `"}`},
		{"query multibyte", `{"query":"` + unicode + `"}`},
		{"blank query", `{"query":"   "}`},
		{"missing query", `{}`},
		{"non-string query", `{"query": 5}`},
		{"not json", `nope`},
	}
	tool := SessionSearchTool("/some/dir", "")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, naiveSessionSearchDetail(json.RawMessage(tc.input)), tool.DetailFromArgs(json.RawMessage(tc.input)))
		})
	}
}
