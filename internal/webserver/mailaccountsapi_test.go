package webserver

import (
        "bufio"
        "encoding/json"
        "fmt"
        "net"
        "net/http"
        "net/http/httptest"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
        "gopkg.in/yaml.v3"
)

// The mail CONFIGURATOR's trio (dashboard chunk 8). The ruling under
// test: this surface writes the keyring, never the letters — the GET
// never carries a password, the PUT's keep rule resolves credentials by
// name against the saved list, the test dial persists nothing, and every
// write composes the gate and the CSRF guard like the kanban before it.

// doMailAccounts issues one request against the configurator's routes and
// decodes a JSON response when out is non-nil.
func doMailAccounts(t *testing.T, srv *Server, method, path, body string, origin string, out any) int {
        t.Helper()
        req := httptest.NewRequest(method, path, strings.NewReader(body))
        req.Header.Set("Content-Type", "application/json")
        if origin != "" {
                req.Header.Set("Origin", origin)
        }
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, req)
        if out != nil && rec.Code == http.StatusOK {
                require.NoError(t, json.Unmarshal(rec.Body.Bytes(), out), "decode %s %s: %s", method, path, rec.Body.String())
        }
        return rec.Code
}

// fixtureAccountsYAML builds a personal section with the legacy root, one
// saved imap account (dead by construction — port 1), and one maildir
// account beside it.
func fixtureAccountsYAML(root, second string) string {
        return fmt.Sprintf(`personal:
  enabled: true
  mail_dir: %s
  mail_accounts:
    - name: work
      type: imap
      host: 127.0.0.1
      port: 1
      username: me
      password: kept-secret
      tls: false
    - name: home
      root: %s
`, root, second)
}

// ---------- positive ----------

// TestMailAccountsListAndSave_Positive: the GET never leaks a credential,
// the PUT replaces the list wholesale, keepPassword carries the saved
// password onto the same-named entry, and the save is LIVE — the rail
// serves the new account without a restart (onConfigSaved reloaded the
// project config under the handler).
func TestMailAccountsListAndSave_Positive(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        srv := newPersonalFixtureProject(t, fixtureAccountsYAML(root, second))

        // The list: entries carry hasPassword and never the password.
        code := doMailAccounts(t, srv, http.MethodGet, "/api/personal/mail/accounts", "", "", nil)
        require.Equal(t, http.StatusOK, code)
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/personal/mail/accounts", nil))
        require.Equal(t, http.StatusOK, rec.Code)
        assert.NotContains(t, rec.Body.String(), "kept-secret", "the GET never carries the password")
        var list mailAccountsResponse
        require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
        assert.Equal(t, root, list.LegacyDir, "the legacy root rides as the pinned row")
        require.Len(t, list.Entries, 2)
        assert.Equal(t, "work", list.Entries[0].Name)
        assert.Equal(t, "imap", list.Entries[0].Type)
        assert.True(t, list.Entries[0].HasPassword)
        assert.Equal(t, "home", list.Entries[1].Name)
        assert.Equal(t, "maildir", list.Entries[1].Type)
        assert.False(t, list.Entries[1].HasPassword)
        require.Len(t, list.Resolved, 3, "local + work + home")
        assert.Equal(t, "local", list.Resolved[0].Name)

        // The save: replace the list with [work kept, home, arch new] —
        // work sends NO password (the editor never saw one) and expects
        // the saved credential to survive.
        body := fmt.Sprintf(`{"accounts":[
          {"name":"work","type":"imap","host":"imap.work.dev","port":993,"username":"me@work.dev","keepPassword":true,"mailbox":"INBOX"},
          {"name":"home","type":"maildir","root":%q},
          {"name":"arch","type":"maildir","root":%q}
        ]}`, second, root)
        var saved mailAccountsResponse
        code = doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", body, "", &saved)
        require.Equal(t, http.StatusOK, code)
        require.Len(t, saved.Entries, 3)
        assert.True(t, saved.Entries[0].HasPassword, "keep carried the credential onto the same-named entry")
        assert.False(t, saved.Entries[2].HasPassword, "the new account has none to carry")
        require.Len(t, saved.Resolved, 4, "local + the saved three")

        // The disk: the kept password is in the file under work's entry,
        // and the new account's root is too.
        data, err := os.ReadFile(srv.proj.Global().ConfigFile())
        require.NoError(t, err)
        text := string(data)
        assert.Contains(t, text, "kept-secret", "the keep rule wrote the saved credential back")
        assert.Contains(t, text, "name: arch")
        assert.Contains(t, text, "host: imap.work.dev")

        // The save is live: the rail serves the NEW account now, from the
        // reloaded config, no restart.
        code, raw := getMail(t, srv, "/api/personal/mail/mailboxes?account=arch")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        assert.Equal(t, "arch", rail.Account)
        require.Len(t, rail.Mailboxes, 3, "the seeded fixture's boxes through the reloaded account")

        // An empty save empties the list (and keeps the legacy root).
        code = doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", `{"accounts":[]}`, "", nil)
        require.Equal(t, http.StatusOK, code)
        data, err = os.ReadFile(srv.proj.Global().ConfigFile())
        require.NoError(t, err)
        assert.NotContains(t, string(data), "mail_accounts", "the sequence is gone")
        assert.Contains(t, string(data), "mail_dir:", "the legacy root survives an empty save")
}

