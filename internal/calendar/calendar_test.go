package calendar

import (
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// goldenICS loads the shared fixture. The committed file uses bare LF —
// deliberately, so the LF path is exercised everywhere — and CRLFTwin
// derives the RFC-strict CRLF variant on the fly.
func goldenICS(t *testing.T) string {
        t.Helper()
        b, err := os.ReadFile(filepath.Join("testdata", "golden.ics"))
        require.NoError(t, err)
        return string(b)
}

func crlf(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }

// writeICS writes content under a fresh temp dir and returns the dir.
func writeICS(t *testing.T, name, content string) string {
        t.Helper()
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
        return dir
}

// findEvent returns the parsed event with the given UID.
func findEvent(t *testing.T, evs []Event, uid string) Event {
        t.Helper()
        for _, ev := range evs {
                if ev.UID == uid {
                        return ev
                }
        }
        t.Fatalf("event %q not found", uid)
        return Event{}
}

// ---------- plain unit coverage (the trio lives in trio_test.go) ----------

func TestUnfoldEdgeCases(t *testing.T) {
        lines, orphans := unfold("A:1\r\n B\r\n\tC\nD:2\n\n E")
        assert.Equal(t, []string{"A:1BC", "D:2E"}, lines, "space and tab continuations join after their one syntax char; the blank line drops")
        assert.Equal(t, 0, orphans)

        lines, orphans = unfold(" orphan")
        assert.Equal(t, []string{" orphan"}, lines)
        assert.Equal(t, 1, orphans, "a continuation before any line is an orphan")

        lines, _ = unfold("only line\n")
        assert.Equal(t, []string{"only line"}, lines, "trailing newline is not a blank line")
}

func TestEscapeRoundTripUnit(t *testing.T) {
        for _, s := range []string{
                "plain", `back\slash`, "semi;colon", "com,ma", "line\nbreak", `mixed \;,\ text`,
                "日本語のテキスト、テスト", "", "trailing\\", `\n\N\\`,
        } {
                assert.Equal(t, s, UnescapeText(EscapeText(s)), "round trip for %q", s)
        }
        assert.Equal(t, "a\nb", UnescapeText(`a\nb`), "lowercase n escape")
        assert.Equal(t, "a\nb", UnescapeText(`a\Nb`), "uppercase N escape")
        assert.Equal(t, `a\qb`, UnescapeText(`a\qb`), "unknown escape keeps both bytes")
}

func TestParseDurationUnit(t *testing.T) {
        for _, tc := range []struct {
                in   string
                want time.Duration
        }{
                {"PT1H30M", 90 * time.Minute},
                {"P1D", 24 * time.Hour},
                {"P2W", 14 * 24 * time.Hour},
                {"PT15M", 15 * time.Minute},
                {"PT45S", 45 * time.Second},
                {"P1DT2H", 26 * time.Hour},
                {"-PT1H", -time.Hour},
                {"+PT1H", time.Hour},
        } {
                got, err := ParseDuration(tc.in)
                require.NoError(t, err, tc.in)
                assert.Equal(t, tc.want, got, tc.in)
        }
        for _, bad := range []string{"", "P", "PT", "1H", "PT1", "P1M", "PT1H1", "XPT1H", "PT1D"} {
                _, err := ParseDuration(bad)
                assert.Error(t, err, bad)
        }
}

func TestFoldNeverSplitsRunes(t *testing.T) {
        // A 300-rune CJK string: every rune is 3 bytes, so a naive byte cut
        // at 75 would land mid-rune more often than not.
        long := strings.Repeat("祝", 300)
        var buf []byte
        buf = appendFolded(buf, "SUMMARY:"+long)
        physical := strings.Split(strings.TrimSuffix(string(buf), "\r\n"), "\r\n")
        for i, p := range physical {
                assert.LessOrEqual(t, len(p), 75, "physical line %d within the octet cap", i)
                if i > 0 {
                        assert.Equal(t, byte(' '), p[0], "continuations start with a space")
                }
        }
        // The unfold of the folded form is the original logical line.
        lines, orphans := unfold(string(buf))
        require.Equal(t, 1, len(lines))
        assert.Equal(t, 0, orphans)
        assert.Equal(t, "SUMMARY:"+long, lines[0])
}

func TestParseCRLFAndLFAgree(t *testing.T) {
        lfEvents, lfStats := Parse(goldenICS(t))
        crlfEvents, crlfStats := Parse(crlf(goldenICS(t)))
        require.Len(t, lfEvents, len(crlfEvents))
        for i := range lfEvents {
                assert.Equal(t, lfEvents[i].Summary, crlfEvents[i].Summary)
                assert.True(t, lfEvents[i].Start.Equal(crlfEvents[i].Start), "%s", lfEvents[i].UID)
        }
        assert.Equal(t, lfStats, crlfStats)
}

func TestScanMemoFollowsRewrites(t *testing.T) {
        dir := writeICS(t, "one.ics", "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20261105T100000Z\r\nSUMMARY:first\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
        evs, st, err := Scan(dir)
        require.NoError(t, err)
        require.Len(t, evs, 1)
        require.Equal(t, 1, st.Files)

        // Same length, different content: only the mtime moves. A short sleep
        // clears the coarse inode-timestamp tick so the external rewrite is
        // genuinely later (our OWN writes don't need this — they invalidate
        // the memo directly).
        time.Sleep(60 * time.Millisecond)
        require.NoError(t, os.WriteFile(filepath.Join(dir, "one.ics"), []byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:u1\r\nDTSTART:20261105T100000Z\r\nSUMMARY:secnd\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"), 0o644))
        evs, _, err = Scan(dir)
        require.NoError(t, err)
        require.Len(t, evs, 1)
        assert.Equal(t, "secnd", evs[0].Summary, "a same-size rewrite must not be served from the memo")

        // Deleted file: the memo must not resurrect it.
        require.NoError(t, os.Remove(filepath.Join(dir, "one.ics")))
        evs, st, err = Scan(dir)
        require.NoError(t, err)
        assert.Empty(t, evs)
        assert.Equal(t, 0, st.Files, "the walk counts only files that exist")
}
