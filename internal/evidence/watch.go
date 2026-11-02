package evidence

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Watched checks: exit-code, http-probe, port-probe, and screenshot
// observations for check ids the model has declared in a contract. The
// ledger is deliberately quiet by default — an arbitrary bash command
// records no exit evidence, so the verdict gate's "a check ran" signal
// stays meaningful. Declaring `exit:0:make lint` in a contract WATCHES that
// command; from then on every completed run of exactly `make lint`
// records pass/fail for the declared expectation. The same applies to
// `http:<status>:<url>` (observed from curl status lines),
// `port:<host>:<port>` (observed from nc -z probes), and
// `screenshot:<name>` (observed from named browser captures).
//
// Config-defined checks are the permanent variant: config.yaml names a
// command plus a success condition, and every completed run of that exact
// command records `check:<name>`. They need no contract declaration.

// watchKind discriminates how a watched check is observed.
type watchKind int

const (
	watchExit watchKind = iota
	watchHTTP
	watchPort
	watchScreenshot
)

// watch is one declared expectation awaiting observations.
type watch struct {
	kind   watchKind
	key    string // exit: the command verbatim; http: normalized url; port: host:port
	expect int    // exit code / http status; unused for port
}

// CheckDef is one config-defined check (config.yaml `checks:`). The check
// id in the ledger is "check:<name>".
type CheckDef struct {
	// Name is the check id suffix; letters, digits, dashes, underscores.
	Name string
	// Command is the exact shell command whose completed runs produce
	// evidence for this check (whitespace-trimmed comparison).
	Command string
	// Exit is the expected exit code; 0 unless configured otherwise.
	Exit int
	// Pattern is an optional success regex over the combined output; a
	// matching run passes only when the regex also matches.
	Pattern string
}

type compiledCheck struct {
	def CheckDef
	re  *regexp.Regexp
}

// Watch registers a declared check id for observation. It returns true
// when the id is a watchable form (exit:/http:/port:); re-watching an
// already-watched id is a no-op. Suite ids (go-build, js-test, …) return
// false — they are observed through classification, not watches.
func (l *Ledger) Watch(id string) bool {
	if l == nil {
		return false
	}
	w, ok := parseWatch(id)
	if !ok {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.watches == nil {
		l.watches = make(map[string]watch)
	}
	if _, exists := l.watches[id]; exists {
		return true
	}
	l.watches[id] = w
	return true
}

// Watched reports the check ids currently watched (sorted).
func (l *Ledger) Watched() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.watches))
	for id := range l.watches {
		out = append(out, id)
	}
	sortStrings(out)
	return out
}

// SetChecks installs config-defined checks, replacing any previous set.
// A bad success pattern fails the whole call — a typo'd check must be
// loud at startup, not silently never-green.
func (l *Ledger) SetChecks(defs []CheckDef) error {
	compiled := make([]compiledCheck, 0, len(defs))
	for _, d := range defs {
		if d.Name == "" || d.Command == "" {
			continue
		}
		cc := compiledCheck{def: d}
		if d.Pattern != "" {
			re, err := regexp.Compile(d.Pattern)
			if err != nil {
				return err
			}
			cc.re = re
		}
		compiled = append(compiled, cc)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.checks = compiled
	return nil
}

// CheckDefs returns the installed config-defined check definitions.
func (l *Ledger) CheckDefs() []CheckDef {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]CheckDef, 0, len(l.checks))
	for _, cc := range l.checks {
		out = append(out, cc.def)
	}
	return out
}

