// Package maildir reads a local Maildir tree — the personal-data surface
// behind the dashboard's mail widget (dashboard chunk 4) and the agent's
// personal_mail tool. It is READ-ONLY by ruling, for the model exactly as
// for the webui: there is no API here that moves, marks, or deletes a
// message, and none will be added (sending mail is out of scope for v1
// too — see TODO.md's anti-goals). Flags are read from the filename and
// never written back.
//
// The layout treated here is a DIRECTORY OF MAILBOXES: every subdirectory
// of the root that carries a cur/ or new/ (tmp/ is ignored — it holds
// messages mid-delivery) is one mailbox. The root itself is not a mailbox,
// and nested Maildir++ folders (".Archive.2026") are not expanded — v1
// keeps one flat rail.
//
// The doctrine throughout mirrors internal/calendar: malformed content is
// skipped-with-count, never fatal — one corrupt message must not take the
// mailbox down. Sizes are capped (a hostile "mailbox" that is really a
// disk dump must not be slurped whole), and bodies are TEXT: text/plain
// verbatim, text/html stripped to text. No remote content is ever fetched
// — there is no fetching code in this package at all (the privacy ruling).
package maildir

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Size caps. maxHeaderBytes bounds the prefix a listing parses (header
// blocks live far below this; a file whose headers run past it is counted
// skipped, not slurped). MaxMessageBytes bounds a whole-file read for
// Open — beyond it the caller gets headers with Truncated set, never a
// partial body presented as the whole one.
const (
	maxHeaderBytes = 128 << 10
	// MaxMessageBytes bounds a whole-message read for Open (and for any
	// other reader that materializes a message, like the IMAP account
	// path). Beyond it the caller gets headers with Truncated set, never
	// a partial body presented as the whole one.
	MaxMessageBytes = 8 << 20
)

// Errors. The web handlers map these: ErrBadBox/ErrBadID are client
// mistakes (400), ErrNoSuchBox/ErrNoMessage are honest misses (404).
var (
	ErrBadBox    = errors.New("maildir: invalid mailbox name")
	ErrBadID     = errors.New("maildir: invalid message id")
	ErrNoSuchBox = errors.New("maildir: no such mailbox")
	ErrNoMessage = errors.New("maildir: no such message")
)

// Mailbox is one mailbox under the root: its name (the subdirectory) and
// filename-level counts. Total counts every message file in cur/ + new/;
// Unread counts new/ (unseen by definition) plus cur/ files without the
// S flag. Both come from directory reads only — no file bodies touched.
type Mailbox struct {
	Name   string
	Total  int
	Unread int
}

// Message is one message's summary, as a listing shows it. Date is the
// Date header parsed; when the header is missing or unparseable the
// file's mtime is the honest fallback (when it landed on disk), so
// ordering stays total. ID is the Maildir unique key — the filename
// before the info separator — never a filesystem path.
type Message struct {
	ID       string
	From     string
	To       string
	Subject  string
	Date     time.Time
	Size     int64
	Seen     bool
	Answered bool
	Flagged  bool
	Trashed  bool
}

// Full is one message with its body, as Open returns it. Body is text:
// text/plain verbatim, text/html stripped (script/style contents dropped,
// tags to newlines, entities decoded). Truncated means the file exceeded
// the size cap and only headers were read — an honest refusal, not a
// silently short body. WasHTML marks a body recovered from an HTML part.
type Full struct {
	Message
	Body      string
	Truncated bool
	WasHTML   bool
}

// Stats is the honest accounting: what a scan saw, not just what it
// served. "degraded" is the one-line summary surfaces check.
type Stats struct {
	Files   int // message files considered
	Skipped int // unreadable or header-unparsable files
}

// Degraded reports whether anything was skipped.
func (s Stats) Degraded() bool { return s.Skipped > 0 }

// ---------- name validation ----------

// validBox enforces the one rule that matters: a mailbox name is a single
// path element. Separators (either flavor), ".", "..", NUL, and the empty
// string are refused — that is the whole path-traversal story, checked
// before any filesystem call runs.
func validBox(box string) bool {
	if box == "" || box == "." || box == ".." {
		return false
	}
	if strings.ContainsAny(box, "/\\\x00") {
		return false
	}
	return true
}

// validID is validBox for message ids: the id names a file, so it gets
// the same single-element discipline.
func validID(id string) bool { return validBox(id) }

// splitMaildirName splits a Maildir filename into its unique key and its
// info suffix. Per spec the info starts at the first ':' ("uniq:2,FLAGS");
// filenames without one (new/ delivery, or a cur/ file a sloppy tool left
// bare) are tolerated — the whole name is the key and the info is empty.
func splitMaildirName(name string) (uniq, info string) {
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[:i], name[i+1:]
	}
	return name, ""
}