// TestMailAccountTestDial_Positive: the test dial reads one spec, reports
// honestly, and persists nothing. The fake IMAP listener records the
// conversation so the keep rule's server-side merge is proven on the
// wire: a saved account is tested with the password the client never
// saw, and an explicit password dials as typed.
func TestMailAccountTestDial_Positive(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        srv := newPersonalFixtureProject(t, fixtureAccountsYAML(root, second))

        // A local maildir spec dials true and counts the fixture's boxes.
        var res mailTestResponse
        code := doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test",
                fmt.Sprintf(`{"type":"maildir","root":%q}`, root), "", &res)
        require.Equal(t, http.StatusOK, code)
        assert.True(t, res.OK)
        assert.Equal(t, 3, res.Boxes)
        require.Len(t, res.Sample, 3)
        assert.Equal(t, "inbox", res.Sample[2], "name-sorted, the maildir rail's order")

        // A saved imap account, tested with keepPassword, dials with the
        // STORED credential — recorded by a fake server that greets,
        // reads the LOGIN, and refuses it.
        fake := newFakeIMAP(t)
        defer fake.close()
        body := fmt.Sprintf(`{"name":"work","type":"imap","host":"127.0.0.1","port":%d,"username":"me","keepPassword":true,"tls":false}`, fake.port)
        code = doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test", body, "", &res)
        require.Equal(t, http.StatusOK, code)
        assert.False(t, res.OK, "the fake server refuses the login")
        assert.Contains(t, res.Error, "imap")
        assert.Contains(t, fake.waitConversation("kept-secret", 2*time.Second), "kept-secret", "the dial used the stored credential the client never saw")

        // An explicit password dials as typed.
        body = fmt.Sprintf(`{"name":"work","type":"imap","host":"127.0.0.1","port":%d,"username":"me","password":"fresh-secret","tls":false}`, fake.port)
        code = doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test", body, "", &res)
        require.Equal(t, http.StatusOK, code)
        assert.False(t, res.OK)
        assert.Contains(t, fake.waitConversation("fresh-secret", 2*time.Second), "fresh-secret", "the explicit password dials as typed")

        // Nothing persisted: the spec never touched the file.
        data, err := os.ReadFile(srv.proj.Global().ConfigFile())
        require.NoError(t, err)
        assert.NotContains(t, string(data), "fresh-secret", "the test dial is not a save")
        assert.Contains(t, string(data), "port: 1", "the saved work account kept its dead port")
}

// fakeIMAP is a minimal IMAP listener: every connection gets a greeting,
// everything the client sends is recorded until a LOGIN lands, and the
// command is answered with its own tag, refused. Just enough protocol to
// prove WHICH credential crossed the wire, for as many dials as the test
// makes.
type fakeIMAP struct {
        ln     net.Listener
        port   int
        mu     chan struct{} // guards the recorded conversation
        logged string
}

func newFakeIMAP(t *testing.T) *fakeIMAP {
        t.Helper()
        ln, err := net.Listen("tcp", "127.0.0.1:0")
        require.NoError(t, err)
        f := &fakeIMAP{ln: ln, port: ln.Addr().(*net.TCPAddr).Port, mu: make(chan struct{}, 1)}
        f.mu <- struct{}{}
        go func() {
                for {
                        conn, err := ln.Accept()
                        if err != nil {
                                return
                        }
                        f.handle(conn)
                }
        }()
        return f
}

