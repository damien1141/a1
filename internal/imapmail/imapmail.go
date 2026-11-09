// Package imapmail reads one IMAP account — the remote half of the
// personal mail surface (dashboard chunk 7). It exists so the mail widget
// can treat "a Maildir on disk" and "a server across the network" as the
// same thing: every function returns the maildir package's own types, so
// the web handlers and the widget carry ONE wire shape for both kinds of
// account and the semantics cannot drift apart.
//
// Rulings, inherited from internal/maildir and restated for the network:
//
//   - READ-ONLY, unconditionally. There is no API here that moves, marks,
//     deletes, or appends a message, and none will be added — the widget
//     reads envelopes and letters, the agent facade never sees this
//     package at all, and \Seen is left exactly where the server set it
//     (every SELECT is read-only, every body fetch is BODY.PEEK).
//   - One connection per call, no pooling. The widget fetches a few times
//     per open; a stateless connect-login-read-close cycle keeps the
//     handler goroutine-safe for free and leaks nothing. Costs one round
//     trip per call — honest at personal-calendar scale.
//   - Degradation mirrors the maildir doctrine: a missing box is an
//     ErrNoSuchBox (the widget's 404), a dead server is ErrUnavailable
//     (the widget's honest error line), and nothing is ever a panic.
//   - Message identity is the UID (decimal string), stable within a
//     mailbox for the lifetime of its UIDVALIDITY — the same role the
//     Maildir unique key plays on disk.
//
// The emersion/go-imap v2 client does the protocol; this package is the
// policy (timeouts, read-only selections, ordering, caps).
package imapmail

import (
        "context"
        "crypto/tls"
        "errors"
        "fmt"
        "net"
        "sort"
        "strconv"
        "strings"
        "time"

        "github.com/emersion/go-imap/v2"
        "github.com/emersion/go-imap/v2/imapclient"

        "github.com/damien1141/a1/internal/maildir"
        "github.com/damien1141/a1/internal/project"
)

// Errors the surfaces map: ErrNoSuchBox / ErrNoMessage are honest misses
// (404s), ErrBadID is a client mistake (400), ErrAuth / ErrUnavailable are
// the account's own condition (502 — the config pointed somewhere that did
// not answer, which is nobody's input mistake).
var (
        ErrBadID       = errors.New("imap: invalid message id")
        ErrNoSuchBox   = errors.New("imap: no such mailbox")
        ErrNoMessage   = errors.New("imap: no such message")
        ErrAuth        = errors.New("imap: login rejected")
        ErrUnavailable = errors.New("imap: server unreachable")
)

// Timeouts. The dial and the whole-command lifetime share one ceiling: a
// wedged server must cost the widget one honest error line, not a hung
// request. The deadline rides the underlying conn, so every read in the
// command cycle is covered.
const (
        dialTimeout  = 10 * time.Second
        opTimeout    = 30 * time.Second
        maxFetchPage = 200 // mirrors the web layer's mailMaxLimit clamp
)

// dial opens, greets, and authenticates one session. The caller owns
// Logout; every exported function defers it.
func dial(ctx context.Context, acct project.MailAccount) (*imapclient.Client, error) {
        host := strings.TrimSpace(acct.Host)
        if host == "" {
                return nil, fmt.Errorf("%w: imap account %q has no host", ErrUnavailable, acct.Name)
        }
        tlsOn := acct.TLS == nil || *acct.TLS
        port := acct.Port
        if port == 0 {
                if tlsOn {
                        port = 993
                } else {
                        port = 143
                }
        }
        addr := net.JoinHostPort(host, strconv.Itoa(port))

        d := net.Dialer{Timeout: dialTimeout}
        conn, err := d.DialContext(ctx, "tcp", addr)
        if err != nil {
                return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
        }

        var c *imapclient.Client
        if tlsOn {
                tc := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
                c = imapclient.New(tc, nil)
        } else {
                c = imapclient.New(conn, nil)
        }
        _ = conn.SetDeadline(time.Now().Add(opTimeout))

        if err := c.Login(strings.TrimSpace(acct.Username), acct.Password).Wait(); err != nil {
                c.Close()
                return nil, fmt.Errorf("%w: %v", ErrAuth, err)
        }
        return c, nil
}