// parseWatch converts a declared check id into a watch. Grammar:
//
//	exit:<n>:<command>   command exits n (single-segment commands only)
//	http:<code>:<url>    url answers with status code (curl probes)
//	port:<host>:<port>   tcp port is open (nc -z probes)
//	screenshot:<name>    named browser capture matches its baseline
func parseWatch(id string) (watch, bool) {
	var w watch
	switch {
	case strings.HasPrefix(id, "exit:"):
		rest := strings.TrimPrefix(id, "exit:")
		colon := strings.IndexByte(rest, ':')
		if colon <= 0 {
			return w, false
		}
		n, err := strconv.Atoi(rest[:colon])
		if err != nil || n < 0 || rest[colon+1:] == "" {
			return w, false
		}
		return watch{kind: watchExit, key: rest[colon+1:], expect: n}, true
	case strings.HasPrefix(id, "http:"):
		rest := strings.TrimPrefix(id, "http:")
		colon := strings.IndexByte(rest, ':')
		if colon <= 0 {
			return w, false
		}
		code, err := strconv.Atoi(rest[:colon])
		if err != nil || code < 100 || code > 599 {
			return w, false
		}
		key := strings.TrimSuffix(rest[colon+1:], "/")
		if key == "" {
			return w, false
		}
		return watch{kind: watchHTTP, key: key, expect: code}, true
	case strings.HasPrefix(id, "port:"):
		rest := strings.TrimPrefix(id, "port:")
		colon := strings.LastIndexByte(rest, ':')
		if colon <= 0 {
			return w, false
		}
		port, err := strconv.Atoi(rest[colon+1:])
		if err != nil || port < 1 || port > 65535 || rest[:colon] == "" {
			return w, false
		}
		return watch{kind: watchPort, key: rest, expect: 0}, true
	case strings.HasPrefix(id, "screenshot:"):
		name := strings.TrimPrefix(id, "screenshot:")
		if !validScreenshotName(name) {
			return w, false
		}
		return watch{kind: watchScreenshot, key: name, expect: 0}, true
	}
	return w, false
}

// ObserveExit is Observe with the process exit code threaded through
// (exitCode < 0 = unknown; failed still carries the signal). The exit
// code lets declared exit:<n> watches and config-defined checks judge
// outcomes the output text cannot express.
func (l *Ledger) ObserveExit(command, output string, exitCode int, failed bool, at time.Time) Verdict {
	if l == nil {
		return Verdict{}
	}
	records := classify(command, output, failed)
	records = append(records, l.watchRecords(command, output, exitCode, failed)...)
	return l.commitRecords(command, records, at)
}

// watchRecords derives records from declared watches and config-defined
// checks for one completed command. Caller must NOT hold l.mu (commitRecords
// takes it); this method takes and releases it once.
func (l *Ledger) watchRecords(command, output string, exitCode int, failed bool) []Record {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return nil
	}
	l.mu.Lock()
	watches := make([]watch, 0, len(l.watches))
	for _, w := range l.watches {
		watches = append(watches, w)
	}
	checks := append([]compiledCheck(nil), l.checks...)
	l.mu.Unlock()

	var records []Record
	for _, w := range watches {
		switch w.kind {
		case watchExit:
			if cmd != w.key {
				continue
			}
			switch {
			case exitCode >= 0:
				if exitCode == w.expect {
					records = append(records, Record{Check: watchID(w), Status: Pass})
				} else {
					records = append(records, Record{Check: watchID(w), Status: Fail, Detail: "exit " + itoa(exitCode)})
				}
			case w.expect == 0:
				// Unknown code, known failed-ness: a non-zero exit is
				// definitely not 0.
				if failed {
					records = append(records, Record{Check: watchID(w), Status: Fail, Detail: "non-zero exit"})
				} else {
					records = append(records, Record{Check: watchID(w), Status: Pass})
				}
			default:
				// Unknown code cannot judge a non-zero expectation.
			}
		case watchHTTP:
			u, ok := curlURL(cmd)
			if !ok || u != w.key {
				continue
			}
			status, ok := httpStatus(output, cmd)
			if !ok {
				continue
			}
			if status == w.expect {
				records = append(records, Record{Check: watchID(w), Status: Pass})
			} else {
				records = append(records, Record{Check: watchID(w), Status: Fail, Detail: "got " + itoa(status)})
			}
		case watchPort:
			host, port, ok := ncTarget(cmd)
			if !ok || host+":"+port != w.key {
				continue
			}
			if !failed {
				records = append(records, Record{Check: watchID(w), Status: Pass})
			} else {
				records = append(records, Record{Check: watchID(w), Status: Fail, Detail: "connection refused"})
			}
		}
	}
	for _, cc := range checks {
		if cmd != strings.TrimSpace(cc.def.Command) {
			continue
		}
		id := "check:" + cc.def.Name
		pass := false
		detail := ""
		switch {
		case exitCode >= 0:
			pass = exitCode == cc.def.Exit
			if !pass {
				detail = "exit " + itoa(exitCode) + ", want " + itoa(cc.def.Exit)
			}
		default:
			pass = !failed
			if !pass {
				detail = "non-zero exit"
			}
		}
		if pass && cc.re != nil && !cc.re.MatchString(output) {
			pass = false
			detail = "output did not match success pattern"
		}
		records = append(records, Record{Check: id, Status: statusOf(pass), Detail: detail})
	}
	return records
}