// handle speaks just enough IMAP for one dial: greet, read until LOGIN,
// refuse it with the command's own tag, and record the conversation.
func (f *fakeIMAP) handle(conn net.Conn) {
        defer conn.Close()
        _ = conn.SetDeadline(time.Now().Add(10 * time.Second))
        if _, err := conn.Write([]byte("* OK a1 fake ready\r\n")); err != nil {
                return
        }
        r := bufio.NewReader(conn)
        var all strings.Builder
        for {
                line, err := r.ReadString('\n')
                if err != nil {
                        break
                }
                all.WriteString(line)
                if strings.Contains(strings.ToUpper(line), "LOGIN") {
                        tag := strings.Fields(line)
                        if len(tag) > 0 {
                                _, _ = conn.Write([]byte(tag[0] + " NO rejected by fixture\r\n"))
                        }
                        break
                }
        }
        <-f.mu
        f.logged += all.String()
        f.mu <- struct{}{}
}

// conversation returns everything every client sent.
func (f *fakeIMAP) conversation() string {
        <-f.mu
        s := f.logged
        f.mu <- struct{}{}
        return s
}

// waitConversation polls until the recorded conversation contains substr
// (the handle goroutine records asynchronously, a hair after the dial's
// HTTP response) or the deadline passes, returning what it has.
func (f *fakeIMAP) waitConversation(substr string, timeout time.Duration) string {
        deadline := time.Now().Add(timeout)
        for {
                s := f.conversation()
                if strings.Contains(s, substr) || time.Now().After(deadline) {
                        return s
                }
                time.Sleep(25 * time.Millisecond)
        }
}

func (f *fakeIMAP) close() {
        _ = f.ln.Close()
}

// ---------- negative ----------

func TestMailAccountsGatedOff_Negative(t *testing.T) {
        // The master switch governs the configurator like every personal
        // surface — including the GET, because account names and hosts
        // are gated content (the ungated status carries only a count).
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: false\n")
        for _, tc := range []struct{ method, path, body string }{
                {http.MethodGet, "/api/personal/mail/accounts", ""},
                {http.MethodPut, "/api/personal/mail/accounts", `{"accounts":[]}`},
                {http.MethodPost, "/api/personal/mail/test", `{"type":"maildir","root":"/x"}`},
        } {
                assert.Equal(t, http.StatusForbidden, doMailAccounts(t, srv, tc.method, tc.path, tc.body, "", nil),
                        "%s %s is gated", tc.method, tc.path)
        }
}