// parseFlags reads the Maildir flag letters out of an info suffix
// ("2,RS" → seen+answered). Anything not "2,..." is a legacy/unknown info
// block: tolerated, no flags.
func parseFlags(info string) (seen, answered, flagged, trashed bool) {
	rest, ok := strings.CutPrefix(info, "2,")
	if !ok {
		return false, false, false, false
	}
	for _, c := range rest {
		switch c {
		case 'S':
			seen = true
		case 'R':
			answered = true
		case 'F':
			flagged = true
		case 'T':
			trashed = true
		}
	}
	return seen, answered, flagged, trashed
}

// ---------- mailbox rail ----------

// Mailboxes lists the root's mailboxes sorted by name. A missing root is
// an empty rail, not an error — "not configured" vs "empty" is a config
// distinction that lives one level up. Any other root error (permissions)
// is returned. Subdirectory trouble degrades to zero counts here; List
// surfaces the details when the box is opened.
func Mailboxes(root string) ([]Mailbox, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Mailbox
	for _, e := range entries {
		if !e.IsDir() {
			continue // stray files (READMEs, .DS_Store) are not mailboxes
		}
		name := e.Name()
		if !validBox(name) {
			continue // unaddressable name; the rail cannot link to it anyway
		}
		if !isMailboxDir(filepath.Join(root, name)) {
			continue // no cur/ and no new/: a plain directory, not a mailbox
		}
		total, unread := countBox(filepath.Join(root, name))
		out = append(out, Mailbox{Name: name, Total: total, Unread: unread})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// countBox counts one mailbox's messages from filenames alone.
func countBox(dir string) (total, unread int) {
	if ents, err := os.ReadDir(filepath.Join(dir, "cur")); err == nil {
		for _, e := range ents {
			if !e.Type().IsRegular() {
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue // dotfiles are mailbox metadata (.keep, .DS_Store), never messages
			}
			total++
			_, info := splitMaildirName(e.Name())
			seen, _, _, _ := parseFlags(info)
			if !seen {
				unread++
			}
		}
	}
	if ents, err := os.ReadDir(filepath.Join(dir, "new")); err == nil {
		for _, e := range ents {
			if !e.Type().IsRegular() {
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue // same rule in new/: metadata is not mail
			}
			total++
			unread++ // new/ is unseen by definition
		}
	}
	return total, unread
}

// isMailboxDir reports whether the directory carries a cur/ or new/.
func isMailboxDir(dir string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// ---------- message listing ----------

// rawMsg is one file found by a box scan, before parsing.
type rawMsg struct {
	path string
	id   string
	seen bool
	ans  bool
	flg  bool
	trs  bool
	info fs.FileInfo
}

// scanBox collects a box's message files (cur/ + new/, tmp/ never read).
// A missing box directory is ErrNoSuchBox — a typo'd name must not read
// as an empty mailbox; a box that exists but is empty returns nothing.
func scanBox(root, box string) ([]rawMsg, error) {
	dir := filepath.Join(root, box)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: %s", ErrNoSuchBox, box)
	}
	var out []rawMsg
	for _, sub := range []string{"cur", "new"} {
		isNew := sub == "new"
		ents, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			continue // absent or unreadable half: degrade, the other still lists
		}
		for _, e := range ents {
			if !e.Type().IsRegular() {
				continue // subdirectories and symlinks are not messages
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue // dotfiles in cur/new are metadata, never messages
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			uniq, info := splitMaildirName(e.Name())
			seen, ans, flg, trs := parseFlags(info)
			if isNew {
				seen = false // new/ is unseen whatever a stray suffix claims
			}
			out = append(out, rawMsg{
				path: filepath.Join(dir, sub, e.Name()),
				id:   uniq,
				seen: seen, ans: ans, flg: flg, trs: trs,
				info: fi,
			})
		}
	}
	return out, nil
}

// fileMemo is the per-file header cache: a file whose mtime AND size are
// unchanged is not re-parsed (the calendar's doctrine — either alone can
// be forged, the pair is cheap to check). Callers never persist anything;
// "no vault storage until profiling demands it" is the plan's ruling.
type fileMemo struct {
	mod     time.Time
	size    int64
	msg     Message
	skipped bool
}

var (
	scanMu   sync.Mutex
	scanMemo = map[string]*fileMemo{}
)

// List returns a box's message summaries, latest first (Date desc, id
// asc as the tiebreaker — deterministic). limit <= 0 means all; the
// endpoints own the default-and-clamp policy, the library stays honest.
func List(root, box string, limit int) ([]Message, Stats, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, Stats{}, fmt.Errorf("%w: (empty)", ErrBadBox)
	}
	if !validBox(box) {
		return nil, Stats{}, fmt.Errorf("%w: %q", ErrBadBox, box)
	}
	raw, err := scanBox(root, box)
	if err != nil {
		return nil, Stats{}, err
	}

	scanMu.Lock()
	defer scanMu.Unlock()

	var st Stats
	msgs := make([]Message, 0, len(raw))
	for _, r := range raw {
		st.Files++
		if m, ok := scanMemo[r.path]; ok && m.mod.Equal(r.info.ModTime()) && m.size == r.info.Size() {
			if m.skipped {
				st.Skipped++
				continue
			}
			msgs = append(msgs, m.msg)
			continue
		}
		msg, err := parseHeaders(r.path, r)
		if err != nil {
			st.Skipped++
			scanMemo[r.path] = &fileMemo{mod: r.info.ModTime(), size: r.info.Size(), skipped: true}
			continue
		}
		scanMemo[r.path] = &fileMemo{mod: r.info.ModTime(), size: r.info.Size(), msg: msg}
		msgs = append(msgs, msg)
	}

	sort.SliceStable(msgs, func(i, j int) bool {
		if !msgs[i].Date.Equal(msgs[j].Date) {
			return msgs[i].Date.After(msgs[j].Date)
		}
		return msgs[i].ID < msgs[j].ID
	})
	if limit > 0 && len(msgs) > limit {
		msgs = msgs[:limit]
	}
	return msgs, st, nil
}

// Summary returns one message's headers, addressed by id. It is the
// agent facade's read surface: the ENVELOPE without the letter — Open
// (bodies) stays a webui privilege under chunk 4's privacy ruling, so
// the model can triage mail it may not read. Same location rules as
// Open: the file is found by scanning cur/ + new/, so any flag suffix
// matches, and tmp/ is invisible.
func Summary(root, box, id string) (Message, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Message{}, fmt.Errorf("%w: (empty)", ErrBadBox)
	}
	if !validBox(box) {
		return Message{}, fmt.Errorf("%w: %q", ErrBadBox, box)
	}
	if !validID(id) {
		return Message{}, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	raw, err := scanBox(root, box)
	if err != nil {
		return Message{}, err
	}
	for i := range raw {
		if raw[i].id == id {
			msg, err := parseHeaders(raw[i].path, raw[i])
			if err != nil {
				if errors.Is(err, errHeaderRunaway) {
					return Message{}, fmt.Errorf("%w: %s (header block exceeds the size cap)", ErrNoMessage, id)
				}
				return Message{}, err
			}
			return msg, nil
		}
	}
	return Message{}, fmt.Errorf("%w: %s", ErrNoMessage, id)
}

// errHeaderRunaway marks a file whose header block never terminated
// within the cap — a "message" that is really a disk dump. List skips
// it with a count; the by-id surfaces translate it to ErrNoMessage, so
// what a listing refuses to show is not addressable either.
var errHeaderRunaway = errors.New("maildir: header block exceeds the size cap")

// headerTerminatedWithin reports whether the prefix contains the blank
// header/body separator, in any of the line-ending styles real mail
// uses (LF, CRLF, or the mix).
func headerTerminatedWithin(prefix []byte) bool {
	return bytes.Contains(prefix, []byte("\n\n")) || bytes.Contains(prefix, []byte("\n\r\n"))
}

// parseHeaders reads one message's header block (capped prefix) and
// builds its summary. The body is never touched here — listings are
// headers-only by design. The prefix is probed one byte PAST the cap: a
// block that runs past maxHeaderBytes without a blank line is a runaway
// (the documented skip), never slurped — net/mail would otherwise parse
// the truncated stub as a headerless message and list it as mail from
// nobody about nothing.
func parseHeaders(path string, r rawMsg) (Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return Message{}, err
	}
	defer f.Close()
	prefix := make([]byte, maxHeaderBytes+1)
	n, _ := io.ReadFull(f, prefix)
	if n > maxHeaderBytes {
		if !headerTerminatedWithin(prefix[:maxHeaderBytes]) {
			return Message{}, errHeaderRunaway
		}
		n = maxHeaderBytes
	}
	m, err := mail.ReadMessage(bytes.NewReader(prefix[:n]))
	if err != nil {
		return Message{}, err
	}
	return messageFromHeader(m.Header, r), nil
}

