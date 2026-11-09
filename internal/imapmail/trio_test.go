package imapmail

// The imapmail trio. The fixture is a real IMAP server: emersion's
// imapserver over an in-memory backend whose letters are hand-written
// goldens (uids, flags, envelopes, dates, bodies all pinned in one
// table), so the oracle is independent of the client code under test.
//
// The flagship differential runs ONE letter set through BOTH transports:
// the same five raw messages land in a real Maildir tree on disk and in
// the fixture IMAP server on the wire, and the two readers must agree
// entry-by-entry — From, To, Subject, flags, dates, sizes, and (through
// Open) the extracted bodies byte-for-byte. A reader that grows its own
// idea of what a summary or a body is cannot pass by agreeing with
// itself: the disk path is the oracle and the wire path is the suspect.
//
// The negatives pin the error classes the web layer maps: a dead server
// is ErrUnavailable, a rejected login is ErrAuth, a missing box is
// ErrNoSuchBox, a malformed or absent UID is ErrBadID / ErrNoMessage.

import (
        "context"
        "errors"
        "fmt"
        "net"
        "os"
        "path/filepath"
        "strings"
        "testing"
        "time"

        "github.com/emersion/go-imap/v2"
        "github.com/emersion/go-imap/v2/imapserver"
        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/maildir"
        "github.com/damien1141/a1/internal/project"
)

// ---------- the fixture backend ----------

type fixtureMsg struct {
        uid      uint32
        flags    []imap.Flag
        subject  string
        fromName string
        fromBox  string
        fromHost string
        to       string
        date     time.Time        // ENVELOPE date (zero = the client must fall back)
        internal time.Time        // INTERNALDATE
        raw      string           // the whole RFC822 letter; size = len(raw)
}

type fixtureBox struct {
        name string
        msgs []fixtureMsg
}

type fixtureSession struct {
        user     string
        pass     string
        boxes    []fixtureBox
        selected *fixtureBox
        logged   bool
}

func (s *fixtureSession) Close() error { return nil }

func (s *fixtureSession) Login(username, password string) error {
        if username != s.user || password != s.pass {
                return &imap.Error{Type: imap.StatusResponseTypeNo, Text: "AUTHENTICATIONFAILED"}
        }
        s.logged = true
        return nil
}

func (s *fixtureSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
        b, ok := s.box(mailbox)
        if !ok {
                // imap.Error passes through the server verbatim; a plain error is
                // masked as a generic SERVERBUG and the client could not tell a
                // missing box from a dead one.
                return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "no such mailbox: " + mailbox}
        }
        s.selected = b
        return &imap.SelectData{NumMessages: uint32(len(b.msgs))}, nil
}

func (s *fixtureSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
        for i := range s.boxes {
                _ = w.WriteList(&imap.ListData{Mailbox: s.boxes[i].name, Delim: '/'})
        }
        return nil
}

func (s *fixtureSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
        b, ok := s.box(mailbox)
        if !ok {
                return nil, &imap.Error{Type: imap.StatusResponseTypeNo, Text: "no such mailbox: " + mailbox}
        }
        sd := &imap.StatusData{Mailbox: mailbox}
        if options.NumMessages {
                n := uint32(len(b.msgs))
                sd.NumMessages = &n
        }
        if options.NumUnseen {
                u := uint32(0)
                for _, m := range b.msgs {
                        if !flagListHas(m.flags, imap.FlagSeen) {
                                u++
                        }
                }
                sd.NumUnseen = &u
        }
        return sd, nil
}

func (s *fixtureSession) Fetch(w *imapserver.FetchWriter, numSet imap.NumSet, options *imap.FetchOptions) error {
        if s.selected == nil {
                return errors.New("FETCH failed: no mailbox selected")
        }
        var whole *imap.FetchItemBodySection
        if len(options.BodySection) > 0 {
                for _, sec := range options.BodySection {
                        if sec.Specifier != imap.PartSpecifierHeader {
                                whole = sec
                        }
                }
        }
        for i, m := range s.selected.msgs {
                seq := uint32(i + 1)
                if seqSet, ok := numSet.(imap.SeqSet); ok && !seqSet.Contains(seq) {
                        continue
                }
                fw := w.CreateMessage(seq)
                fw.WriteUID(imap.UID(m.uid))
                fw.WriteFlags(m.flags)
                env := m.envelope()
                fw.WriteEnvelope(&env)
                if !m.internal.IsZero() {
                        fw.WriteInternalDate(m.internal)
                }
                fw.WriteRFC822Size(int64(len(m.raw)))
                if whole != nil {
                        bw := fw.WriteBodySection(whole, int64(len(m.raw)))
                        _, _ = bw.Write([]byte(m.raw))
                        _ = bw.Close()
                }
                _ = fw.Close()
        }
        return nil
}

