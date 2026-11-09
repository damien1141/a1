package webserver

import (
        "context"
        "net/http"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/project"
)

// mailaccountsapi.go is the dashboard's mail CONFIGURATOR (chunk 8) — the
// humane answer to "configure multiple accounts and mail servers" that
// chunk 7's config-file spelling only half-answered. Three endpoints:
//
//      GET  /api/personal/mail/accounts    the saved list (never passwords)
//      PUT  /api/personal/mail/accounts    replace the whole list
//      POST /api/personal/mail/test        dial one spec, persist nothing
//
// The ruling that lets these exist beside chunk 4's "nobody writes here":
// that law governs MAIL — messages, flags, the letters — and it stands
// untouched (the readers stay BODY.PEEK, there is still no handler that
// could send, move, or mark a message). This file writes the KEYRING, not
// the letters: the account list is configuration, the same object the
// /settings editor already edits, and the one thing the user asked to stop
// hand-writing YAML for. The write ladder for mail therefore reads: config
// yes (here), data never (anywhere).
//
// The credential contract — the wire never carries a password it did not
// have to:
//
//   - GET returns hasPassword, never the password. An editor that cannot
//     show a secret cannot leak one.
//   - PUT entries carry password (set) or keepPassword (preserve). Blank
//     + keep means "the field the client never saw stays as it was":
//     named entries resolve by name, unnamed ones by their type-and-
//     server fingerprint (the collision the browser pass caught — two
//     accounts saved without names both key on the empty string), and a
//     keep only lands on a UNIQUE match so edits and deletions cannot
//     shuffle credentials onto the wrong account the way an index-
//     anchored keep could. The cost is stated in the UI: renaming an
//     account does not carry its password — re-enter it after a rename.
//   - The test endpoint takes the same single-spec body (keepPassword
//     resolved server-side the same way) because "test the saved account
//     without retyping its app password" is the honest test. It dials,
//     reports, and persists NOTHING — the disk is not a scratchpad for
//     things the user only wanted to try.
//
// Validation is the configurator's own doctrine, deliberately stricter
// than the read path's lenient degrade: hand-edited YAML may carry a
// half-written account and the rail will render its error inline (chunk
// 7's ruling — configuration mistakes must be visible, never fatal), but
// THIS surface refuses to save a shape it knows is wrong (unknown type,
// imap without host, maildir without root, port out of range) with a 400.
// A configurator that saved known-bad entries would be a slower way to
// hand-edit YAML. The list is capped at 32 entries — bounds the body, not
// the ambition; the read path stays uncapped because it reads what the
// file says.

// mailAccountMaxBounds the saved list. Thirty-two inboxes is a hobby, not
// a typo — beyond that the editor is the wrong tool.
const mailAccountMax = 32

// mailTestTimeout bounds one test dial: a hung server must not hold the
// PUT's follow-up or the button's spinner forever.
const mailTestTimeout = 10 * time.Second

// mailAccountEntryJSON is one SAVED account over the wire: exactly the
// fields the editor edits, minus the one it must never see. hasPassword
// tells the edit form whether a blank field means "keep" (has one) or
// "empty" (does not).
type mailAccountEntryJSON struct {
        Name        string `json:"name"`
        Type        string `json:"type"`
        Root        string `json:"root,omitempty"`
        Host        string `json:"host,omitempty"`
        Port        int    `json:"port,omitempty"`
        Username    string `json:"username,omitempty"`
        Mailbox     string `json:"mailbox,omitempty"`
        TLS         *bool  `json:"tls,omitempty"`
        HasPassword bool   `json:"hasPassword"`
}

// mailAccountsResponse is the configurator's whole read model: the legacy
// mail_dir (shown as a pinned read-only row — this surface does not own
// that key), the saved entries, and the resolved account list (the same
// pills the rail serves, so the editor can show what a name resolves to
// without re-deriving the de-duplication rules client-side).
type mailAccountsResponse struct {
        LegacyDir string                `json:"legacyDir,omitempty"`
        Entries   []mailAccountEntryJSON `json:"entries"`
        Resolved  []mailAccountJSON     `json:"resolved"`
}

// mailAccountSpecJSON is one entry in the PUT body, and the whole body of
// the test endpoint. Password is write-only: it rides TO the server and
// never back.
type mailAccountSpecJSON struct {
        Name         string `json:"name"`
        Type         string `json:"type"`
        Root         string `json:"root"`
        Host         string `json:"host"`
        Port         int    `json:"port"`
        Username     string `json:"username"`
        Password     string `json:"password"`
        KeepPassword bool   `json:"keepPassword"`
        Mailbox      string `json:"mailbox"`
        TLS          *bool  `json:"tls"`
}

