package maildir

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The trio for the Maildir reader. The unit under test is the library
// itself: the rail (filename-level counting), the listing (header
// parsing, ordering, the memo), and the single-message surfaces (Open
// with bodies, Summary without). The fixture is the committed golden
// Maildir tree; the differential adds a seeded generator whose plan is
// the oracle.

// goldenRoot resolves the committed fixture and pins the one file whose
// honest date is its MTIME: a git checkout stamps every file with clone
// time, so the no-Date message's fallback would otherwise depend on when
// the machine happened to run the tests. Pinning makes the expected
// order a property of the fixture, not of the checkout.
func goldenRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("testdata", "golden-maildir"))
	require.NoError(t, err)
	pin := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filepath.Join(root, "inbox", "new", "1102.ten@a1.host"), pin, pin))
	return root
}

// mkbox lays down a mailbox skeleton (all three halves).
func mkbox(t *testing.T, root, name string) {
	t.Helper()
	for _, sub := range []string{"cur", "new", "tmp"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, name, sub), 0o755))
	}
}

// fileSize is the honest cross-check for Size fields: the summary must
// report what the disk says, not what a parser reckons.
func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	require.NoError(t, err)
	return fi.Size()
}

// ---------- positive ----------

// TestGoldenRail_Positive pins the mailbox rail on the committed fixture:
// subdirectories with cur/ or new/ are mailboxes, sorted by name; a plain
// directory and stray files are not; tmp/ contents never count; and the
// drafts placeholder (.keep) is metadata, not a message.
func TestGoldenRail_Positive(t *testing.T) {
	boxes, err := Mailboxes(goldenRoot(t))
	require.NoError(t, err)
	require.Len(t, boxes, 3, "archive, drafts, inbox — nothing else qualifies")
	assert.Equal(t, "archive", boxes[0].Name)
	assert.Equal(t, "drafts", boxes[1].Name)
	assert.Equal(t, "inbox", boxes[2].Name)

	assert.Equal(t, 1, boxes[0].Total)
	assert.Equal(t, 0, boxes[0].Unread)

	assert.Equal(t, 0, boxes[1].Total, "the .keep placeholder is mailbox metadata, never a message")
	assert.Equal(t, 0, boxes[1].Unread)

	assert.Equal(t, 15, boxes[2].Total, "cur×12 + new×3; tmp/ is never counted")
	assert.Equal(t, 6, boxes[2].Unread, "new/×3 unseen by definition + cur without S×3 (1004, 1005, 1006)")
}

// TestGoldenList_Positive pins the inbox listing on the committed
// fixture: one unparsable blob skipped-with-count, the rest ordered
// latest-first with the id tiebreak, RFC 2047 words decoded, quoted
// display names with commas handled, and the flag letters read off the
// filenames.
func TestGoldenList_Positive(t *testing.T) {
	msgs, st, err := List(goldenRoot(t), "inbox", 0)
	require.NoError(t, err)
	require.Len(t, msgs, 14, "15 files − 1 unparsable")
	assert.Equal(t, 15, st.Files)
	assert.Equal(t, 1, st.Skipped, "the binary blob is counted, not hidden")
	assert.True(t, st.Degraded())

	wantOrder := []string{
		"1100", "1101", "1006", "1005", "1003", "1002", "1001",
		"1014", "1013", "1012", "1011", "1010", "1102", "1004",
	}
	got := make([]string, len(msgs))
	for i, m := range msgs {
		got[i] = strings.SplitN(m.ID, ".", 2)[0]
	}
	assert.Equal(t, wantOrder, got, "Date desc, id asc on ties; the no-Date message rides its pinned mtime")

	byID := make(map[string]Message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}

	one := byID["1001.one@a1.host"]
	assert.Equal(t, "Quarterly planning moved", one.Subject)
	assert.Equal(t, "Alice Engineer", one.From, "display name preferred over the address")
	assert.Equal(t, "user@example.org", one.To)
	assert.True(t, one.Seen)
	assert.False(t, one.Flagged)
	assert.Equal(t, fileSize(t, filepath.Join("testdata", "golden-maildir", "inbox", "cur", "1001.one@a1.host:2,S")), one.Size)

	two := byID["1002.two@a1.host"]
	assert.Equal(t, "Bob, Contractor", two.From, "a quoted display name containing a comma")
	assert.True(t, two.Answered, "the R flag")
	assert.True(t, two.Seen, "the S flag")

	three := byID["1003.three@a1.host"]
	assert.Empty(t, three.To, "no To header is an honest empty")
	assert.True(t, three.Flagged, "the F flag")

	four := byID["1004.four@a1.host"]
	assert.True(t, four.Trashed, "the T flag")
	assert.False(t, four.Seen, "trash is not seen")

	assert.False(t, byID["1005.five@a1.host"].Seen, "'2,' with nothing after the comma is no flags")
	assert.False(t, byID["1006.six@a1.host"].Seen, "a bare filename with no info suffix is no flags")

	eight := byID["1101.eight@a1.host"]
	assert.Equal(t, "田中太郎", eight.From, "RFC 2047 base64 display name decodes")
	assert.Equal(t, "会議の案内", eight.Subject, "RFC 2047 base64 subject decodes")
	assert.False(t, eight.Seen, "new/ is unseen whatever a stray suffix might claim")
}