// messageFromHeader builds a Message summary off a parsed header block
// plus the filename facts (id, flags, size, mtime fallback).
func messageFromHeader(hdr mail.Header, r rawMsg) Message {
	msg := Message{
		ID:       r.id,
		Size:     r.info.Size(),
		Seen:     r.seen,
		Answered: r.ans,
		Flagged:  r.flg,
		Trashed:  r.trs,
		From:     decodeAddressList(hdr.Get("From")),
		To:       decodeAddressList(hdr.Get("To")),
		Subject:  DecodeHeaderText(hdr.Get("Subject")),
	}
	// Date: the header when parseable, else the file's mtime — a total
	// order that stays honest about where the fact came from (the disk).
	if d, err := mail.ParseDate(hdr.Get("Date")); err == nil {
		msg.Date = d
	} else {
		msg.Date = r.info.ModTime()
	}
	return msg
}

// decodeAddressList renders a From/To value for display: parsed names
// (RFC 2047 decoded by net/mail itself) preferred, the raw value kept
// when parsing fails (a broken address must not blank the sender).
func decodeAddressList(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(v)
	if err != nil {
		return v
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, a.Name)
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

// DecodeHeaderText decodes RFC 2047 encoded words (=?UTF-8?B?...?=);
// plain text passes through unchanged. Exported for the sibling readers
// (the IMAP account path) so a subject renders identically no matter
// which transport carried it.
func DecodeHeaderText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if dec, err := (&mime.WordDecoder{}).DecodeHeader(v); err == nil {
		return dec
	}
	return v
}

