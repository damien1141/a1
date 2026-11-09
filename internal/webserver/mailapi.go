package webserver

import (
        "context"
        "errors"
        "fmt"
        "net/http"
        "strconv"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/imapmail"
        "github.com/damien1141/a1/internal/maildir"
        "github.com/damien1141/a1/internal/project"
)

// mailapi.go serves the dashboard's mail widget (dashboard chunk 4, grown
// multi-account in chunk 7): three GETs, all behind personalGate at
// registration — the master switch is enforced server-side, never just
// hidden in the UI:
//
//      GET /api/personal/mail/mailboxes?account=   the rail (name, total, unread)
//      GET /api/personal/mail/messages?account&box&limit   one box's latest
//      GET /api/personal/mail/message?account&box&id       one message, body included
//
// Every endpoint reads through the resolved account list
// (PersonalConfig.ResolvedMailAccounts): the legacy mail_dir arrives as
// the "local" account, mail_accounts entries as their own. ?account=
// picks one (the first is the default); the rail response carries the
// account list itself (names and kinds only — never credentials) so the
// widget can render the tabs from the same call that fills the first
// one. The WEB surface is the mail reader's full face: it shows bodies
// (the read pane), which is exactly what the AGENT surface (personal_mail)
// refuses — the envelope is the model's, the letter is the human's. And
// this file never writes MAIL: there is no handler here that could send,
// move, or mark a message, and the IMAP path composes read-only SELECTs
// and BODY.PEEK fetches, so the wire account's \Seen stays exactly where
// the server set it. (Chunk 8 added the one write the surface does own —
// the ACCOUNT LIST, in mailaccountsapi.go: the keyring, not the letters.)
//
// Hostile inputs follow the usage endpoint's doctrine where a fallback
// exists (limit: unparsable or non-positive ⇒ default 50, oversize ⇒
// clamp 200) and answer honestly where one does not (a traversal-shaped
// box is a 400 — the library's single-element rule — a missing box or id
// is a 404, an unknown account is a 400, and a dead or rejecting IMAP
// server is a 502 — the config pointed somewhere that did not answer,
// which is nobody's input mistake). Degraded boxes (binary junk, runaway
// header blocks) answer 200 with honest stats — one corrupt message must
// not take the mailbox down. An unconfigured surface (no accounts at all)
// answers configured:false with empty lists, the card's not-configured
// state.

const (
        // mailDefaultLimit is the list pane's page ("latest 50").
        mailDefaultLimit = 50
        // mailMaxLimit bounds a hostile ?limit=.
        mailMaxLimit = 200
)

// mailAccountJSON is one resolved account over the wire: the label the
// widget renders and the ?account= key that selects it. Never carries
// credentials — the password stays in config.yaml where it lives.
type mailAccountJSON struct {
        Name  string `json:"name"`
        Type  string `json:"type"`
        Error string `json:"error,omitempty"`
}

// mailboxJSON is one rail entry over the wire.
type mailboxJSON struct {
        Name   string `json:"name"`
        Total  int    `json:"total"`
        Unread int    `json:"unread"`
}

// mailStatsJSON is the honest accounting: what the scan saw, not just
// what it served. "degraded" is the one-line summary surfaces check.
type mailStatsJSON struct {
        Files    int  `json:"files"`
        Skipped  int  `json:"skipped"`
        Degraded bool `json:"degraded"`
}

// mailMessageJSON is one message over the wire. The body (and its two
// markers) ride only on the message endpoint — listings are headers.
type mailMessageJSON struct {
        ID        string `json:"id"`
        From      string `json:"from,omitempty"`
        To        string `json:"to,omitempty"`
        Subject   string `json:"subject,omitempty"`
        Date      string `json:"date"`
        Size      int64  `json:"size"`
        Seen      bool   `json:"seen"`
        Answered  bool   `json:"answered"`
        Flagged   bool   `json:"flagged"`
        Trashed   bool   `json:"trashed"`
        Body      string `json:"body,omitempty"`
        Truncated bool   `json:"truncated,omitempty"`
        WasHTML   bool   `json:"wasHtml,omitempty"`
}

type mailMailboxesResponse struct {
        Configured bool              `json:"configured"`
        Accounts   []mailAccountJSON `json:"accounts,omitempty"`
        Account    string            `json:"account,omitempty"`
        Error      string            `json:"error,omitempty"`
        Mailboxes  []mailboxJSON     `json:"mailboxes"`
}

type mailMessagesResponse struct {
        Configured bool              `json:"configured"`
        Account    string            `json:"account,omitempty"`
        Box        string            `json:"box,omitempty"`
        Messages   []mailMessageJSON `json:"messages"`
        Stats      mailStatsJSON     `json:"stats"`
}

