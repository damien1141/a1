package webserver

import (
        "encoding/json"
        "fmt"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/maildir"
)

// The mail endpoints' trio (dashboard chunk 4). The reader's own trio
// lives in internal/maildir; this file pins the HTTP layer: the gate
// composition, the hostile-input doctrine (fallbacks where one exists,
// honest 400/404s where one does not), and endpoint-vs-library parity.

// seedMailFixture fills root with the standard fixture set: an inbox
// with three dated messages (one unseen, one answered), an archive with
// one, a drafts box whose only content is a .keep placeholder, and one
// binary junk file the scans must count but never serve.
func seedMailFixture(t *testing.T, root string) {
        t.Helper()
        msg := func(name, date, subject string) string {
                return fmt.Sprintf("From: Sender <%s@example.dev>\nTo: user@example.org\nSubject: %s\nDate: %s\n\nBody of %s.\n",
                        name, subject, date, subject)
        }
        require.NoError(t, os.MkdirAll(filepath.Join(root, "inbox", "cur"), 0o755))
        require.NoError(t, os.MkdirAll(filepath.Join(root, "inbox", "new"), 0o755))
        require.NoError(t, os.MkdirAll(filepath.Join(root, "archive", "cur"), 0o755))
        require.NoError(t, os.MkdirAll(filepath.Join(root, "drafts", "cur"), 0o755))
        require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "cur", "1001.one@a1.host:2,S"),
                []byte(msg("one", "Sun, 1 Nov 2026 09:00:00 +0000", "First")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "cur", "1002.two@a1.host:2,RS"),
                []byte(msg("two", "Mon, 2 Nov 2026 10:00:00 +0000", "Second")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "new", "1003.three@a1.host"),
                []byte(msg("three", "Tue, 3 Nov 2026 11:00:00 +0000", "Third")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "archive", "cur", "2000.old@a1.host:2,S"),
                []byte(msg("old", "Fri, 2 Jan 2026 11:00:00 +0000", "Old thread")), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "drafts", "cur", ".keep"), nil, 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(root, "inbox", "cur", "1099.junk@a1.host:2,S"),
                []byte{0x00, 0x01, 0xff, 0xfe, 'n', 'o', ' ', 'h', 'e', 'a', 'd', 'e', 'r', 's'}, 0o644))
}

// newMailServer builds a fixture project with the personal section
// pointing at the seeded root.
func newMailServer(t *testing.T, root string) *Server {
        t.Helper()
        return newPersonalFixtureProject(t, fmt.Sprintf("personal:\n  enabled: true\n  mail_dir: %s\n", root))
}

func getMail(t *testing.T, srv *Server, path string) (int, string) {
        t.Helper()
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
        return rec.Code, rec.Body.String()
}

// ---------- positive ----------

func TestMailEndpointsServeFixture_Positive(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newMailServer(t, root)

        // The rail: three boxes, the placeholder not a message, the junk
        // counted in inbox's total (filename-level counting).
        code, raw := getMail(t, srv, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        assert.True(t, rail.Configured)
        require.Len(t, rail.Mailboxes, 3)
        assert.Equal(t, mailboxJSON{Name: "archive", Total: 1, Unread: 0}, rail.Mailboxes[0])
        assert.Equal(t, mailboxJSON{Name: "drafts", Total: 0, Unread: 0}, rail.Mailboxes[1], "the .keep placeholder is not mail")
        assert.Equal(t, mailboxJSON{Name: "inbox", Total: 4, Unread: 1}, rail.Mailboxes[2], "junk counts; only the new/ message is unread")

        // The list: latest first, junk skipped-with-count, headers only.
        code, raw = getMail(t, srv, "/api/personal/mail/messages?box=inbox")
        require.Equal(t, http.StatusOK, code)
        var list mailMessagesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.True(t, list.Configured)
        assert.Equal(t, "inbox", list.Box)
        require.Len(t, list.Messages, 3, "the binary junk is skipped, never served")
        assert.Equal(t, "1003.three@a1.host", list.Messages[0].ID, "latest first")
        assert.False(t, list.Messages[0].Seen, "new/ is unseen")
        assert.True(t, list.Messages[1].Answered, "the R flag surfaces")
        assert.Empty(t, list.Messages[1].Body, "listings are headers-only")
        assert.Equal(t, 4, list.Stats.Files)
        assert.Equal(t, 1, list.Stats.Skipped)
        assert.True(t, list.Stats.Degraded)

        // The read pane: one message with its body.
        code, raw = getMail(t, srv, "/api/personal/mail/message?box=inbox&id=1003.three@a1.host")
        require.Equal(t, http.StatusOK, code)
        var full mailMessageResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &full))
        assert.True(t, full.Configured)
        assert.Equal(t, "Third", full.Message.Subject)
        assert.Equal(t, "Body of Third.\n", full.Message.Body, "the web reads bodies — the one face the agent never gets")
        assert.False(t, full.Message.Truncated)
}