func (s *fixtureSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
        if s.selected == nil {
                return nil, errors.New("SEARCH failed: no mailbox selected")
        }
        matched := imap.SeqSet{}
        var uidMatched imap.UIDSet
        for i, m := range s.selected.msgs {
                seq := uint32(i + 1)
                inUID := false
                for _, set := range criteria.UID {
                        if set.Contains(imap.UID(m.uid)) {
                                inUID = true
                                break
                        }
                }
                if len(criteria.UID) > 0 && !inUID {
                        continue
                }
                matched.AddNum(seq)
                uidMatched.AddNum(imap.UID(m.uid))
        }
        if kind == imapserver.NumKindSeq {
                return &imap.SearchData{All: matched}, nil
        }
        return &imap.SearchData{All: uidMatched}, nil
}

// Everything the reader never does is refused loudly — the fixture would
// rather fail the test than silently pretend a write happened.
func (s *fixtureSession) Create(mailbox string, options *imap.CreateOptions) error {
        return errors.New("fixture: Create unsupported")
}
func (s *fixtureSession) Delete(mailbox string) error { return errors.New("fixture: Delete unsupported") }
func (s *fixtureSession) Rename(mailbox, newName string, options *imap.RenameOptions) error {
        return errors.New("fixture: Rename unsupported")
}
func (s *fixtureSession) Subscribe(mailbox string) error { return nil }
func (s *fixtureSession) Unsubscribe(mailbox string) error {
        return nil
}
func (s *fixtureSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
        return nil, errors.New("fixture: Append unsupported")
}
func (s *fixtureSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
        return nil
}
func (s *fixtureSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
        <-stop
        return nil
}
func (s *fixtureSession) Unselect() error { s.selected = nil; return nil }
func (s *fixtureSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
        return errors.New("fixture: Expunge unsupported")
}
func (s *fixtureSession) Store(w *imapserver.FetchWriter, numSet imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
        return errors.New("fixture: Store unsupported")
}
func (s *fixtureSession) Copy(numSet imap.NumSet, dest string) (*imap.CopyData, error) {
        return nil, errors.New("fixture: Copy unsupported")
}

func (s *fixtureSession) box(name string) (*fixtureBox, bool) {
        for i := range s.boxes {
                if s.boxes[i].name == name {
                        return &s.boxes[i], true
                }
        }
        return nil, false
}

func (m fixtureMsg) envelope() imap.Envelope {
        return imap.Envelope{
                Date:    m.date,
                Subject: m.subject,
                From:    []imap.Address{{Name: m.fromName, Mailbox: m.fromBox, Host: m.fromHost}},
                To:      []imap.Address{{Name: "", Mailbox: "you", Host: "example.org"}},
        }
}

func flagListHas(flags []imap.Flag, want imap.Flag) bool {
        for _, f := range flags {
                if f == want {
                        return true
                }
        }
        return false
}

// ---------- the golden letters ----------