func statusOf(pass bool) Status {
	if pass {
		return Pass
	}
	return Fail
}

func watchID(w watch) string {
	switch w.kind {
	case watchExit:
		return "exit:" + itoa(w.expect) + ":" + w.key
	case watchHTTP:
		return "http:" + itoa(w.expect) + ":" + w.key
	case watchPort:
		return "port:" + w.key
	case watchScreenshot:
		return "screenshot:" + w.key
	}
	return ""
}

// ---------- curl / nc observation extraction ----------

// httpStatusLineRe matches curl -I/-i response status lines:
//
//	HTTP/1.1 200 OK
//	HTTP/2 404
var httpStatusLineRe = regexp.MustCompile(`(?m)^HTTP/[\d.]+[ \t]+(\d{3})`)

// httpCodeWriteRe matches a bare 3-digit status from `-w '%{http_code}'`.
var httpCodeWriteRe = regexp.MustCompile(`(?m)^[ \t]*(\d{3})[ \t]*$`)

// curlURL extracts the probed URL from a curl command, normalized the same
// way parseWatch's `http:` ids are written: scheme stripped, exactly one
// trailing slash dropped (root or deep), query kept. Only tokens containing
// "://" are considered, so quoted header values cannot masquerade as the
// target.
func curlURL(command string) (string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "curl" {
		return "", false
	}
	for _, f := range fields[1:] {
		if !strings.Contains(f, "://") {
			continue
		}
		u, err := url.Parse(f)
		if err != nil || u.Host == "" {
			continue
		}
		norm := u.Host + u.Path
		norm = strings.TrimSuffix(norm, "/")
		if u.RawQuery != "" {
			norm += "?" + u.RawQuery
		}
		if len(norm) > 200 {
			continue
		}
		return norm, true
	}
	return "", false
}

// httpStatus reads the observed status code from curl output: a response
// status line when present, else the bare code printed by -w %{http_code}.
func httpStatus(output, command string) (int, bool) {
	if m := httpStatusLineRe.FindStringSubmatch(output); m != nil {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code, true
		}
	}
	if strings.Contains(command, "%{http_code}") {
		if m := httpCodeWriteRe.FindStringSubmatch(output); m != nil {
			if code, err := strconv.Atoi(m[1]); err == nil {
				return code, true
			}
		}
	}
	return 0, false
}

// ncTarget extracts host and port from an `nc -z` probe:
//
//	nc -z localhost 5432
//	nc -z -w 2 db.internal 5432
func ncTarget(command string) (host, port string, ok bool) {
	fields := strings.Fields(command)
	if len(fields) < 4 || fields[0] != "nc" {
		return "", "", false
	}
	hasZ := false
	for _, f := range fields[1:] {
		if f == "-z" || f == "-zv" || f == "-vz" {
			hasZ = true
		}
	}
	if !hasZ {
		return "", "", false
	}
	host, port = fields[len(fields)-2], fields[len(fields)-1]
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", false
	}
	if strings.HasPrefix(host, "-") {
		return "", "", false
	}
	return host, port, true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