// ---------- negative ----------

func TestMailGatedOff_Negative(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newPersonalFixtureProject(t, fmt.Sprintf("personal:\n  enabled: false\n  mail_dir: %s\n", root))
        for _, path := range []string{
                "/api/personal/mail/mailboxes",
                "/api/personal/mail/messages?box=inbox",
                "/api/personal/mail/message?box=inbox&id=1001.one@a1.host",
        } {
                code, _ := getMail(t, srv, path)
                assert.Equal(t, http.StatusForbidden, code, "%s", path)
        }
}

func TestMailUnconfiguredRoot_Negative(t *testing.T) {
        // Enabled but no mail_dir: every endpoint answers the card's
        // not-configured state, 200s with configured:false — no gated fetch
        // ever needs to error.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")
        for _, path := range []string{
                "/api/personal/mail/mailboxes",
                "/api/personal/mail/messages?box=inbox",
                "/api/personal/mail/message?box=inbox&id=x",
        } {
                code, raw := getMail(t, srv, path)
                require.Equal(t, http.StatusOK, code, "%s", path)
                assert.Contains(t, raw, `"configured":false`, "%s", path)
        }
}

func TestMailHostileInputs_Negative(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newMailServer(t, root)

        // limit falls back and clamps, never errors.
        for _, raw := range []string{"", "abc", "0", "-5", "99999999999999999999", "10000", "2"} {
                code, body := getMail(t, srv, "/api/personal/mail/messages?box=inbox&limit="+raw)
                require.Equal(t, http.StatusOK, code, "limit=%q never errors", raw)
                var list mailMessagesResponse
                require.NoError(t, json.Unmarshal([]byte(body), &list))
                if raw == "2" {
                        assert.Len(t, list.Messages, 2, "limit=2 is honored")
                } else {
                        assert.Len(t, list.Messages, 3, "limit=%q serves the default page", raw)
                }
        }

        // Traversal-shaped names are 400s; missing things are 404s; a
        // missing box param is a 400. Nothing is ever a 500.
        for _, tc := range []struct {
                path string
                want int
        }{
                {"/api/personal/mail/messages", http.StatusBadRequest},
                {"/api/personal/mail/messages?box=", http.StatusBadRequest},
                {"/api/personal/mail/messages?box=../../etc", http.StatusBadRequest},
                {"/api/personal/mail/messages?box=inbox%2Fcur", http.StatusBadRequest},
                {"/api/personal/mail/messages?box=ghost", http.StatusNotFound},
                {"/api/personal/mail/message?box=inbox", http.StatusBadRequest},
                {"/api/personal/mail/message?id=1001.one@a1.host", http.StatusBadRequest},
                {"/api/personal/mail/message?box=inbox&id=../x", http.StatusBadRequest},
                {"/api/personal/mail/message?box=inbox&id=9999.ghost@a1.host", http.StatusNotFound},
                {"/api/personal/mail/message?box=ghost&id=x", http.StatusNotFound},
        } {
                code, _ := getMail(t, srv, tc.path)
                assert.Equal(t, tc.want, code, "%s", tc.path)
        }

        // A configured-but-missing root is an empty rail, not an error (the
        // widget distinguishes "not configured" via status, "no boxes" via
        // this).
        missing := filepath.Join(t.TempDir(), "does-not-exist")
        srv2 := newMailServer(t, missing)
        code, raw := getMail(t, srv2, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        assert.True(t, rail.Configured)
        assert.Empty(t, rail.Mailboxes)
}

func TestMailDirEmptiedAtRuntime_Negative(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newMailServer(t, root)

        code, raw := getMail(t, srv, "/api/personal/mail/messages?box=inbox")
        require.Equal(t, http.StatusOK, code)
        var list mailMessagesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        require.NotEmpty(t, list.Messages)

        // The box emptied at runtime serves empty, not stale.
        require.NoError(t, os.Remove(filepath.Join(root, "inbox", "cur", "1001.one@a1.host:2,S")))
        require.NoError(t, os.Remove(filepath.Join(root, "inbox", "cur", "1002.two@a1.host:2,RS")))
        require.NoError(t, os.Remove(filepath.Join(root, "inbox", "cur", "1099.junk@a1.host:2,S")))
        require.NoError(t, os.Remove(filepath.Join(root, "inbox", "new", "1003.three@a1.host")))
        code, raw = getMail(t, srv, "/api/personal/mail/messages?box=inbox")
        require.Equal(t, http.StatusOK, code)
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.Empty(t, list.Messages, "an emptied box serves an empty list, not a stale one")
        assert.Equal(t, 0, list.Stats.Files)
}

// ---------- differential ----------

// TestMailEndpointMatchesLibrary_Differential: the endpoints must be a
// faithful projection of the library — the same fixture tree through
// two implementations (HTTP handlers vs direct maildir calls), compared
// entry-by-entry, plus the limit-prefix law over the wire.
func TestMailEndpointMatchesLibrary_Differential(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newMailServer(t, root)

        // The rail vs maildir.Mailboxes.
        code, raw := getMail(t, srv, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        wantBoxes, err := maildir.Mailboxes(root)
        require.NoError(t, err)
        require.Len(t, rail.Mailboxes, len(wantBoxes))
        for i, want := range wantBoxes {
                assert.Equal(t, want.Name, rail.Mailboxes[i].Name)
                assert.Equal(t, want.Total, rail.Mailboxes[i].Total)
                assert.Equal(t, want.Unread, rail.Mailboxes[i].Unread)
        }

        // The list vs maildir.List, message-by-message, and the limit-prefix
        // law: every limit is a prefix of the full order.
        full, _, err := maildir.List(root, "inbox", 0)
        require.NoError(t, err)
        for _, q := range []string{
                "/api/personal/mail/messages?box=inbox",
                "/api/personal/mail/messages?box=inbox&limit=1",
                "/api/personal/mail/messages?box=inbox&limit=2",
                "/api/personal/mail/messages?box=inbox&limit=10000",
        } {
                t.Run(q, func(t *testing.T) {
                        code, raw := getMail(t, srv, q)
                        require.Equal(t, http.StatusOK, code)
                        var list mailMessagesResponse
                        require.NoError(t, json.Unmarshal([]byte(raw), &list))
                        want := full
                        if len(list.Messages) > 0 && len(list.Messages) < len(full) {
                                want = full[:len(list.Messages)]
                        }
                        require.Len(t, list.Messages, len(want))
                        for i, m := range list.Messages {
                                assert.Equal(t, want[i].ID, m.ID)
                                assert.Equal(t, want[i].From, m.From)
                                assert.Equal(t, want[i].To, m.To)
                                assert.Equal(t, want[i].Subject, m.Subject)
                                assert.Equal(t, want[i].Size, m.Size)
                                assert.Equal(t, want[i].Seen, m.Seen)
                                assert.Equal(t, want[i].Answered, m.Answered)
                                assert.Equal(t, want[i].Flagged, m.Flagged)
                                assert.Equal(t, want[i].Trashed, m.Trashed)
                                gotDate, err := time.Parse(time.RFC3339, m.Date)
                                require.NoError(t, err)
                                assert.True(t, want[i].Date.Equal(gotDate), "%s: date %s vs %s", m.ID, m.Date, want[i].Date.Format(time.RFC3339))
                        }
                })
        }

        // The read pane vs maildir.Open — body included, the web's one
        // privilege over the agent's facade.
        code, raw = getMail(t, srv, "/api/personal/mail/message?box=archive&id=2000.old@a1.host")
        require.Equal(t, http.StatusOK, code)
        var fullResp mailMessageResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &fullResp))
        wantFull, err := maildir.Open(root, "archive", "2000.old@a1.host")
        require.NoError(t, err)
        assert.Equal(t, wantFull.Subject, fullResp.Message.Subject)
        assert.Equal(t, wantFull.Body, fullResp.Message.Body)
        assert.Equal(t, wantFull.WasHTML, fullResp.Message.WasHTML)
        assert.Equal(t, wantFull.Truncated, fullResp.Message.Truncated)
}