// The five letters, hand-written once and shared by both transports: the
// IMAP fixture serves raw verbatim, and the maildir tree is built from
// the SAME raw bytes, so the differential's oracle is the table itself.
var goldenMsgs = []struct {
        uid      uint32
        flags    []imap.Flag
        subject  string
        fromName string
        fromBox  string
        fromHost string
        date     time.Time
        internal time.Time
        raw      string
        maildirFile string // cur/ or new/ placement with flag suffix, "" = skip
}{
        {
                uid: 101, flags: []imap.Flag{imap.FlagSeen},
                subject: "Lunch?", fromName: "Alya Voss", fromBox: "alya", fromHost: "x.dev",
                date: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC),
                raw:  "From: Alya Voss <alya@x.dev>\r\nTo: you@example.org\r\nSubject: Lunch?\r\nDate: Mon, 2 Nov 2026 09:00:00 +0000\r\n\r\nPlain text body.\r\n",
                maildirFile: "cur/101.lunch@a1.host:2,S",
        },
        {
                uid: 105, flags: []imap.Flag{imap.FlagAnswered, imap.FlagFlagged},
                subject: "Re: invoice", fromName: "Boris Klein", fromBox: "boris", fromHost: "y.dev",
                date: time.Date(2026, 11, 3, 14, 30, 0, 0, time.UTC),
                raw:  "From: Boris Klein <boris@y.dev>\r\nTo: you@example.org\r\nSubject: Re: invoice\r\nDate: Tue, 3 Nov 2026 14:30:00 +0000\r\n\r\nAnswered and flagged.\r\n",
                maildirFile: "cur/105.invoice@a1.host:2,RF", // R=answered F=flagged
        },
        {
                uid: 110, flags: nil,
                subject: "No envelope date", fromName: "1970 Bot", fromBox: "bot", fromHost: "z.dev",
                date: time.Time{}, // zero: ENVELOPE date missing
                internal: time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC),
                raw:  "From: 1970 Bot <bot@z.dev>\r\nTo: you@example.org\r\nSubject: No envelope date\r\n\r\nDateless letter.\r\n",
                maildirFile: "new/110.dateless@a1.host", // new/ = unseen
        },
        {
                uid: 120, flags: []imap.Flag{imap.FlagDeleted},
                subject: "HTML blast", fromName: "Ads", fromBox: "ads", fromHost: "spam.dev",
                date: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
                raw:  "From: Ads <ads@spam.dev>\r\nTo: you@example.org\r\nSubject: HTML blast\r\nDate: Thu, 5 Nov 2026 10:00:00 +0000\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<html><body><p>Big <b>sale</b>!</p><script>evil()</script></body></html>\r\n",
                maildirFile: "cur/120.html@a1.host:2,T", // T=trashed(deleted)
        },
        {
                uid: 130, flags: nil,
                subject: "Multipart hello", fromName: "Multipart", fromBox: "mp", fromHost: "m.dev",
                date: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC),
                raw: "From: Multipart <mp@m.dev>\r\nTo: you@example.org\r\nSubject: Multipart hello\r\nDate: Fri, 6 Nov 2026 12:00:00 +0000\r\nContent-Type: multipart/alternative; boundary=BOUND\r\n\r\n--BOUND\r\nContent-Type: text/plain\r\n\r\nPlain wins.\r\n--BOUND\r\nContent-Type: text/html\r\n\r\n<p>HTML loses</p>\r\n--BOUND--\r\n",
                maildirFile: "new/130.multipart@a1.host",
        },
}

// expectedOrder is the Date-desc golden order (all dates distinct, so the
// tiebreakers cannot disagree): 130, 120, 110, 105, 101.
var expectedOrder = []uint32{130, 120, 110, 105, 101}

func fixtureAccount(port int) project.MailAccount {
        off := false
        return project.MailAccount{Name: "fixture", Type: "imap", Host: "127.0.0.1", Port: port, Username: "user", Password: "pass", TLS: &off}
}

// startFixture runs the in-memory IMAP server and returns its port.
func startFixture(t *testing.T) (*fixtureSession, int) {
        t.Helper()
        msgs := make([]fixtureMsg, len(goldenMsgs))
        for i, g := range goldenMsgs {
                msgs[i] = fixtureMsg{uid: g.uid, flags: g.flags, subject: g.subject, fromName: g.fromName, fromBox: g.fromBox, fromHost: g.fromHost, to: "you@example.org", date: g.date, internal: g.internal, raw: g.raw}
        }
        sess := &fixtureSession{
                user: "user", pass: "pass",
                boxes: []fixtureBox{
                        {name: "INBOX", msgs: msgs},
                        {name: "Archive", msgs: []fixtureMsg{{
                                uid: 201, subject: "Kept", fromName: "Keeper", fromBox: "k", fromHost: "k.dev",
                                date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
                                raw:  "From: Keeper <k@k.dev>\r\nTo: you@example.org\r\nSubject: Kept\r\nDate: Thu, 1 Jan 2026 00:00:00 +0000\r\n\r\nKept body.\r\n",
                        }}},
                        {name: "Empty"},
                },
        }
        srv := imapserver.New(&imapserver.Options{
                NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
                        clone := *sess
                        clone.selected = nil
                        return &clone, nil, nil
                },
                InsecureAuth: true,
        })
        ln, err := net.Listen("tcp", "127.0.0.1:0")
        require.NoError(t, err)
        go func() { _ = srv.Serve(ln) }()
        t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })
        return sess, ln.Addr().(*net.TCPAddr).Port
}