// ---------- single message ----------

// Open returns one message with its body. The id is the Maildir unique
// key (as List reports); the file is located by scanning cur/ + new/ and
// matching keys, so any flag suffix is found. An oversized file answers
// with headers only and Truncated set — never a body cut mid-sentence
// and presented as whole.
func Open(root, box, id string) (Full, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return Full{}, fmt.Errorf("%w: (empty)", ErrBadBox)
	}
	if !validBox(box) {
		return Full{}, fmt.Errorf("%w: %q", ErrBadBox, box)
	}
	if !validID(id) {
		return Full{}, fmt.Errorf("%w: %q", ErrBadID, id)
	}
	raw, err := scanBox(root, box)
	if err != nil {
		return Full{}, err
	}
	var found *rawMsg
	for i := range raw {
		if raw[i].id == id {
			found = &raw[i]
			break
		}
	}
	if found == nil {
		return Full{}, fmt.Errorf("%w: %s", ErrNoMessage, id)
	}

	// Headers come from the bounded probe — the runaway rule (a header
	// block past the cap is not a message) lives there, so the by-id
	// surfaces and the listings agree on what is addressable. The header
	// prefix read is bounded independently so even a cap-busting file
	// gets its headers.
	msg, err := parseHeaders(found.path, *found)
	if err != nil {
		if errors.Is(err, errHeaderRunaway) {
			return Full{}, fmt.Errorf("%w: %s (header block exceeds the size cap)", ErrNoMessage, id)
		}
		return Full{}, err
	}
	if found.info.Size() > MaxMessageBytes {
		return Full{Message: msg, Truncated: true}, nil
	}

	rawBytes, err := os.ReadFile(found.path)
	if err != nil {
		return Full{}, err
	}
	m, err := mail.ReadMessage(strings.NewReader(string(rawBytes)))
	if err != nil {
		return Full{}, err
	}
	full := Full{Message: msg}
	full.Body, full.WasHTML, err = extractBody(m)
	if err != nil {
		return Full{}, err
	}
	return full, nil
}

// ---------- body extraction ----------

// ExtractBody pulls the text out of one raw RFC822 message — the entry
// the IMAP account path shares with Open, so a letter's body reads
// identically no matter which transport carried it: text/plain verbatim,
// text/html stripped, multipart walked with plain preferred. wasHTML
// marks a body recovered from an HTML part.
func ExtractBody(raw []byte) (body string, wasHTML bool, err error) {
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	return extractBody(m)
}

// extractBody pulls the text out of a parsed message: text/plain
// verbatim, text/html stripped to text, multipart walked (depth-capped)
// with text/plain preferred over text/html. Other content types
// (attachments, application/*) contribute nothing — the body is empty,
// which the surfaces render as "(no text body)". Nothing is ever
// fetched: the only inputs are the bytes already on disk.
func extractBody(m *mail.Message) (string, bool, error) {
	return extractBodyFrom(mail.Header(m.Header), m.Body, 0)
}