// TestOpenBodies_Positive is the body matrix on the committed fixture:
// plain verbatim, multipart/alternative with text/plain winning, HTML
// stripped (script and style CONTENTS dropped, quoted attributes not
// ending tags early), quoted-printable soft breaks and hex escapes,
// base64 with line wrapping, and an attachment contributing nothing.
func TestOpenBodies_Positive(t *testing.T) {
	root := goldenRoot(t)

	one, err := Open(root, "inbox", "1001.one@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "The planning session moved to Thursday 10:00.\nBring the roadmap draft.\n", one.Body, "text/plain is verbatim, newline included")
	assert.False(t, one.WasHTML)
	assert.False(t, one.Truncated)

	eleven, err := Open(root, "inbox", "1010.eleven@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "Plain wins.", eleven.Body, "text/plain beats text/html in multipart/alternative; the newline before the boundary belongs to the boundary")
	assert.False(t, eleven.WasHTML)
	assert.NotContains(t, eleven.Body, "loses")
	assert.NotContains(t, eleven.Body, "alert", "script CONTENT is dropped, not just the tags")

	twelve, err := Open(root, "inbox", "1011.twelve@a1.host")
	require.NoError(t, err)
	assert.True(t, twelve.WasHTML)
	assert.Contains(t, twelve.Body, "Heading")
	assert.Contains(t, twelve.Body, "Some link text.", "a quoted '>' inside an attribute does not end the tag early")
	assert.NotContains(t, twelve.Body, "color:red", "style CONTENT is dropped")
	assert.NotContains(t, twelve.Body, "<", "no tags survive the strip")

	thirteen, err := Open(root, "inbox", "1012.thirteen@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "First line continued.\nCafé time.\n", thirteen.Body, "soft breaks join, =XX hex decodes")

	fourteen, err := Open(root, "inbox", "1013.fourteen@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "Hello from a base64 body, wrapped the way real mailers wrap it. Regards, Mia.",
		fourteen.Body, "base64 decodes across its line wrapping")

	fifteen, err := Open(root, "inbox", "1014.fifteen@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "The contract is attached.", fifteen.Body, "attachments contribute nothing; the text part wins")
	assert.NotContains(t, fifteen.Body, "JVBERi")

	// tmp/ is mid-delivery by spec: the file exists on disk, but no
	// surface may address it.
	_, err = Open(root, "inbox", "9999.zero@a1.host")
	assert.ErrorIs(t, err, ErrNoMessage, "tmp/ is invisible to Open")

	// Summary is Open minus the letter: the same header facts, no body.
	sum, err := Summary(root, "inbox", "1001.one@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "Quarterly planning moved", sum.Subject)
	assert.Equal(t, "Alice Engineer", sum.From)
	assert.True(t, sum.Seen)
	assert.Equal(t, one.Size, sum.Size, "Summary and Open agree on the file's facts")

	_, err = Summary(root, "inbox", "9999.zero@a1.host")
	assert.ErrorIs(t, err, ErrNoMessage, "tmp/ is invisible to Summary too")
}

// ---------- negative ----------

// TestMaildirHostileNames_Negative: traversal-shaped box and id names are
// refused before any filesystem call runs — that is the whole
// path-traversal story, and the error class proves it (a scan that had
// started would answer ErrNoSuchBox or ErrNoMessage instead).
func TestMaildirHostileNames_Negative(t *testing.T) {
	root := goldenRoot(t)
	for _, box := range []string{"", ".", "..", "../..", "inbox/cur", `inbox\cur`, "a\x00b", "..\\.."} {
		_, _, err := List(root, box, 0)
		assert.ErrorIs(t, err, ErrBadBox, "box %q", box)
		_, err = Open(root, box, "1001.one@a1.host")
		assert.ErrorIs(t, err, ErrBadBox, "box %q", box)
		_, err = Summary(root, box, "1001.one@a1.host")
		assert.ErrorIs(t, err, ErrBadBox, "box %q", box)
	}
	for _, id := range []string{"", ".", "..", "../../etc/passwd", "x/y", `x\y`, "a\x00b"} {
		_, err := Open(root, "inbox", id)
		assert.ErrorIs(t, err, ErrBadID, "id %q", id)
		_, err = Summary(root, "inbox", id)
		assert.ErrorIs(t, err, ErrBadID, "id %q", id)
	}

	// The drafts placeholder is unaddressable: validation lets the name
	// through, the scan never offers it as a message.
	_, err := Open(root, "drafts", ".keep")
	assert.ErrorIs(t, err, ErrNoMessage)
}

// TestMaildirMissingThings_Negative: a missing root is an empty rail
// (not-configured is a distinction one level up); a missing BOX is an
// error, not an empty mailbox — a typo must not read as "no mail"; a
// missing message in a real box is an honest miss.
func TestMaildirMissingThings_Negative(t *testing.T) {
	root := goldenRoot(t)

	boxes, err := Mailboxes(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err, "a missing root is an empty rail, not an error")
	assert.Empty(t, boxes)
	boxes, err = Mailboxes("   ")
	require.NoError(t, err)
	assert.Empty(t, boxes)

	_, _, err = List(root, "ghost", 0)
	assert.ErrorIs(t, err, ErrNoSuchBox)
	_, err = Open(root, "ghost", "1001.one@a1.host")
	assert.ErrorIs(t, err, ErrNoSuchBox)
	_, err = Summary(root, "ghost", "1001.one@a1.host")
	assert.ErrorIs(t, err, ErrNoSuchBox)

	_, err = Open(root, "inbox", "9999.ghost@a1.host")
	assert.ErrorIs(t, err, ErrNoMessage)
	_, err = Summary(root, "inbox", "9999.ghost@a1.host")
	assert.ErrorIs(t, err, ErrNoMessage)

	_, _, err = List("", "inbox", 0)
	assert.ErrorIs(t, err, ErrBadBox, "an unconfigured root refuses")
	_, err = Open("", "inbox", "1001.one@a1.host")
	assert.ErrorIs(t, err, ErrBadBox)
	_, err = Summary("", "inbox", "1001.one@a1.host")
	assert.ErrorIs(t, err, ErrBadBox)
}

// TestMaildirOversizedAndDegraded_Negative: the two size caps. A message
// past the whole-file cap answers headers-only with Truncated set — an
// honest refusal, never a silently short body. A file whose "headers"
// run past the header cap (no blank line within 128 KiB) is
// skipped-with-count, never slurped.
func TestMaildirOversizedAndDegraded_Negative(t *testing.T) {
	dir := t.TempDir()
	mkbox(t, dir, "big")

	f, err := os.Create(filepath.Join(dir, "big", "cur", "1.huge@a1.host:2,S"))
	require.NoError(t, err)
	_, err = f.WriteString("From: Big <big@example.dev>\nSubject: Huge\nDate: Sun, 1 Nov 2026 09:00:00 +0000\nContent-Type: text/plain\n\n")
	require.NoError(t, err)
	chunk := strings.Repeat("x", 1<<20)
	for i := 0; i <= int(MaxMessageBytes)/(1<<20); i++ { // 9 MiB > the 8 MiB cap
		_, err = f.WriteString(chunk)
		require.NoError(t, err)
	}
	require.NoError(t, f.Close())

	full, err := Open(dir, "big", "1.huge@a1.host")
	require.NoError(t, err)
	assert.Equal(t, "Huge", full.Subject, "headers survive the refusal")
	assert.True(t, full.Truncated, "the size cap refuses the body honestly")
	assert.Empty(t, full.Body)

	// ...and a Listing still shows it — the header prefix read is bounded
	// independently of the whole-file cap.
	msgs, st, err := List(dir, "big", 0)
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Huge", msgs[0].Subject)
	assert.False(t, st.Degraded(), "an oversized-but-parseable file is not a skip")

	// The runaway header block: 150 KiB of header lines, no blank line.
	mkbox(t, dir, "runaway")
	runaway := strings.Repeat("X-Filler: 0123456789abcdef\n", 8000) // ~152 KiB
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runaway", "cur", "2.pad@a1.host"), []byte(runaway), 0o644))
	msgs, st, err = List(dir, "runaway", 0)
	require.NoError(t, err)
	assert.Empty(t, msgs)
	assert.Equal(t, 1, st.Files)
	assert.Equal(t, 1, st.Skipped, "the header cap skips-with-count, never slurps")
	assert.True(t, st.Degraded())
}

// ---------- differential ----------

// TestGoldenInstants_Differential cross-checks every dated message in
// the fixture against TWO independent oracles: (a) a FixedZone
// construction of the header's wall clock, and (b) a hand-computed UTC
// literal. The literals pin the actual offset arithmetic:
//
//   - 1001: 09:15 +0100                 → 08:15Z
//   - 1002: 16:45 -0500                 → 21:45Z
//   - 1003: 12:00 +0000                 → 12:00Z
//   - 1101: Nov 8 01:10 +0900           → Nov 7 16:10Z (the day moves)
//   - 1102: no Date header              → the pinned mtime (Oct 31 12:00Z)
//
// A parser that dropped the zone, or read the fallback off the wrong
// clock, diverges from at least one oracle.
func TestGoldenInstants_Differential(t *testing.T) {
	msgs, _, err := List(goldenRoot(t), "inbox", 0)
	require.NoError(t, err)
	byID := make(map[string]Message, len(msgs))
	for _, m := range msgs {
		byID[m.ID] = m
	}

	for _, tc := range []struct {
		id      string
		wantUTC string // oracle (b): the hand-computed literal
	}{
		{"1001.one@a1.host", "2026-11-02T08:15:00Z"},
		{"1002.two@a1.host", "2026-11-03T21:45:00Z"},
		{"1003.three@a1.host", "2026-11-04T12:00:00Z"},
		{"1004.four@a1.host", "2026-10-26T03:00:00Z"},
		{"1101.eight@a1.host", "2026-11-07T16:10:00Z"},
		{"1102.ten@a1.host", "2026-10-31T12:00:00Z"},
	} {
		assert.Equal(t, tc.wantUTC, byID[tc.id].Date.UTC().Format(time.RFC3339),
			"%s: the hand-computed UTC literal", tc.id)
	}

	// Oracle (a): fixed-zone constructions of the same wall clocks.
	assert.True(t, byID["1001.one@a1.host"].Date.Equal(
		time.Date(2026, 11, 2, 9, 15, 0, 0, time.FixedZone("+0100", 3600))), "+0100 header")
	assert.True(t, byID["1002.two@a1.host"].Date.Equal(
		time.Date(2026, 11, 3, 16, 45, 0, 0, time.FixedZone("-0500", -5*3600))), "-0500 header")
	assert.True(t, byID["1101.eight@a1.host"].Date.Equal(
		time.Date(2026, 11, 8, 1, 10, 0, 0, time.FixedZone("+0900", 9*3600))), "+0900 header crosses the day boundary")
}

// TestListLimitPrefix_Differential: the limit is a PREFIX of the full
// latest-first order, for every k from 0 (all) to len+2 (oversize) —
// clamping and ordering must compose, not re-rank.
func TestListLimitPrefix_Differential(t *testing.T) {
	all, _, err := List(goldenRoot(t), "inbox", 0)
	require.NoError(t, err)
	require.Greater(t, len(all), 5)
	for k := 0; k <= len(all)+2; k++ {
		got, _, err := List(goldenRoot(t), "inbox", k)
		require.NoError(t, err)
		want := all
		if k > 0 && k < len(all) {
			want = all[:k]
		}
		assert.Equal(t, want, got, "limit %d is a prefix of the full order", k)
	}
}

// ---------- the seeded generator ----------

// genMsg is the plan's record of one message the generator writes: every
// fact the library must independently rediscover.
type genMsg struct {
	id      string
	info    string // "" or "2," + a subset of "SRFT"
	inNew   bool
	noDate  bool      // omit the Date header; the mtime below is the truth
	date    time.Time // the header instant, or the pinned mtime
	subject string
	mtime   time.Time
	size    int64
	// suffixed records whether the info suffix was actually written into
	// the filename: cur/ always carries it, new/ half the time — the
	// sloppy-tool case whose stray flags must still parse (Seen stays
	// forced false in new/, the rest are read).
	suffixed bool
}

// genBox is one planned mailbox plus its noise budget.
type genBox struct {
	name    string
	msgs    []genMsg
	garbage int // binary junk: counted in Total (filenames), skipped in List
	hasCur  bool
	hasNew  bool
}

// genTree writes a random-but-deterministic Maildir tree from an explicit
// plan and returns the rail it must produce. The plan IS the oracle: the
// expected Total/Unread counts and the expected listing (order, flags,
// dates, sizes) are all derived from what the generator wrote, not from
// reading it back.
func genTree(t *testing.T, rng *rand.Rand, root string) []genBox {
	t.Helper()
	names := []string{"alpha", "beta", "gamma", "delta", "inbox"}
	rng.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	nBoxes := 1 + rng.Intn(4)
	boxes := make([]genBox, 0, nBoxes)
	seq := 0
	dayPool := []time.Time{
		time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 12, 14, 30, 0, 0, time.UTC),
		time.Date(2026, 3, 12, 8, 0, 0, 0, time.UTC), // same day, earlier: ties happen
		time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 11, 15, 0, 0, time.UTC),
	}
	for b := 0; b < nBoxes; b++ {
		box := genBox{name: names[b], hasCur: rng.Intn(4) > 0, hasNew: rng.Intn(4) > 0}
		if !box.hasCur && !box.hasNew {
			box.hasNew = true // a mailbox needs at least one half
		}
		nMsgs := rng.Intn(7) // 0-6: empty boxes are legal
		for m := 0; m < nMsgs; m++ {
			seq++
			gm := genMsg{
				id:      fmt.Sprintf("m%03d.gen@a1", seq),
				subject: fmt.Sprintf("Subject %d", seq),
				date:    dayPool[rng.Intn(len(dayPool))],
			}
			// Flags: a third of the time no info suffix at all.
			if rng.Intn(3) == 0 {
				gm.info = ""
			} else {
				var fl strings.Builder
				for _, c := range []string{"S", "R", "F", "T"} {
					if rng.Intn(2) == 0 {
						fl.WriteString(c)
					}
				}
				gm.info = "2," + fl.String()
			}
			// Which half: cur (mostly) or new — new/ forces unseen.
			if box.hasNew && rng.Intn(3) == 0 {
				gm.inNew = true
			} else if !box.hasCur {
				gm.inNew = true
			}
			// A sixth of the messages omit Date; their mtime is pinned.
			gm.noDate = rng.Intn(6) == 0
			gm.mtime = dayPool[rng.Intn(len(dayPool))].Add(37 * time.Minute)
			// cur/ always carries the info suffix; new/ carries it half the
			// time (decided here so the plan's expectations know exactly
			// what the filename says).
			gm.suffixed = gm.info != "" && (!gm.inNew || rng.Intn(2) == 0)
			box.msgs = append(box.msgs, gm)
		}
		box.garbage = rng.Intn(3) // 0-2 junk files (always seen-flagged)
		boxes = append(boxes, box)
	}

	// Write the tree.
	for _, box := range boxes {
		dir := filepath.Join(root, box.name)
		if box.hasCur {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "cur"), 0o755))
		}
		if box.hasNew {
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "new"), 0o755))
		}
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "tmp"), 0o755))
		for _, gm := range box.msgs {
			half := "cur"
			if gm.inNew {
				half = "new"
			}
			name := gm.id
			if gm.suffixed {
				name += ":" + gm.info
			}
			var b strings.Builder
			fmt.Fprintf(&b, "From: Gen Sender <g%d@example.dev>\n", seq)
			b.WriteString("To: user@example.org\n")
			fmt.Fprintf(&b, "Subject: %s\n", gm.subject)
			if !gm.noDate {
				b.WriteString("Date: " + gm.date.Format(time.RFC1123Z) + "\n")
			}
			b.WriteString("Content-Type: text/plain\n\n")
			fmt.Fprintf(&b, "Body number %d.\n", seq)
			path := filepath.Join(dir, half, name)
			require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
			gm.size = int64(len(b.String()))
			// Write the size back into the plan record (the loop var is a
			// copy — find it again in the slice).
			for i := range box.msgs {
				if box.msgs[i].id == gm.id {
					box.msgs[i].size = gm.size
				}
			}
			if gm.noDate {
				require.NoError(t, os.Chtimes(path, gm.mtime, gm.mtime))
			}
		}
		for g := 0; g < box.garbage; g++ {
			half := "cur"
			if !box.hasCur {
				half = "new" // new/-only boxes get their junk in new/
			}
			require.NoError(t, os.WriteFile(
				filepath.Join(dir, half, fmt.Sprintf("junk%d.bin@a1:2,S", g)),
				[]byte{0x00, 0x01, 0xff, 0xfe, 'n', 'o', ' ', 'h', 'e', 'a', 'd', 'e', 'r', 's'}, 0o644))
		}
		// Noise the library must ignore: a dotfile, a subdirectory in
		// cur/, and a tmp/ decoy.
		if box.hasCur {
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cur", ".keep"), nil, 0o644))
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "cur", "nested"), 0o755))
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, "tmp", "9999.decoy@a1"),
			[]byte("From: Nobody <n@example.dev>\nSubject: decoy\n\nnever visible\n"), 0o644))
	}
	// Root noise: a stray file and a plain directory are not mailboxes.
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.txt"), []byte("not a mailbox\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "not-a-box"), 0o755))
	return boxes
}