// ---------- multi-account (chunk 7) ----------

// newMultiMailServer builds a fixture project with the legacy mail_dir
// AND two named maildir accounts — the resolved list the widget sees is
// [local, second, named].
func newMultiMailServer(t *testing.T, root, second string) *Server {
        t.Helper()
        return newPersonalFixtureProject(t, fmt.Sprintf(
                "personal:\n  enabled: true\n  mail_dir: %s\n  mail_accounts:\n    - name: second\n      root: %s\n    - name: named\n      root: %s\n",
                root, second, second))
}

func TestMailMultiAccountRail_Positive(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        srv := newMultiMailServer(t, root, second)

        // The default rail (no ?account=): the first resolved account — the
        // legacy "local" — and the account metadata for the tabs.
        code, raw := getMail(t, srv, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        assert.True(t, rail.Configured)
        assert.Equal(t, "local", rail.Account, "the first resolved account is the default")
        require.Len(t, rail.Accounts, 3)
        assert.Equal(t, mailAccountJSON{Name: "local", Type: "maildir"}, rail.Accounts[0])
        assert.Equal(t, mailAccountJSON{Name: "second", Type: "maildir"}, rail.Accounts[1])
        assert.Equal(t, mailAccountJSON{Name: "named", Type: "maildir"}, rail.Accounts[2])
        require.Len(t, rail.Mailboxes, 3, "the fixture's three boxes")

        // Selecting by name reads that account's own root.
        code, raw = getMail(t, srv, "/api/personal/mail/mailboxes?account=named")
        require.Equal(t, http.StatusOK, code)
        var named mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &named))
        assert.Equal(t, "named", named.Account)
        require.Len(t, named.Mailboxes, 3, "the second root carries the same fixture")

        // The list and the read pane honor ?account= too.
        code, raw = getMail(t, srv, "/api/personal/mail/messages?account=named&box=inbox")
        require.Equal(t, http.StatusOK, code)
        var list mailMessagesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.Equal(t, "named", list.Account)
        require.Len(t, list.Messages, 3)

        code, raw = getMail(t, srv, "/api/personal/mail/message?account=second&box=inbox&id=1003.three@a1.host")
        require.Equal(t, http.StatusOK, code)
        var full mailMessageResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &full))
        assert.Equal(t, "second", full.Account)
        assert.Equal(t, "Third", full.Message.Subject)
}