type mailAccountsPutJSON struct {
        Accounts []mailAccountSpecJSON `json:"accounts"`
}

// mailTestResponse is the test dial's honest report: ok with the box
// count (and a few names as proof of life), or not ok with the reason.
// A failed dial is a 200 — the endpoint did its job; the server being
// down is the answer, not an error in getting it.
type mailTestResponse struct {
        OK     bool     `json:"ok"`
        Boxes  int      `json:"boxes,omitempty"`
        Sample []string `json:"sample,omitempty"`
        Error  string   `json:"error,omitempty"`
}

// buildMailAccountsResponse projects a personal config into the
// configurator's read model. Entry type is normalized for display (""
// means maildir in the file); everything else rides as saved.
func buildMailAccountsResponse(p project.PersonalConfig) mailAccountsResponse {
        resp := mailAccountsResponse{
                LegacyDir: p.MailDir,
                Entries:   make([]mailAccountEntryJSON, 0, len(p.MailAccounts)),
                Resolved:  make([]mailAccountJSON, 0, len(p.MailAccounts)),
        }
        for _, a := range p.MailAccounts {
                t := strings.ToLower(strings.TrimSpace(a.Type))
                if t == "" {
                        t = "maildir"
                }
                resp.Entries = append(resp.Entries, mailAccountEntryJSON{
                        Name:        a.Name,
                        Type:        t,
                        Root:        a.Root,
                        Host:        a.Host,
                        Port:        a.Port,
                        Username:    a.Username,
                        Mailbox:     a.Mailbox,
                        TLS:         a.TLS,
                        HasPassword: a.Password != "",
                })
        }
        for _, a := range p.ResolvedMailAccounts() {
                resp.Resolved = append(resp.Resolved, mailAccountJSON{Name: a.Name, Type: a.Type})
        }
        return resp
}

// normalizeMailAccountSpec validates and normalizes one spec into the
// shape project.SetMailAccounts writes. status 0 means ok; anything else
// is the refusal's status with msg its reason. Password handling is NOT
// done here — the caller merges keepPassword against the saved list,
// because only it knows what is currently on disk.
func normalizeMailAccountSpec(spec mailAccountSpecJSON) (project.MailAccount, int, string) {
        acct := project.MailAccount{
                Name:     strings.TrimSpace(spec.Name),
                Root:     strings.TrimSpace(spec.Root),
                Host:     strings.TrimSpace(spec.Host),
                Port:     spec.Port,
                Username: strings.TrimSpace(spec.Username),
                Password: spec.Password, // never trimmed: an app password is literal
                TLS:      spec.TLS,
                Mailbox:  strings.TrimSpace(spec.Mailbox),
        }
        acct.Type = strings.ToLower(strings.TrimSpace(spec.Type))
        if acct.Type == "" {
                acct.Type = "maildir"
        }
        if acct.Root != "" {
                acct.Root = project.ExpandHome(acct.Root)
        }
        switch acct.Type {
        case "maildir":
                if acct.Root == "" {
                        return project.MailAccount{}, http.StatusBadRequest, "mail account of type maildir needs a root"
                }
        case "imap":
                if acct.Host == "" {
                        return project.MailAccount{}, http.StatusBadRequest, "mail account of type imap needs a host"
                }
        default:
                return project.MailAccount{}, http.StatusBadRequest, "mail account type must be maildir or imap"
        }
        if acct.Port < 0 || acct.Port > 65535 {
                return project.MailAccount{}, http.StatusBadRequest, "mail account port must be 1-65535 (or 0 for the default)"
        }
        return acct, 0, ""
}

// mergeMailPassword applies the keep rule: an entry that kept its
// password (blank field + keepPassword) inherits the saved password of
// the account it IS. Named entries match by name; UNNAMED entries match
// by fingerprint (type + root | host) — the collision the browser pass
// caught: two accounts saved without names both key on "", and a
// first-match rule would hand the maildir's empty password to the IMAP
// account (or worse, the reverse). And a keep only lands on a UNIQUE
// match: zero candidates or two indistinguishable ones both mean the
// credential does not carry — the account degrades honestly at read
// time and the test button says so before the save ever lands.
func mergeMailPassword(spec mailAccountSpecJSON, acct project.MailAccount, old []project.MailAccount) project.MailAccount {
        if !spec.KeepPassword {
                return acct
        }
        name := strings.TrimSpace(spec.Name)
        var candidates []string
        if name != "" {
                for _, o := range old {
                        if strings.TrimSpace(o.Name) == name {
                                candidates = append(candidates, o.Password)
                        }
                }
        } else {
                fp := mailAccountFingerprint(acct)
                for _, o := range old {
                        if strings.TrimSpace(o.Name) == "" && mailAccountFingerprint(o) == fp {
                                candidates = append(candidates, o.Password)
                        }
                }
        }
        if len(candidates) == 1 {
                acct.Password = candidates[0]
        }
        return acct
}