// seedMaildir builds the disk half of the differential from the same
// golden table.
func seedMaildir(t *testing.T, root string) {
        t.Helper()
        for _, g := range goldenMsgs {
                if g.maildirFile == "" {
                        continue
                }
                p := filepath.Join(root, "inbox", filepath.Dir(g.maildirFile), filepath.Base(g.maildirFile))
                require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
                // The wire path's size is len(raw); the disk path's size must
                // match, so the file holds exactly the fixture's raw bytes.
                require.NoError(t, os.WriteFile(p, []byte(g.raw), 0o644))
                // And the mtime is pinned to the letter's effective date, so the
                // disk path's mtime fallback (for the dateless letter) is
                // deterministic and both transports order the series identically.
                effective := g.date
                if effective.IsZero() {
                        effective = g.internal
                }
                require.NoError(t, os.Chtimes(p, effective, effective))
        }
}

// ---------- positive ----------

func TestImapListAndOpen_Positive(t *testing.T) {
        _, port := startFixture(t)
        acct := fixtureAccount(port)
        ctx := context.Background()

        // The rail: three boxes, counts off the golden table.
        boxes, err := Boxes(ctx, acct)
        require.NoError(t, err)
        require.Len(t, boxes, 3)
        assert.Equal(t, maildir.Mailbox{Name: "Archive", Total: 1, Unread: 1}, boxes[0])
        assert.Equal(t, maildir.Mailbox{Name: "Empty", Total: 0, Unread: 0}, boxes[1])
        assert.Equal(t, maildir.Mailbox{Name: "INBOX", Total: 5, Unread: 4}, boxes[2], "only the first letter carries \\Seen")

        // The list: golden order, flags and envelopes intact, no body.
        msgs, err := List(ctx, acct, "INBOX", 0)
        require.NoError(t, err)
        require.Len(t, msgs, len(expectedOrder))
        for i, wantUID := range expectedOrder {
                assert.Equal(t, fmt.Sprint(wantUID), msgs[i].ID, "position %d", i)
        }
        assert.Equal(t, "Multipart hello", msgs[0].Subject)
        assert.Equal(t, "Multipart", msgs[0].From, "names preferred over bare addresses")
        assert.Equal(t, "Ads", msgs[1].From)
        assert.True(t, msgs[1].Trashed, "\\Deleted maps to the trashed marker")
        assert.True(t, msgs[3].Answered && msgs[3].Flagged, "\\Answered and \\Flagged map through")
        assert.False(t, msgs[3].Seen)
        assert.Empty(t, msgs[0].Subject[:0], "no side effects")
        assert.False(t, msgs[2].Seen, "flagless letter is unread")

        // The dateless letter falls back to INTERNALDATE.
        assert.True(t, msgs[2].Date.Equal(time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)),
                "envelope-date zero must fall back to INTERNALDATE")

        // The limit law: every page is a prefix of the full order.
        page, err := List(ctx, acct, "INBOX", 3)
        require.NoError(t, err)
        require.Len(t, page, 3)
        for i := range page {
                assert.Equal(t, msgs[i].ID, page[i].ID, "limit page %d is a prefix", i)
        }

        // An empty box answers empty, not an error.
        empty, err := List(ctx, acct, "Empty", 50)
        require.NoError(t, err)
        assert.Empty(t, empty)

        // The read pane: plain body verbatim, \Seen untouched server-side.
        full, err := Open(ctx, acct, "INBOX", "101")
        require.NoError(t, err)
        assert.Equal(t, "Lunch?", full.Subject)
        assert.Equal(t, "Plain text body.\n", full.Body,
                "net/mail normalizes CRLF to LF in the body — identically on both transports, so the extraction agrees")
        assert.False(t, full.WasHTML)
        assert.False(t, full.Truncated)

        // The HTML letter strips to text (script dropped) and says so.
        full, err = Open(ctx, acct, "INBOX", "120")
        require.NoError(t, err)
        assert.True(t, full.WasHTML)
        assert.Contains(t, full.Body, "Big sale!")
        assert.NotContains(t, full.Body, "evil()")

        // The multipart letter prefers text/plain.
        full, err = Open(ctx, acct, "INBOX", "130")
        require.NoError(t, err)
        assert.Contains(t, full.Body, "Plain wins.")
        assert.NotContains(t, full.Body, "HTML loses")
}