// TestMailAccountsOutageRendersNotBlanks_Negative: a dead IMAP account
// answers the rail with its error attached (200), so the widget shows
// the other accounts and the reason — never a blank card; the deeper
// reads (list, open) answer 502, the config-pointed-somewhere-dead
// status.
func TestMailAccountsOutageRendersNotBlanks_Negative(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        srv := newPersonalFixtureProject(t, fmt.Sprintf(
                "personal:\n  enabled: true\n  mail_dir: %s\n  mail_accounts:\n    - name: remote\n      type: imap\n      host: 127.0.0.1\n      port: 1\n      username: u\n      password: p\n      tls: false\n",
                root))

        // The rail for the dead account: 200, the error rides the account
        // entry, the healthy account's entry stays clean.
        code, raw := getMail(t, srv, "/api/personal/mail/mailboxes?account=remote")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        assert.True(t, rail.Configured)
        require.Len(t, rail.Accounts, 2)
        assert.NotEmpty(t, rail.Accounts[1].Error, "the remote account carries its outage")
        assert.Empty(t, rail.Accounts[0].Error, "the local account stays clean")
        assert.NotEmpty(t, rail.Error)
        assert.Empty(t, rail.Mailboxes)

        // The default rail (no ?account=) is the healthy local account.
        code, raw = getMail(t, srv, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var healthy mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &healthy))
        assert.Empty(t, healthy.Error)
        assert.Len(t, healthy.Mailboxes, 3, "the healthy default serves its boxes")

        // An unknown account name is a client mistake: 400.
        code, _ = getMail(t, srv, "/api/personal/mail/mailboxes?account=ghost")
        assert.Equal(t, http.StatusBadRequest, code)
        code, _ = getMail(t, srv, "/api/personal/mail/messages?account=ghost&box=inbox")
        assert.Equal(t, http.StatusBadRequest, code)
        code, _ = getMail(t, srv, "/api/personal/mail/message?account=ghost&box=inbox&id=x")
        assert.Equal(t, http.StatusBadRequest, code)

        // The dead account's deeper reads answer 502 — honest, not a 500.
        code, _ = getMail(t, srv, "/api/personal/mail/messages?account=remote&box=INBOX")
        assert.Equal(t, http.StatusBadGateway, code)
        code, _ = getMail(t, srv, "/api/personal/mail/message?account=remote&box=INBOX&id=1")
        assert.Equal(t, http.StatusBadGateway, code)

        // The healthy account keeps working end-to-end meanwhile.
        code, _ = getMail(t, srv, "/api/personal/mail/messages?account=local&box=inbox")
        assert.Equal(t, http.StatusOK, code)
}

func TestMailStatusCountsAccounts(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        srv := newMultiMailServer(t, root, second)

        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/personal/status", nil))
        require.Equal(t, http.StatusOK, rec.Code)
        var st personalStatusResponse
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &st))
        assert.Equal(t, 3, st.MailAccounts, "legacy mail_dir + two explicit accounts")
        assert.True(t, st.Enabled)
}
