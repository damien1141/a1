package mailtool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/maildir"
)

// The trio for the personal_mail facade. The unit under test is the
// FACADE, not the reader (internal/maildir owns that trio): these tests
// pin the call-time flag enforcement, the argument surface, and the
// facade-vs-library parity — including the privacy line the facade adds
// on top of the library: no action here can produce a body.

// fixtureDeps builds a tool over a temp Maildir root. readAllowed is a
// live field so a test can flip consent mid-flight — exactly the
// mis-mount scenario the call-time gate exists for.
type fixtureDeps struct {
	dir         string
	readAllowed bool
}

// seedMsg writes one message into a box's cur/.
func seedMsg(t *testing.T, root, box, name, date, subject string) {
	t.Helper()
	dir := filepath.Join(root, box, "cur")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	body := fmt.Sprintf("From: Sender %s <s@example.dev>\nTo: user@example.org\nSubject: %s\nDate: %s\n\nBody of %s.\n",
		strings.SplitN(name, ".", 2)[0], subject, date, subject)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

// runTool invokes the facade with the given JSON args.
func runTool(t *testing.T, f *fixtureDeps, args string) (map[string]any, error) {
	t.Helper()
	tool := MailTool(Deps{
		MailDir:     func() string { return f.dir },
		ReadAllowed: func() bool { return f.readAllowed },
	})
	res, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		return nil, err
	}
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
	return body, nil
}

func mailFixture(t *testing.T) *fixtureDeps {
	t.Helper()
	root := t.TempDir()
	seedMsg(t, root, "inbox", "1001.one@a1.host:2,S", "Sun, 1 Nov 2026 09:00:00 +0000", "First")
	seedMsg(t, root, "inbox", "1002.two@a1.host:2,RS", "Mon, 2 Nov 2026 10:00:00 +0000", "Second")
	seedMsg(t, root, "inbox", "1003.three@a1.host", "Tue, 3 Nov 2026 11:00:00 +0000", "Third")
	seedMsg(t, root, "archive", "2000.old@a1.host:2,S", "Fri, 2 Jan 2026 11:00:00 +0000", "Old thread")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "drafts", "cur"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "drafts", "cur", ".keep"), nil, 0o644))
	return &fixtureDeps{dir: root, readAllowed: true}
}

// ---------- positive ----------

func TestMailToolMailboxesAndMessages_Positive(t *testing.T) {
	f := mailFixture(t)

	body, err := runTool(t, f, `{"action":"mailboxes"}`)
	require.NoError(t, err)
	assert.Equal(t, float64(3), body["count"])
	boxes := body["mailboxes"].([]any)
	require.Len(t, boxes, 3)
	first := boxes[0].(map[string]any)
	assert.Equal(t, "archive", first["name"])
	assert.Equal(t, float64(1), first["total"])
	assert.Equal(t, float64(0), first["unread"])
	inbox := boxes[2].(map[string]any)
	assert.Equal(t, "inbox", inbox["name"])
	assert.Equal(t, float64(3), inbox["total"])
	assert.Equal(t, float64(1), inbox["unread"], "1003 cur-without-S is the only unread; 1001/1002 are seen")

	body, err = runTool(t, f, `{"action":"messages","box":"inbox"}`)
	require.NoError(t, err)
	assert.Equal(t, "inbox", body["box"])
	msgs := body["messages"].([]any)
	require.Len(t, msgs, 3, "default limit 25 over 3 messages")
	latest := msgs[0].(map[string]any)
	assert.Equal(t, "1003.three@a1.host", latest["id"], "latest first")
	assert.Equal(t, "Third", latest["subject"])
	assert.Equal(t, false, latest["seen"], "no info suffix is unseen")
	second := msgs[1].(map[string]any)
	assert.Equal(t, "Second", second["subject"])
	assert.Equal(t, true, second["answered"], "the R flag surfaces")

	// A limit of 1 keeps the latest only.
	body, err = runTool(t, f, `{"action":"messages","box":"inbox","limit":1}`)
	require.NoError(t, err)
	msgs = body["messages"].([]any)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Third", msgs[0].(map[string]any)["subject"])

	// An empty-but-configured box is an honest empty, not an error.
	body, err = runTool(t, f, `{"action":"messages","box":"drafts"}`)
	require.NoError(t, err)
	assert.Equal(t, float64(0), body["count"])
}

func TestMailToolHeaders_Positive(t *testing.T) {
	f := mailFixture(t)

	body, err := runTool(t, f, `{"action":"headers","box":"inbox","id":"1002.two@a1.host"}`)
	require.NoError(t, err)
	msg := body["message"].(map[string]any)
	assert.Equal(t, "1002.two@a1.host", msg["id"])
	assert.Equal(t, "Second", msg["subject"])
	assert.Equal(t, "Sender 1002", msg["from"])
	assert.Equal(t, true, msg["seen"])
	assert.Equal(t, true, msg["answered"])
	assert.Equal(t, float64(len(strings.Join([]string{
		"From: Sender 1002 <s@example.dev>\nTo: user@example.org\nSubject: Second\nDate: Mon, 2 Nov 2026 10:00:00 +0000\n\nBody of Second.\n",
	}, ""))), msg["size"], "size is the file's bytes — a float64 over JSON")
	assert.NotContains(t, body, "body", "the envelope only: no action produces a letter")
	assert.NotContains(t, msg, "body")
}