// Boxes lists the account's selectable mailboxes with filename-level
// counts, name-sorted to match the maildir rail's order. STATUS per box
// costs one round trip each — honest at personal-mailbox counts.
func Boxes(ctx context.Context, acct project.MailAccount) ([]maildir.Mailbox, error) {
        c, err := dial(ctx, acct)
        if err != nil {
                return nil, err
        }
        defer func() { _ = c.Logout().Wait() }()

        listed, err := c.List("", "%", nil).Collect()
        if err != nil {
                return nil, fmt.Errorf("%w: list failed: %v", ErrUnavailable, err)
        }
        out := make([]maildir.Mailbox, 0, len(listed))
        for _, m := range listed {
                if attrListContains(m.Attrs, imap.MailboxAttrNonExistent, imap.MailboxAttrNoSelect) {
                        continue // a vessel, not a mailbox
                }
                sd, err := c.Status(m.Mailbox, &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
                if err != nil {
                        // A box that vanished between LIST and STATUS still lists,
                        // zeroed — the counts are advisory, the name is the fact.
                        out = append(out, maildir.Mailbox{Name: m.Mailbox})
                        continue
                }
                b := maildir.Mailbox{Name: m.Mailbox}
                if sd.NumMessages != nil {
                        b.Total = int(*sd.NumMessages)
                }
                if sd.NumUnseen != nil {
                        b.Unread = int(*sd.NumUnseen)
                }
                out = append(out, b)
        }
        sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
        return out, nil
}

// List returns a box's latest messages, latest first — the maildir.List
// order (Date desc; UID desc as the tiebreaker, the UID's numeric order
// standing in for the filename's). limit <= 0 means all, capped at
// maxFetchPage. The page walks the box's TAIL by sequence number (IMAP
// sequence order is arrival order), so the fetch never reads a whole
// mailbox to answer "latest 50".
func List(ctx context.Context, acct project.MailAccount, box string, limit int) ([]maildir.Message, error) {
        if strings.TrimSpace(box) == "" {
                return nil, fmt.Errorf("%w: empty mailbox", ErrNoSuchBox)
        }
        if limit <= 0 || limit > maxFetchPage {
                limit = maxFetchPage
        }
        c, err := dial(ctx, acct)
        if err != nil {
                return nil, err
        }
        defer func() { _ = c.Logout().Wait() }()

        sel, err := c.Select(box, &imap.SelectOptions{ReadOnly: true}).Wait()
        if err != nil {
                return nil, mapSelectErr(box, err)
        }
        total := int(sel.NumMessages)
        if total == 0 {
                return []maildir.Message{}, nil
        }
        start := 1
        if total > limit {
                start = total - limit + 1
        }
        var set imap.SeqSet
        set.AddRange(uint32(start), uint32(total))

        bufs, err := c.Fetch(set, &imap.FetchOptions{
                UID:          true,
                Flags:        true,
                Envelope:     true,
                InternalDate: true,
                RFC822Size:   true,
        }).Collect()
        if err != nil {
                return nil, fmt.Errorf("%w: fetch failed: %v", ErrUnavailable, err)
        }

        msgs := make([]maildir.Message, 0, len(bufs))
        for _, b := range bufs {
                if b.Envelope == nil {
                        continue // a headerless monster: nothing honest to show
                }
                msgs = append(msgs, messageFromBuffer(b))
        }
        sort.SliceStable(msgs, func(i, j int) bool {
                if !msgs[i].Date.Equal(msgs[j].Date) {
                        return msgs[i].Date.After(msgs[j].Date)
                }
                return uidOf(msgs[i].ID) > uidOf(msgs[j].ID)
        })
        return msgs, nil
}

// Open returns one message with its body, addressed by UID (as List
// reports it). The body comes back BODY.PEEK[] — the server's \Seen is
// never touched — and runs through maildir.ExtractBody, so a letter reads
// identically from disk and from the wire. Oversized messages answer
// headers only, Truncated set: never a body cut mid-sentence presented
// as whole.
func Open(ctx context.Context, acct project.MailAccount, box, id string) (maildir.Full, error) {
        if strings.TrimSpace(box) == "" {
                return maildir.Full{}, fmt.Errorf("%w: empty mailbox", ErrNoSuchBox)
        }
        uid, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32)
        if err != nil || uid == 0 {
                return maildir.Full{}, fmt.Errorf("%w: %q", ErrBadID, id)
        }
        c, err := dial(ctx, acct)
        if err != nil {
                return maildir.Full{}, err
        }
        defer func() { _ = c.Logout().Wait() }()

        if _, err := c.Select(box, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
                return maildir.Full{}, mapSelectErr(box, err)
        }

        var uidSet imap.UIDSet
        uidSet.AddNum(imap.UID(uid))
        // beta.8 has no UIDFetch: SEARCH (sequence-number flavor) locates the
        // UID's sequence position, and FETCH addresses it there.
        search, err := c.Search(&imap.SearchCriteria{UID: []imap.UIDSet{uidSet}}, nil).Wait()
        if err != nil {
                return maildir.Full{}, fmt.Errorf("%w: search failed: %v", ErrUnavailable, err)
        }
        seqs, ok := search.All.(imap.SeqSet)
        if !ok || len(seqs) == 0 {
                return maildir.Full{}, fmt.Errorf("%w: uid %s", ErrNoMessage, id)
        }
        var seqSet imap.SeqSet
        nums, _ := seqs.Nums()
        seqSet.AddNum(nums...)

        return fetchFull(c, seqSet, id, false), nil
}