// mailAccountFingerprint is an unnamed account's identity: its kind and
// where it lives (root for a maildir, host for an IMAP server). Two
// accounts with the same fingerprint and no names are indistinguishable
// to the keep rule — by design, that ambiguity refuses to carry.
func mailAccountFingerprint(a project.MailAccount) string {
        t := strings.ToLower(strings.TrimSpace(a.Type))
        if t == "" {
                t = "maildir"
        }
        if t == "imap" {
                return "imap|" + strings.TrimSpace(a.Host)
        }
        return "maildir|" + strings.TrimSpace(a.Root)
}

// handleMailAccountsList serves the saved list. personalGate at
// registration: account names and hosts are gated content (the ungated
// status endpoint carries a count, never a label).
func (s *Server) handleMailAccountsList(w http.ResponseWriter, _ *http.Request) {
        writeJSON(w, http.StatusOK, buildMailAccountsResponse(s.personalConfig()))
}

// handleMailAccountsSave replaces the whole saved list. Composition is
// the kanban precedent: personalGate(guard(...)) — the master switch
// governs the surface, and a cross-origin page must not be able to edit
// the keyring. On success the project config is reloaded and live
// engines rebind their personal tools (onConfigSaved), so the new
// account is on the rail — and in the agent's facade — without a
// restart; the response is the fresh read model so the editor updates in
// one round-trip.
func (s *Server) handleMailAccountsSave(w http.ResponseWriter, r *http.Request) {
        if !s.requireProject(w) {
                return
        }
        var req mailAccountsPutJSON
        if err := decodeBody(r, &req); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        if len(req.Accounts) > mailAccountMax {
                writeStatus(w, http.StatusBadRequest, "too many mail accounts (max 32)")
                return
        }
        old := s.personalConfig().MailAccounts
        accts := make([]project.MailAccount, 0, len(req.Accounts))
        for _, spec := range req.Accounts {
                if spec.Password != "" && spec.KeepPassword {
                        writeStatus(w, http.StatusBadRequest, "mail account cannot both set and keep its password")
                        return
                }
                acct, status, msg := normalizeMailAccountSpec(spec)
                if status != 0 {
                        writeStatus(w, status, msg)
                        return
                }
                accts = append(accts, mergeMailPassword(spec, acct, old))
        }
        if err := project.SetMailAccounts(s.proj.Global(), accts); err != nil {
                writeStatus(w, http.StatusInternalServerError, "save mail accounts: "+err.Error())
                return
        }
        s.onConfigSaved()
        writeJSON(w, http.StatusOK, buildMailAccountsResponse(s.personalConfig()))
}

// handleMailAccountTest dials ONE spec and reports. It resolves
// keepPassword against the saved list (so a saved account can be tested
// without its password ever crossing the wire) and persists nothing. A
// dead or rejecting server is a 200 with ok:false — the answer the
// button exists to fetch; only shape mistakes are 400s.
func (s *Server) handleMailAccountTest(w http.ResponseWriter, r *http.Request) {
        var spec mailAccountSpecJSON
        if err := decodeBody(r, &spec); err != nil {
                writeStatus(w, http.StatusBadRequest, err.Error())
                return
        }
        if spec.Password != "" && spec.KeepPassword {
                writeStatus(w, http.StatusBadRequest, "mail account cannot both set and keep its password")
                return
        }
        acct, status, msg := normalizeMailAccountSpec(spec)
        if status != 0 {
                writeStatus(w, status, msg)
                return
        }
        acct = mergeMailPassword(spec, acct, s.personalConfig().MailAccounts)
        if acct.Name == "" {
                // The dial's error messages quote the account's name; a
                // spec being tested before it is named still deserves a
                // label that means something.
                if acct.Host != "" {
                        acct.Name = acct.Host
                } else if acct.Root != "" {
                        acct.Name = acct.Root
                } else {
                        acct.Name = "account"
                }
        }
        ctx, cancel := context.WithTimeout(r.Context(), mailTestTimeout)
        defer cancel()
        boxes, err := readMailboxes(ctx, acct)
        if err != nil {
                writeJSON(w, http.StatusOK, mailTestResponse{OK: false, Error: err.Error()})
                return
        }
        resp := mailTestResponse{OK: true, Boxes: len(boxes)}
        for i, b := range boxes {
                if i >= 5 {
                        break
                }
                resp.Sample = append(resp.Sample, b.Name)
        }
        writeJSON(w, http.StatusOK, resp)
}