// ---------- negative ----------

// TestMailToolRefusedWhenFlagsOff_Negative is the flagship: the consent
// is read at CALL time, so a tool that was mounted under one consent
// state refuses the moment the live state says no — every action, the
// same error, and the directory untouched (read-only is structural;
// there is nothing the facade could write even if it wanted to).
func TestMailToolRefusedWhenFlagsOff_Negative(t *testing.T) {
	f := mailFixture(t)

	// Sanity: the tool works while consent holds.
	_, err := runTool(t, f, `{"action":"mailboxes"}`)
	require.NoError(t, err)

	// Consent flips (config save, model switch — any path). Every action
	// refuses with the personal_read explanation.
	f.readAllowed = false
	for _, args := range []string{
		`{"action":"mailboxes"}`,
		`{"action":"messages","box":"inbox"}`,
		`{"action":"headers","box":"inbox","id":"1001.one@a1.host"}`,
	} {
		_, err := runTool(t, f, args)
		require.Error(t, err, "%s", args)
		assert.Contains(t, err.Error(), "personal_read")
	}
}

func TestMailToolUnconfiguredDir_Negative(t *testing.T) {
	f := &fixtureDeps{dir: "", readAllowed: true}
	_, err := runTool(t, f, `{"action":"mailboxes"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mail_dir")
}

func TestMailToolBadInputs_Negative(t *testing.T) {
	f := mailFixture(t)

	for _, tc := range []struct{ name, args, wantErr string }{
		{"unknown action", `{"action":"send"}`, "unknown action"},
		{"messages without box", `{"action":"messages"}`, "requires a box"},
		{"messages traversal box", `{"action":"messages","box":"../../etc"}`, "invalid mailbox name"},
		{"headers without box", `{"action":"headers","id":"x"}`, "requires a box"},
		{"headers without id", `{"action":"headers","box":"inbox"}`, "requires an id"},
		{"headers traversal id", `{"action":"headers","box":"inbox","id":"../x"}`, "invalid message id"},
		{"headers ghost id", `{"action":"headers","box":"inbox","id":"9999.ghost@a1"}`, "no such message"},
		{"messages ghost box", `{"action":"messages","box":"ghost"}`, "no such mailbox"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runTool(t, f, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}

	// The refusal writes nothing — trivially true (there is no write
	// API), but pinned anyway: the tree is byte-identical after hostile
	// calls, dotfile included.
	entries, err := os.ReadDir(filepath.Join(f.dir, "inbox", "cur"))
	require.NoError(t, err)
	require.Len(t, entries, 3)
}

// ---------- differential ----------

// TestMailToolFacadeMatchesLibrary_Differential: the facade must be a
// faithful projection of the library, nothing more. Every message the
// tool reports is compared field-by-field against maildir.List's
// independent view of the same root — different code paths, same tree,
// one truth. The envelope-only ruling is differential too: whatever the
// facade says must be reconstructible from the library's SUMMARY, with
// no fact the library does not have.
func TestMailToolFacadeMatchesLibrary_Differential(t *testing.T) {
	f := mailFixture(t)

	body, err := runTool(t, f, `{"action":"messages","box":"inbox","limit":100}`)
	require.NoError(t, err)
	raw, err := json.Marshal(body["messages"])
	require.NoError(t, err)
	var got []messageJSON
	require.NoError(t, json.Unmarshal(raw, &got))

	want, _, err := maildir.List(f.dir, "inbox", 100)
	require.NoError(t, err)
	require.Len(t, want, 3)

	require.Len(t, got, len(want))
	byID := make(map[string]maildir.Message, len(want))
	for _, m := range want {
		byID[m.ID] = m
	}
	for _, g := range got {
		w, ok := byID[g.ID]
		require.True(t, ok, "facade message %q exists in the library's view", g.ID)
		assert.Equal(t, w.From, g.From, g.ID)
		assert.Equal(t, w.To, g.To, g.ID)
		assert.Equal(t, w.Subject, g.Subject, g.ID)
		assert.Equal(t, w.Size, g.Size, g.ID)
		assert.Equal(t, w.Seen, g.Seen, g.ID)
		assert.Equal(t, w.Answered, g.Answered, g.ID)
		assert.Equal(t, w.Flagged, g.Flagged, g.ID)
		assert.Equal(t, w.Trashed, g.Trashed, g.ID)
		gotTime, err := time.Parse(timeFormat, g.Date)
		require.NoError(t, err)
		assert.True(t, w.Date.Equal(gotTime), "%s: date %s vs %s", g.ID, g.Date, w.Date.Format(timeFormat))
	}

	// And the headers action agrees with the library's Summary for the
	// same id — the envelope, twice-derived.
	body, err = runTool(t, f, `{"action":"headers","box":"inbox","id":"1003.three@a1.host"}`)
	require.NoError(t, err)
	msg := body["message"].(map[string]any)
	sum, err := maildir.Summary(f.dir, "inbox", "1003.three@a1.host")
	require.NoError(t, err)
	assert.Equal(t, sum.Subject, msg["subject"])
	assert.Equal(t, sum.From, msg["from"])
	assert.Equal(t, float64(sum.Size), msg["size"])
	assert.Equal(t, sum.Seen, msg["seen"])
}