// TestImapMatchesMaildir_Differential runs ONE letter set through BOTH
// transports and demands entry-by-entry agreement — the disk path is the
// oracle, the wire path is the suspect.
func TestImapMatchesMaildir_Differential(t *testing.T) {
        _, port := startFixture(t)
        acct := fixtureAccount(port)
        root := t.TempDir()
        seedMaildir(t, root)
        ctx := context.Background()

        // The listing: maildir.List vs imapmail.List, same order and fields.
        disk, _, err := maildir.List(root, "inbox", 0)
        require.NoError(t, err)
        wire, err := List(ctx, acct, "INBOX", 0)
        require.NoError(t, err)
        require.Len(t, wire, len(disk), "both transports see the same letters")
        for i := range disk {
                assert.Equal(t, disk[i].From, wire[i].From, "%d From", i)
                assert.Equal(t, disk[i].To, wire[i].To, "%d To", i)
                assert.Equal(t, disk[i].Subject, wire[i].Subject, "%d Subject", i)
                assert.Equal(t, disk[i].Seen, wire[i].Seen, "%d Seen", i)
                assert.Equal(t, disk[i].Answered, wire[i].Answered, "%d Answered", i)
                assert.Equal(t, disk[i].Flagged, wire[i].Flagged, "%d Flagged", i)
                assert.Equal(t, disk[i].Trashed, wire[i].Trashed, "%d Trashed", i)
                assert.Equal(t, disk[i].Size, wire[i].Size, "%d Size (the file holds the wire's raw bytes)", i)
                assert.True(t, disk[i].Date.Equal(wire[i].Date), "%d Date: %s vs %s", i, disk[i].Date, wire[i].Date)
        }

        // The read pane: both transports extract the same body bytes.
        for _, g := range goldenMsgs {
                if g.maildirFile == "" {
                        continue
                }
                // The maildir id is the filename's unique part: strip the flag
                // suffix ("101.lunch@a1.host:2,S" → "101.lunch@a1.host").
                base := filepath.Base(g.maildirFile)
                id := base
                if i := strings.IndexByte(base, ':'); i >= 0 {
                        id = base[:i]
                }
                diskFull, err := maildir.Open(root, "inbox", id)
                require.NoError(t, err, "%s", g.maildirFile)
                wireFull, err := Open(ctx, acct, "INBOX", fmt.Sprint(g.uid))
                require.NoError(t, err)
                assert.Equal(t, diskFull.Body, wireFull.Body, "%s body", g.subject)
                assert.Equal(t, diskFull.WasHTML, wireFull.WasHTML, "%s wasHTML", g.subject)
                assert.Equal(t, diskFull.Subject, wireFull.Subject, "%s subject", g.subject)
        }
}

// ---------- negative ----------

func TestImapRefusals_Negative(t *testing.T) {
        _, port := startFixture(t)
        ctx := context.Background()

        // A dead server is ErrUnavailable, not a hang and not a panic.
        dead := fixtureAccount(port + 1)
        _, err := Boxes(ctx, dead)
        assert.True(t, errors.Is(err, ErrUnavailable), "dead server: %v", err)
        _, err = List(ctx, dead, "INBOX", 10)
        assert.True(t, errors.Is(err, ErrUnavailable), "dead server list: %v", err)

        // A rejected login is ErrAuth — against its own fixture, so the
        // shared one's credentials are never mutated mid-test.
        badSess, badPort := startFixture(t)
        bad := fixtureAccount(badPort)
        bad.Username = badSess.user
        bad.Password = "wrong"
        _, err = Boxes(ctx, bad)
        assert.True(t, errors.Is(err, ErrAuth), "bad login: %v", err)

        // A missing box is ErrNoSuchBox on both reads.
        acct := fixtureAccount(port)
        _, err = List(ctx, acct, "Ghost", 10)
        assert.True(t, errors.Is(err, ErrNoSuchBox), "ghost box list: %v", err)
        _, err = Open(ctx, acct, "Ghost", "101")
        assert.True(t, errors.Is(err, ErrNoSuchBox), "ghost box open: %v", err)

        // A malformed UID is a client mistake (ErrBadID); a well-formed but
        // absent one is an honest miss (ErrNoMessage).
        _, err = Open(ctx, acct, "INBOX", "not-a-number")
        assert.True(t, errors.Is(err, ErrBadID), "bad id: %v", err)
        _, err = Open(ctx, acct, "INBOX", "0")
        assert.True(t, errors.Is(err, ErrBadID), "uid zero: %v", err)
        _, err = Open(ctx, acct, "INBOX", "999")
        assert.True(t, errors.Is(err, ErrNoMessage), "absent uid: %v", err)

        // An empty box name is refused before any wire traffic.
        _, err = List(ctx, acct, "", 10)
        assert.True(t, errors.Is(err, ErrNoSuchBox), "empty box: %v", err)

        // An imap account with no host degrades to ErrUnavailable (the
        // config mistake is visible, never fatal to the surface).
        noHost := project.MailAccount{Name: "broken", Type: "imap"}
        _, err = Boxes(ctx, noHost)
        assert.True(t, errors.Is(err, ErrUnavailable), "no host: %v", err)
}