func extractBodyFrom(hdr mail.Header, body io.Reader, depth int) (string, bool, error) {
	if depth > 5 {
		return "", false, nil // pathological nesting: tolerated, empty
	}
	mediatype := "text/plain" // absent Content-Type is plain text per RFC 822
	ct := hdr.Get("Content-Type")
	if strings.TrimSpace(ct) != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil {
			return "", false, nil // unparseable type: no text we can trust
		}
		mediatype = strings.ToLower(mt)
	}

	if strings.HasPrefix(mediatype, "multipart/") {
		_, params, err := mime.ParseMediaType(ct)
		if err != nil || params["boundary"] == "" {
			return "", false, nil
		}
		mr := multipart.NewReader(body, params["boundary"])
		var htmlFallback string
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", false, nil // malformed boundary: no text we can trust
			}
			if txt, wasHTML, err := extractBodyFrom(mail.Header(p.Header), p, depth+1); err == nil {
				if !wasHTML && txt != "" {
					return txt, false, nil // first text/plain wins
				}
				if wasHTML && htmlFallback == "" {
					htmlFallback = txt
				}
			}
		}
		return htmlFallback, htmlFallback != "", nil
	}

	decoded, err := decodeCTE(body, strings.ToLower(strings.TrimSpace(hdr.Get("Content-Transfer-Encoding"))))
	if err != nil {
		return "", false, nil // a body that will not decode contributes nothing
	}
	switch {
	case mediatype == "text/plain":
		return normalizeText(decoded), false, nil
	case mediatype == "text/html":
		return htmlToText(decoded), true, nil
	default:
		return "", false, nil
	}
}

// decodeCTE undoes the Content-Transfer-Encoding. quoted-printable and
// base64 are the ones real mail uses; 7bit/8bit/binary pass through, and
// an unknown encoding passes through too — better honest bytes than a
// dropped body (documented tolerance). base64.NewDecoder ignores the \r
// and \n line wrapping mail bodies interleave, so no preprocessing.
func decodeCTE(r io.Reader, enc string) (string, error) {
	switch enc {
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(r))
		return string(b), err
	case "base64":
		b, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, r))
		return string(b), err
	default: // 7bit, 8bit, binary, empty, unknown
		b, err := io.ReadAll(r)
		return string(b), err
	}
}

// htmlToText strips an HTML document down to readable text — the v1
// privacy ruling in code: script and style CONTENTS are dropped (not just
// their tags), every other tag is removed (block-level tags become line
// breaks), entities are decoded, and whitespace is normalized. There is
// no renderer here and never will be: no remote content, no inline
// styles, no links followed.
func htmlToText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '<' {
			b.WriteByte(s[i])
			i++
			continue
		}
		// Find the tag's end, honoring quoted attribute values so
		// <a title="a>b"> does not end the tag early.
		j := i + 1
		inQuote := byte(0)
		for j < len(s) {
			c := s[j]
			if inQuote != 0 {
				if c == inQuote {
					inQuote = 0
				}
			} else if c == '"' || c == '\'' {
				inQuote = c
			} else if c == '>' {
				break
			}
			j++
		}
		if j >= len(s) { // unterminated tag at EOF: drop the rest
			break
		}
		tag := strings.ToLower(strings.TrimSpace(s[i+1 : j]))
		tag = strings.TrimRight(tag, "/ \t\n")
		name := tag
		if sp := strings.IndexAny(tag, " \t\n"); sp >= 0 {
			name = tag[:sp]
		}
		switch name {
		case "script", "style":
			// Drop the element INCLUDING its content: everything until
			// the matching close tag is code, not text.
			closeTag := "</" + name
			tail := strings.ToLower(s[j+1:])
			if k := strings.Index(tail, closeTag); k >= 0 {
				b.WriteString("\n") // the block still occupied vertical space
				i = j + 1 + k + len(closeTag)
				if e := strings.IndexByte(s[i:], '>'); e >= 0 {
					i += e + 1
					continue
				}
			}
			i = len(s) // no close tag: everything after is script
		case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6",
			"blockquote", "ul", "ol", "table", "section", "article", "header",
			"footer", "hr", "pre", "dl", "dt", "dd":
			b.WriteString("\n")
			i = j + 1
		default:
			i = j + 1
		}
	}
	out := html.UnescapeString(b.String())
	// Collapse the blank-line runs the tag-to-newline swaps create; keep
	// single newlines and at most one blank line between paragraphs.
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	blank := false
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			blank = true
			continue
		}
		if blank && len(kept) > 0 {
			kept = append(kept, "")
		}
		blank = false
		kept = append(kept, strings.TrimRight(ln, " \t"))
	}
	return strings.Join(kept, "\n")
}

// normalizeText canonicalizes line endings for a text/plain body (CRLF
// and lone CR become LF); the bytes are otherwise verbatim.
func normalizeText(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}