type mailMessageResponse struct {
        Configured bool            `json:"configured"`
        Account    string          `json:"account,omitempty"`
        Box        string          `json:"box,omitempty"`
        Message    mailMessageJSON `json:"message"`
}

func mailMessageToJSON(m maildir.Message) mailMessageJSON {
        return mailMessageJSON{
                ID:       m.ID,
                From:     m.From,
                To:       m.To,
                Subject:  m.Subject,
                Date:     m.Date.Format(time.RFC3339),
                Size:     m.Size,
                Seen:     m.Seen,
                Answered: m.Answered,
                Flagged:  m.Flagged,
                Trashed:  m.Trashed,
        }
}

// resolveMailAccount normalizes ?account= against the resolved list: the
// empty name is the first account, an unknown name is a client mistake
// (the caller answers 400), and the account metadata rides back for the
// response's accounts field.
func resolveMailAccount(accts []project.MailAccount, name string) (*project.MailAccount, []mailAccountJSON, bool) {
        meta := make([]mailAccountJSON, 0, len(accts))
        for _, a := range accts {
                meta = append(meta, mailAccountJSON{Name: a.Name, Type: a.Type})
        }
        if len(accts) == 0 {
                return nil, meta, false
        }
        name = strings.TrimSpace(name)
        if name == "" {
                return &accts[0], meta, true
        }
        for i := range accts {
                if accts[i].Name == name {
                        return &accts[i], meta, true
                }
        }
        return nil, meta, false
}

// handlePersonalMailboxes serves the rail. No accounts at all is
// configured:false (the card's not-configured state); the picked
// account's own failure rides the 200 as an Error the widget renders
// inline — the other accounts' names stay visible, so the surface never
// blanks just because one server is down.
func (s *Server) handlePersonalMailboxes(w http.ResponseWriter, r *http.Request) {
        accts := s.personalConfig().ResolvedMailAccounts()
        acct, meta, ok := resolveMailAccount(accts, r.URL.Query().Get("account"))
        if !ok {
                if len(accts) == 0 {
                        writeJSON(w, http.StatusOK, mailMailboxesResponse{Configured: false})
                        return
                }
                writeStatus(w, http.StatusBadRequest, "mail: unknown account")
                return
        }
        resp := mailMailboxesResponse{Configured: true, Accounts: meta, Account: acct.Name, Mailboxes: []mailboxJSON{}}
        boxes, err := readMailboxes(r.Context(), *acct)
        if err != nil {
                for i := range resp.Accounts {
                        if resp.Accounts[i].Name == acct.Name {
                                resp.Accounts[i].Error = err.Error()
                        }
                }
                resp.Error = err.Error()
                writeJSON(w, http.StatusOK, resp)
                return
        }
        out := make([]mailboxJSON, 0, len(boxes))
        for _, b := range boxes {
                out = append(out, mailboxJSON{Name: b.Name, Total: b.Total, Unread: b.Unread})
        }
        resp.Mailboxes = out
        writeJSON(w, http.StatusOK, resp)
}

// handlePersonalMailMessages serves one box's latest messages. limit
// falls back to 50 (unparsable or non-positive) and clamps at 200 —
// the widget's fetch is not a place to teach anyone a lesson.
func (s *Server) handlePersonalMailMessages(w http.ResponseWriter, r *http.Request) {
        accts := s.personalConfig().ResolvedMailAccounts()
        acct, _, ok := resolveMailAccount(accts, r.URL.Query().Get("account"))
        if !ok {
                if len(accts) == 0 {
                        writeJSON(w, http.StatusOK, mailMessagesResponse{Configured: false})
                        return
                }
                writeStatus(w, http.StatusBadRequest, "mail: unknown account")
                return
        }
        box := strings.TrimSpace(r.URL.Query().Get("box"))
        if box == "" {
                writeStatus(w, http.StatusBadRequest, "mail: box is required")
                return
        }
        limit := mailDefaultLimit
        if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
                if n, err := strconv.Atoi(raw); err == nil && n > 0 {
                        if n > mailMaxLimit {
                                n = mailMaxLimit
                        }
                        limit = n
                }
        }
        msgs, st, err := readMailMessages(r.Context(), *acct, box, limit)
        if err != nil {
                s.writeMailErr(w, err)
                return
        }
        out := make([]mailMessageJSON, 0, len(msgs))
        for _, m := range msgs {
                out = append(out, mailMessageToJSON(m))
        }
        writeJSON(w, http.StatusOK, mailMessagesResponse{
                Configured: true,
                Account:    acct.Name,
                Box:        box,
                Messages:   out,
                Stats: mailStatsJSON{
                        Files:    st.Files,
                        Skipped:  st.Skipped,
                        Degraded: st.Degraded(),
                },
        })
}