func TestMailAccountsCrossOriginRefused_Negative(t *testing.T) {
        // A cross-origin page must not be able to edit the keyring: both
        // writes compose guard() on top of the gate.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")
        assert.Equal(t, http.StatusForbidden, doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", `{"accounts":[]}`, "http://evil.example", nil))
        assert.Equal(t, http.StatusForbidden, doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test", `{"type":"maildir","root":"/x"}`, "http://evil.example", nil))
        // The refused write left nothing behind.
        code, raw := getMail(t, srv, "/api/personal/mail/accounts")
        require.Equal(t, http.StatusOK, code)
        var list mailAccountsResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        assert.Empty(t, list.Entries)
}

func TestMailAccountsHostileSaves_Negative(t *testing.T) {
        root := t.TempDir()
        second := t.TempDir()
        srv := newPersonalFixtureProject(t, fixtureAccountsYAML(root, second))

        for _, tc := range []struct{ name, body string }{
                {"malformed json", `{"accounts":[`},
                {"not an object", `nope`},
                {"unknown type", `{"accounts":[{"name":"x","type":"pop3","root":"/x"}]}`},
                {"imap without host", `{"accounts":[{"name":"x","type":"imap"}]}`},
                {"maildir without root", `{"accounts":[{"name":"x","type":"maildir"}]}`},
                {"port negative", `{"accounts":[{"name":"x","type":"imap","host":"h","port":-1}]}`},
                {"port oversize", `{"accounts":[{"name":"x","type":"imap","host":"h","port":65536}]}`},
                {"password and keep both set", `{"accounts":[{"name":"x","type":"maildir","root":"/x","password":"p","keepPassword":true}]}`},
        } {
                code := doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", tc.body, "", nil)
                assert.Equal(t, http.StatusBadRequest, code, "%s", tc.name)
        }

        // The list cap: 33 accounts is beyond the editor's ambition.
        var many strings.Builder
        many.WriteString(`{"accounts":[`)
        for i := 0; i < 33; i++ {
                if i > 0 {
                        many.WriteByte(',')
                }
                fmt.Fprintf(&many, `{"name":"a%d","type":"maildir","root":%q}`, i, root)
        }
        many.WriteString(`]}`)
        assert.Equal(t, http.StatusBadRequest, doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", many.String(), "", nil))

        // Every refused save left the file untouched: the fixture's two
        // accounts are still exactly what they were.
        code, raw := getMail(t, srv, "/api/personal/mail/accounts")
        require.Equal(t, http.StatusOK, code)
        var list mailAccountsResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        require.Len(t, list.Entries, 2, "no hostile write ever landed")

        // The test endpoint runs the same shape validation.
        for _, body := range []string{
                `{"type":"pop3"}`,
                `{"type":"imap"}`,
                `{"type":"maildir"}`,
                `{"type":"imap","host":"h","port":99999}`,
                `{"type":"maildir","root":"/x","password":"p","keepPassword":true}`,
                `nope`,
        } {
                assert.Equal(t, http.StatusBadRequest, doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test", body, "", nil), "%s", body)
        }

        // A dead server is the honest answer, not an error: ok:false with
        // the reason the button exists to fetch.
        var res mailTestResponse
        code = doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test",
                `{"name":"work","type":"imap","host":"127.0.0.1","port":1,"username":"me","password":"x","tls":false}`, "", &res)
        require.Equal(t, http.StatusOK, code)
        assert.False(t, res.OK)
        assert.NotEmpty(t, res.Error)

        // keepPassword with no same-named saved account keeps nothing —
        // the dial goes out with no credential and the server says why.
        code = doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test",
                `{"name":"ghost","type":"imap","host":"127.0.0.1","port":1,"username":"me","keepPassword":true,"tls":false}`, "", &res)
        require.Equal(t, http.StatusOK, code)
        assert.False(t, res.OK)
}

// TestMailAccountsUnnamedKeepFingerprint_Negative pins the collision the
// browser pass caught: two accounts saved WITHOUT names both key on the
// empty string, and a first-match keep rule handed the maildir's empty
// password to the IMAP account (the credential silently vanished). The
// rule now resolves unnamed keeps by type-and-server fingerprint, and a
// keep only lands on a unique match: an unnamed imap among unnamed
// maildirs keeps its own password; two indistinguishable accounts
// (same fingerprint, both unnamed) refuse to carry rather than guess.
func TestMailAccountsUnnamedKeepFingerprint_Negative(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        // One unnamed maildir + one unnamed imap with a password — the
        // exact shape the browser flow produced (the imap points at the
        // loopback so the later dial can reach the fake server).
        srv := newPersonalFixtureProject(t, fmt.Sprintf(`personal:
  enabled: true
  mail_accounts:
    - root: %s
    - type: imap
      host: 127.0.0.1
      port: 1
      username: me
      password: kept-secret
      tls: false
`, second))

        // PUT both back with keepPassword on the unnamed imap (blank
        // password field, hasPassword never seen by the client).
        body := fmt.Sprintf(`{"accounts":[
          {"type":"maildir","root":%q},
          {"type":"imap","host":"127.0.0.1","port":1,"username":"me@work.dev","keepPassword":true,"mailbox":"INBOX"}
        ]}`, second)
        var saved mailAccountsResponse
        code := doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", body, "", &saved)
        require.Equal(t, http.StatusOK, code)
        require.Len(t, saved.Entries, 2)
        assert.False(t, saved.Entries[0].HasPassword, "the maildir never had one")
        assert.True(t, saved.Entries[1].HasPassword, "the unnamed imap kept its own password — not the maildir's emptiness")

        // The disk agrees: the password survived the unnamed edit.
        data, err := os.ReadFile(srv.proj.Global().ConfigFile())
        require.NoError(t, err)
        assert.Contains(t, string(data), "kept-secret")

        // Two indistinguishable accounts (same host, both unnamed): the
        // keep refuses to guess — the password drops, honestly.
        srv2 := newPersonalFixtureProject(t, `personal:
  enabled: true
  mail_accounts:
    - type: imap
      host: mx.work.dev
      username: a
      password: pw-a
      tls: false
    - type: imap
      host: mx.work.dev
      username: b
      password: pw-b
      tls: false
`)
        code = doMailAccounts(t, srv2, http.MethodPut, "/api/personal/mail/accounts",
                `{"accounts":[{"type":"imap","host":"mx.work.dev","username":"a","keepPassword":true,"tls":false}]}`, "", &saved)
        require.Equal(t, http.StatusOK, code)
        require.Len(t, saved.Entries, 1)
        assert.False(t, saved.Entries[0].HasPassword, "an ambiguous fingerprint carries nothing rather than guess")
        data, err = os.ReadFile(srv2.proj.Global().ConfigFile())
        require.NoError(t, err)
        assert.NotContains(t, string(data), "pw-a", "neither twin's credential was picked")
        assert.NotContains(t, string(data), "pw-b")

        // The test dial resolves the same way: a unique unnamed match
        // dials with the stored credential.
        fake := newFakeIMAP(t)
        defer fake.close()
        var tres mailTestResponse
        code = doMailAccounts(t, srv, http.MethodPost, "/api/personal/mail/test",
                fmt.Sprintf(`{"type":"imap","host":"127.0.0.1","port":%d,"username":"me","keepPassword":true,"tls":false}`, fake.port), "", &tres)
        require.Equal(t, http.StatusOK, code)
        assert.False(t, tres.OK, "the fake server refuses the login")
        assert.Contains(t, fake.waitConversation("kept-secret", 2*time.Second), "kept-secret",
                "the unnamed test dial used the stored credential by fingerprint")
}

// ---------- differential ----------

// TestMailAccountsServeFile_Differential: the endpoint's entries must be
// a faithful projection of the file — the test parses config.yaml with
// its OWN yaml unmarshal (not the server's reader) and compares
// entry-by-entry, hasPassword included. And the resolved pills the
// editor serves are the same ones the rail serves: two endpoints, one
// truth, compared.
func TestMailAccountsServeFile_Differential(t *testing.T) {
        root := t.TempDir()
        seedMailFixture(t, root)
        second := t.TempDir()
        seedMailFixture(t, second)
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")

        body := fmt.Sprintf(`{"accounts":[
          {"name":"work","type":"imap","host":"imap.work.dev","port":143,"username":"me","password":"apppw","mailbox":"Archive","tls":false},
          {"name":"home","type":"maildir","root":%q},
          {"type":"maildir","root":%q}
        ]}`, second, root)
        code := doMailAccounts(t, srv, http.MethodPut, "/api/personal/mail/accounts", body, "", nil)
        require.Equal(t, http.StatusOK, code)

        code, raw := getMail(t, srv, "/api/personal/mail/accounts")
        require.Equal(t, http.StatusOK, code)
        var list mailAccountsResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &list))
        require.Len(t, list.Entries, 3)

        // The independent oracle: the test's own YAML read of the file.
        data, err := os.ReadFile(srv.proj.Global().ConfigFile())
        require.NoError(t, err)
        var doc struct {
                Personal struct {
                        MailAccounts []struct {
                                Name     string `yaml:"name"`
                                Type     string `yaml:"type"`
                                Root     string `yaml:"root"`
                                Host     string `yaml:"host"`
                                Port     int    `yaml:"port"`
                                Username string `yaml:"username"`
                                Password string `yaml:"password"`
                                TLS      *bool  `yaml:"tls"`
                                Mailbox  string `yaml:"mailbox"`
                        } `yaml:"mail_accounts"`
                } `yaml:"personal"`
        }
        require.NoError(t, yaml.Unmarshal(data, &doc))
        require.Len(t, doc.Personal.MailAccounts, 3, "the file carries what was saved")
        for i, want := range doc.Personal.MailAccounts {
                got := list.Entries[i]
                assert.Equal(t, want.Name, got.Name, "entry %d name", i)
                assert.Equal(t, want.Type, got.Type, "entry %d type", i)
                assert.Equal(t, want.Root, got.Root, "entry %d root", i)
                assert.Equal(t, want.Host, got.Host, "entry %d host", i)
                assert.Equal(t, want.Port, got.Port, "entry %d port", i)
                assert.Equal(t, want.Username, got.Username, "entry %d username", i)
                assert.Equal(t, want.Mailbox, got.Mailbox, "entry %d mailbox", i)
                assert.Equal(t, want.TLS, got.TLS, "entry %d tls", i)
                assert.Equal(t, want.Password != "", got.HasPassword, "entry %d hasPassword is the file's truth", i)
        }

        // The raw GET never carried the password even though the file has
        // it (belt and braces: the oracle just read it from disk).
        assert.NotContains(t, raw, "apppw")

        // The resolved pills match the rail's account list exactly.
        code, raw = getMail(t, srv, "/api/personal/mail/mailboxes")
        require.Equal(t, http.StatusOK, code)
        var rail mailMailboxesResponse
        require.NoError(t, json.Unmarshal([]byte(raw), &rail))
        require.Len(t, rail.Accounts, len(list.Resolved))
        for i := range rail.Accounts {
                assert.Equal(t, rail.Accounts[i].Name, list.Resolved[i].Name, "resolved account %d agrees with the rail", i)
                assert.Equal(t, rail.Accounts[i].Type, list.Resolved[i].Type, "resolved type %d agrees with the rail", i)
        }
        // The unnamed third account resolved to its root's basename.
        assert.Equal(t, filepath.Base(root), list.Resolved[len(list.Resolved)-1].Name)
}