// TestGeneratedTreesMatchModel_Differential is the seeded flagship: for
// each seed the generator writes a tree from an explicit plan and the
// library must independently arrive at the same answers — the rail's
// counts from filenames alone, and each box's listing: skip rules
// (dotfiles, subdirectories, tmp/, binary junk), flag parsing, the
// new/-unseen override, the no-Date mtime fallback, ordering (date desc,
// id asc), and sizes. Model versus implementation, twenty trees over.
func TestGeneratedTreesMatchModel_Differential(t *testing.T) {
	for seed := int64(1); seed <= 20; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			root := t.TempDir()
			plan := genTree(t, rng, root)

			// The rail: sorted names, Total = msgs + garbage, Unread =
			// new/ (always) + cur messages without S.
			boxes, err := Mailboxes(root)
			require.NoError(t, err)
			sorted := make([]genBox, len(plan))
			copy(sorted, plan)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].name < sorted[j].name })
			require.Len(t, boxes, len(sorted))
			for i, want := range sorted {
				assert.Equal(t, want.name, boxes[i].Name)
				total := len(want.msgs) + want.garbage
				unread := 0
				for _, gm := range want.msgs {
					if gm.inNew || !strings.Contains(gm.info, "S") {
						unread++
					}
				}
				if !want.hasCur {
					unread += want.garbage // junk in new/ counts unread: filenames only, no parsing
				}
				assert.Equal(t, total, boxes[i].Total, "%s: rail total counts filenames, junk included", want.name)
				assert.Equal(t, unread, boxes[i].Unread, "%s: rail unread = new/ + cur-without-S", want.name)
			}

			// The listings: junk skipped-with-count, dotfiles and decoys
			// invisible, order and every field per the plan.
			for _, want := range sorted {
				msgs, st, err := List(root, want.name, 0)
				require.NoError(t, err, "box %s", want.name)

				expected := make([]genMsg, len(want.msgs))
				copy(expected, want.msgs)
				// The effective date: the header instant, or the pinned
				// mtime when the plan omitted Date — the same rule the
				// library must apply, derived here from the plan alone.
				effective := func(gm genMsg) time.Time {
					if gm.noDate {
						return gm.mtime
					}
					return gm.date
				}
				sort.SliceStable(expected, func(i, j int) bool {
					di, dj := effective(expected[i]), effective(expected[j])
					if !di.Equal(dj) {
						return di.After(dj)
					}
					return expected[i].id < expected[j].id
				})

				require.Len(t, msgs, len(expected), "box %s: junk skipped, messages kept", want.name)
				assert.Equal(t, len(want.msgs)+want.garbage, st.Files, "box %s: stats see every file", want.name)
				if want.garbage > 0 {
					assert.Equal(t, want.garbage, st.Skipped, "box %s: junk is counted, not hidden", want.name)
					assert.True(t, st.Degraded())
				} else {
					assert.False(t, st.Degraded())
				}

				for i, gm := range expected {
					got := msgs[i]
					assert.Equal(t, gm.id, got.ID, "box %s[%d]: order is date desc, id asc", want.name, i)
					assert.Equal(t, gm.subject, got.Subject)
					assert.Equal(t, "Gen Sender", got.From)
					assert.Equal(t, "user@example.org", got.To)
					assert.Equal(t, gm.size, got.Size)
					seen := !gm.inNew && gm.suffixed && strings.Contains(gm.info, "S")
					assert.Equal(t, seen, got.Seen, "box %s[%d]: new/ forces unseen", want.name, i)
					assert.Equal(t, gm.suffixed && strings.Contains(gm.info, "R"), got.Answered,
						"box %s[%d]: answered per the filename", want.name, i)
					assert.Equal(t, gm.suffixed && strings.Contains(gm.info, "F"), got.Flagged,
						"box %s[%d]: flagged per the filename", want.name, i)
					assert.Equal(t, gm.suffixed && strings.Contains(gm.info, "T"), got.Trashed,
						"box %s[%d]: trashed per the filename", want.name, i)
					wantDate := gm.date
					if gm.noDate {
						wantDate = gm.mtime
					}
					assert.True(t, got.Date.Equal(wantDate), "box %s[%d]: date %s vs plan %s",
						want.name, i, got.Date.Format(time.RFC3339), wantDate.Format(time.RFC3339))
				}
			}
		})
	}
}