// handlePersonalMailMessage serves one message with its body — the read
// pane's fetch, and the one surface where the web holds more than the
// agent ever gets.
func (s *Server) handlePersonalMailMessage(w http.ResponseWriter, r *http.Request) {
        accts := s.personalConfig().ResolvedMailAccounts()
        acct, _, ok := resolveMailAccount(accts, r.URL.Query().Get("account"))
        if !ok {
                if len(accts) == 0 {
                        writeJSON(w, http.StatusOK, mailMessageResponse{Configured: false})
                        return
                }
                writeStatus(w, http.StatusBadRequest, "mail: unknown account")
                return
        }
        q := r.URL.Query()
        box := strings.TrimSpace(q.Get("box"))
        id := strings.TrimSpace(q.Get("id"))
        if box == "" || id == "" {
                writeStatus(w, http.StatusBadRequest, "mail: box and id are required")
                return
        }
        full, err := readMailMessage(r.Context(), *acct, box, id)
        if err != nil {
                s.writeMailErr(w, err)
                return
        }
        msg := mailMessageToJSON(full.Message)
        msg.Body = full.Body
        msg.Truncated = full.Truncated
        msg.WasHTML = full.WasHTML
        writeJSON(w, http.StatusOK, mailMessageResponse{Configured: true, Account: acct.Name, Box: box, Message: msg})
}

// readMailboxes / readMailMessages / readMailMessage dispatch on the
// account's kind. These three are the ONLY place the two transports meet:
// both return the maildir package's types, so everything downstream (the
// JSON shapes, the widget) carries one wire shape no matter where the
// letters live.
func readMailboxes(ctx context.Context, acct project.MailAccount) ([]maildir.Mailbox, error) {
        switch acct.Type {
        case "imap":
                return imapmail.Boxes(ctx, acct)
        case "maildir":
                root := strings.TrimSpace(acct.Root)
                if root == "" {
                        return nil, fmt.Errorf("mail account %q has no root", acct.Name)
                }
                return maildir.Mailboxes(root)
        default:
                return nil, fmt.Errorf("mail account %q has unknown type %q", acct.Name, acct.Type)
        }
}

func readMailMessages(ctx context.Context, acct project.MailAccount, box string, limit int) ([]maildir.Message, maildir.Stats, error) {
        switch acct.Type {
        case "imap":
                msgs, err := imapmail.List(ctx, acct, box, limit)
                if err != nil {
                        return nil, maildir.Stats{}, err
                }
                return msgs, maildir.Stats{Files: len(msgs)}, nil
        case "maildir":
                root := strings.TrimSpace(acct.Root)
                if root == "" {
                        return nil, maildir.Stats{}, fmt.Errorf("mail account %q has no root", acct.Name)
                }
                return maildir.List(root, box, limit)
        default:
                return nil, maildir.Stats{}, fmt.Errorf("mail account %q has unknown type %q", acct.Name, acct.Type)
        }
}

func readMailMessage(ctx context.Context, acct project.MailAccount, box, id string) (maildir.Full, error) {
        switch acct.Type {
        case "imap":
                return imapmail.Open(ctx, acct, box, id)
        case "maildir":
                root := strings.TrimSpace(acct.Root)
                if root == "" {
                        return maildir.Full{}, fmt.Errorf("mail account %q has no root", acct.Name)
                }
                return maildir.Open(root, box, id)
        default:
                return maildir.Full{}, fmt.Errorf("mail account %q has unknown type %q", acct.Name, acct.Type)
        }
}

// writeMailErr maps the readers' error classes to honest statuses:
// client mistakes (traversal-shaped names, malformed ids, unknown
// accounts) are 400s, honest misses (a box or message that is not
// there) are 404s, an account whose server is dead or rejecting is a
// 502 (the config pointed somewhere that did not answer), and nothing
// is a 500 unless the filesystem itself failed.
func (s *Server) writeMailErr(w http.ResponseWriter, err error) {
        switch {
        case errors.Is(err, maildir.ErrBadBox), errors.Is(err, maildir.ErrBadID), errors.Is(err, imapmail.ErrBadID):
                writeStatus(w, http.StatusBadRequest, "mail: "+err.Error())
        case errors.Is(err, maildir.ErrNoSuchBox), errors.Is(err, maildir.ErrNoMessage),
                errors.Is(err, imapmail.ErrNoSuchBox), errors.Is(err, imapmail.ErrNoMessage):
                writeStatus(w, http.StatusNotFound, "mail: "+err.Error())
        case errors.Is(err, imapmail.ErrAuth), errors.Is(err, imapmail.ErrUnavailable):
                writeStatus(w, http.StatusBadGateway, "mail: "+err.Error())
        default:
                writeStatus(w, http.StatusInternalServerError, "mail: "+err.Error())
        }
}