// fetchFull runs one FETCH and shapes the result. headersOnly asks for
// the header section only (the Truncated path).
func fetchFull(c *imapclient.Client, seqSet imap.SeqSet, id string, headersOnly bool) maildir.Full {
        whole := &imap.FetchItemBodySection{Peek: true}
        header := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
        opts := &imap.FetchOptions{
                UID:          true,
                Flags:        true,
                Envelope:     true,
                InternalDate: true,
                RFC822Size:   true,
        }
        if headersOnly {
                opts.BodySection = []*imap.FetchItemBodySection{header}
        } else {
                opts.BodySection = []*imap.FetchItemBodySection{whole}
        }

        bufs, err := c.Fetch(seqSet, opts).Collect()
        if err != nil || len(bufs) == 0 || bufs[0].Envelope == nil {
                return maildir.Full{Message: maildir.Message{ID: id}}
        }
        b := bufs[0]
        full := maildir.Full{Message: messageFromBuffer(b)}
        raw := b.FindBodySection(whole)
        if len(raw) > maildir.MaxMessageBytes {
                // Too big to materialize: headers only, honestly marked.
                return maildir.Full{Message: full.Message, Truncated: true}
        }
        if len(raw) > 0 {
                body, wasHTML, err := maildir.ExtractBody(raw)
                if err == nil {
                        full.Body, full.WasHTML = body, wasHTML
                }
        }
        return full
}

// messageFromBuffer shapes one FETCH response into the shared summary.
func messageFromBuffer(b *imapclient.FetchMessageBuffer) maildir.Message {
        m := maildir.Message{
                ID:   strconv.FormatUint(uint64(b.UID), 10),
                Size: b.RFC822Size,
        }
        if b.Envelope != nil {
                m.From = formatAddresses(b.Envelope.From)
                m.To = formatAddresses(b.Envelope.To)
                m.Subject = strings.TrimSpace(b.Envelope.Subject)
                if !b.Envelope.Date.IsZero() {
                        m.Date = b.Envelope.Date
                }
        }
        if m.Date.IsZero() && !b.InternalDate.IsZero() {
                m.Date = b.InternalDate
        }
        for _, f := range b.Flags {
                switch f {
                case imap.FlagSeen:
                        m.Seen = true
                case imap.FlagAnswered:
                        m.Answered = true
                case imap.FlagFlagged:
                        m.Flagged = true
                case imap.FlagDeleted:
                        m.Trashed = true
                }
        }
        return m
}

// formatAddresses renders an ENVELOPE address list for display: parsed
// names preferred, the bare address as the fallback — the same shape
// maildir's decodeAddressList produces.
func formatAddresses(addrs []imap.Address) string {
        parts := make([]string, 0, len(addrs))
        for _, a := range addrs {
                name := strings.TrimSpace(a.Name)
                if name != "" {
                        parts = append(parts, name)
                        continue
                }
                if a.Mailbox != "" && a.Host != "" {
                        parts = append(parts, a.Mailbox+"@"+a.Host)
                }
        }
        return strings.Join(parts, ", ")
}

// attrListContains reports whether any wanted attribute is present.
func attrListContains(attrs []imap.MailboxAttr, wanted ...imap.MailboxAttr) bool {
        for _, a := range attrs {
                for _, w := range wanted {
                        if a == w {
                                return true
                        }
                }
        }
        return false
}

// mapSelectErr shapes a SELECT failure: the server's NO on a missing box
// is the widget's 404; anything else is the account's condition.
func mapSelectErr(box string, err error) error {
        msg := strings.ToLower(err.Error())
        if strings.Contains(msg, "nonexistent") || strings.Contains(msg, "no such mailbox") ||
                strings.Contains(msg, "does not exist") || strings.Contains(msg, "unknown mailbox") {
                return fmt.Errorf("%w: %s", ErrNoSuchBox, box)
        }
        return fmt.Errorf("%w: select %q failed: %v", ErrUnavailable, box, err)
}

// uidOf parses a decimal UID string; a malformed id sorts as zero.
func uidOf(id string) uint64 {
        n, _ := strconv.ParseUint(id, 10, 64)
        return n
}
