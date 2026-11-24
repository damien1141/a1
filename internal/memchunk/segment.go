// Package memchunk splits a finished turn into the individual statements worth
// judging for long-term memory.
//
// The problem it solves: the memory outlet used to hand a whole turn to one
// summarizer, so a message pairing "I prefer tabs" with a 200-line stack trace
// was judged once, as a blob. Junk suppression was all-or-nothing at message
// granularity, and a real preference died whenever it shared a turn with code.
//
// Segmentation is deliberately mechanical. It decides where statements END, not
// whether they MATTER — that judgment belongs to the decision model
// (extract.go). Keeping the split deterministic means the expensive judgment
// stage runs against clean, bounded, individually addressable text.
package memchunk

import (
	"regexp"
	"strings"
)

// Kind classifies a chunk by the surface it came from. Extraction treats kinds
// differently: code and commands are dropped before any model call (they are
// never memories and cost tokens to ask about), while prose and reasoning are
// judged.
type Kind string

const (
	// KindProse is ordinary natural-language text from the user or assistant.
	KindProse Kind = "prose"
	// KindReasoning is prose lifted from the assistant's thinking block. It
	// carries real signal — "note that they prefer X" lives here — but it is
	// also more speculative than the final answer, so callers may weight it.
	KindReasoning Kind = "reasoning"
	// KindCode is a fenced code block, inline code, or an indented block that
	// looks like code. Never stored.
	KindCode Kind = "code"
	// KindCommand is a shell command or its output. Never stored.
	KindCommand Kind = "command"
	// KindURL is a bare URL or a link list. Never stored on its own; the prose
	// around it may be.
	KindURL Kind = "url"
	// KindLog is a log or traceback block: timestamped lines, level keywords,
	// or a stack trace. Never stored.
	KindLog Kind = "log"
)

// Chunk is one candidate statement.
type Chunk struct {
	// Text is the cleaned statement. Empty chunks are never emitted.
	Text string
	// Kind is the surface it came from.
	Kind Kind
	// Index is its position within the source it was segmented from. Stable
	// within one Segment call, and used verbatim as the jev question name, so
	// answers join back without positional guessing.
	Index int
}

// storable reports whether a kind is worth paying a model call to judge.
func (k Kind) storable() bool {
	return k == KindProse || k == KindReasoning
}

// Limits bound the work one turn can create. A pasted log must not become four
// hundred questions: the cost is linear in chunks, and an unbounded split is an
// unbounded bill.
const (
	// MaxChunks caps how many storable chunks one Segment call returns.
	MaxChunks = 50
	// MaxChunkChars caps one chunk's length. Longer statements are truncated on
	// a word boundary — a 4 KB paragraph is not more memorable than its first
	// sentence, and the model's context is finite.
	MaxChunkChars = 400
	// MinChunkChars merges anything shorter into its neighbour. "Yes." and "ok"
	// are not memories, and a one-word question wastes a question slot.
	MinChunkChars = 40
)

// fencedBlock matches a triple-backtick fence, with or without a language tag.
// It is line-anchored so inline backticks in prose are not mistaken for a fence.
var fencedBlock = regexp.MustCompile("(?ms)^```[^\\n]*\\n.*?^```[ \\t]*$")

// indentedBlock matches a run of lines indented four spaces or more — the
// shape a pasted stack trace or an aligned table takes.
var indentedBlock = regexp.MustCompile(`(?m)^(?: {4}|\t)\S.*(?:\n(?: {4}|\t)\S.*)*`)

// shellPrompt matches a line that is obviously a command, with or without its
// output. Deliberately narrow: a prose sentence beginning with a capital is not
// a command, and misclassifying prose here would silently discard memories.
// shellVerbs are the leading tokens that can open a command.
var shellVerbs = []string{
	"sudo", "cd", "git", "npm", "bun", "cargo", "docker", "kubectl",
	"curl", "wget", "rm", "mv", "cp", "go", "make", "pip", "python3", "python",
}

// shellPrompt matches a WHOLE command line, and is applied per line rather than
// over the blob, so a match can never consume a partial span and leave a tail
// behind.
//
// The shape is: verb + command-shaped argument. The argument requirement is what
// keeps ordinary prose safe — "go is a language I use daily" has no path, flag,
// or subcommand after "go", so it never matches. Getting this wrong in the
// direction of false positives silently DELETES a memory, which is the one
// failure mode worth being conservative about.
var shellPrompt = regexp.MustCompile(`^\s*(?:\$\s+|>\s*)?(?:` + strings.Join(shellVerbs, "|") +
	`)` +
	// The argument must be command-shaped: a path/package pattern, a flag, a
	// URL, or a subcommand word that is itself followed by a path or flag.
	`(?:\s+(?:[.~/]\S*|-{1,2}[\w-]+|https?://\S+|[a-z][\w-]*\s+[.~/-]\S*))` +
	`[^\n]*$`)

// looksLikeCommand reports whether an entire line is a command, judged line by
// line so nothing is lost to a partial match.
func looksLikeCommand(line string) bool {
	return shellPrompt.MatchString(line)
}

// testOutputLine matches a test-runner result line — the "--- FAIL:" / "--- PASS:"
// shape and the indented file:line assertion under it. These are never prose.
var testOutputLine = regexp.MustCompile(`(?m)^\s*(?:--- (?:FAIL|PASS|SKIP):|={3,} (?:FAIL|RUN)|ok\s+\S+\s+\d|FAIL\s+\S+\s+\d).*$`)

// timestampedLine matches a line that opens with a date or clock timestamp — the
// shape of every log line and of nothing a person says conversationally.
var timestampedLine = regexp.MustCompile(`(?m)^\s*(?:\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}|\d{2}:\d{2}:\d{2}|\[\d{2}:\d{2})`)

// levelKeyword matches a bare log level token, which prose almost never uses as
// a standalone word.
var levelKeyword = regexp.MustCompile(`(?m)(?:^|\s)(?:ERROR|WARN|WARNING|INFO|DEBUG|TRACE|FATAL|CRITICAL)(?:\s|:)`)

// lookLikeLog reports whether a run of text is machine output rather than
// speech. Two independent signals: two or more timestamped lines, or a level
// keyword on at least half the lines.
//
// The "two or more" floor matters — a single line like "at 10:22 I saw it" must
// stay prose, and one stray ERROR token in a sentence is not a log dump.
func lookLikeLog(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 1 {
		return false
	}
	stamps, levels, nonEmpty := 0, 0, 0
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		nonEmpty++
		if timestampedLine.MatchString(ln) {
			stamps++
		}
		if levelKeyword.MatchString(ln) {
			levels++
		}
	}
	if stamps >= 2 {
		return true
	}
	return nonEmpty > 0 && levels*2 >= nonEmpty && levels >= 2
}

// urlLine matches a line that is nothing but a URL (optionally bulleted).
var urlLine = regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?https?://\S+\s*$`)

// Segment splits text into candidate statements of the given kind.
//
// Order matters: fenced blocks are removed and classified FIRST, so a code
// block containing sentence-ending punctuation cannot shatter the prose split
// that follows. The same reasoning applies to indented blocks and command
// lines. What survives is prose, which is then split on sentence boundaries
// with abbreviation guards.
//
// Segment never fails: malformed input degrades to fewer chunks, never an
// error. The caller's job is to store the result, not to handle a splitter
// that refuses to split.
func Segment(text string, kind Kind) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if kind == "" {
		kind = KindProse
	}

	var out []Chunk
	// emit appends one chunk of the given kind, applying the size limits.
	emit := func(s string, k Kind) {
		s = cleanText(s)
		if s == "" {
			return
		}
		out = append(out, Chunk{Text: truncateWords(s, MaxChunkChars), Kind: k})
	}

	// Phase 1: quarantine the non-prose surfaces so they cannot influence the
	// prose split. Each match is replaced with a sentinel that survives the
	// split as its own paragraph, and the extracted text is emitted separately.
	var codeBlocks, commandBlocks, urlBlocks []string
	rest := fencedBlock.ReplaceAllStringFunc(text, func(m string) string {
		codeBlocks = append(codeBlocks, m)
		return "\n\n\x00BLOCK\x00\n\n"
	})
	rest = indentedBlock.ReplaceAllStringFunc(rest, func(m string) string {
		codeBlocks = append(codeBlocks, m)
		return "\n\n\x00BLOCK\x00\n\n"
	})
	// Commands and test output are classified LINE BY LINE. A whole-blob regex
	// matched a prefix and left the remainder (" -run Foo") to be re-formed as
	// prose; per-line classification has no partial-span failure mode.
	var proseLines []string
	for _, ln := range strings.Split(rest, "\n") {
		switch {
		case looksLikeCommand(ln), testOutputLine.MatchString(ln):
			commandBlocks = append(commandBlocks, ln)
		default:
			proseLines = append(proseLines, ln)
		}
	}
	rest = strings.Join(proseLines, "\n")
	rest = urlLine.ReplaceAllStringFunc(rest, func(m string) string {
		urlBlocks = append(urlBlocks, m)
		return "\n\n\x00BLOCK\x00\n\n"
	})
	// Log output survives the splitters above (a log line is neither fenced nor
	// indented nor a command), so it gets a whole-text check of its own. This is
	// the deterministic answer to "a log dump is never a memory": it is dropped
	// before the model is ever asked, which is both cheaper and more reliable
	// than hoping the model rejects it.
	logBlock := ""
	if lookLikeLog(rest) {
		logBlock = rest
		rest = ""
	}

	// Phase 2: split what is left into sentences, paragraph by paragraph.
	// Paragraph structure is meaningful — it marks a change of subject — so the
	// paragraph break is preserved as a hard boundary rather than a soft one.
	for _, para := range strings.Split(rest, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" || para == "\x00BLOCK\x00" {
			continue
		}
		if para == "\x00BLOCK\x00" {
			continue
		}
		for _, sentence := range splitSentences(para) {
			emit(sentence, kind)
		}
	}

	// Phase 3: the quarantined blocks become chunks of their own kind. They are
	// emitted after the prose so a caller reading the slice sees statements
	// before artifacts.
	for _, c := range codeBlocks {
		emit(c, KindCode)
	}
	for _, c := range commandBlocks {
		emit(c, KindCommand)
	}
	for _, c := range urlBlocks {
		emit(c, KindURL)
	}
	if logBlock != "" {
		emit(logBlock, KindLog)
	}

	// Phase 4: renumber over the STORABLE chunks only, so question names stay
	// dense and a code block never consumes an id. Storable chunks are also
	// capped here — the cap is on work, and code costs no model calls.
	idx := 0
	kept := out[:0]
	for _, c := range out {
		if !c.Kind.storable() {
			continue
		}
		if idx >= MaxChunks {
			break
		}
		c.Index = idx
		idx++
		kept = append(kept, c)
	}
	return mergeShort(kept)
}

// splitSentences breaks a paragraph on sentence-ending punctuation, guarding the
// abbreviations that would otherwise shatter a statement mid-phrase.
func splitSentences(para string) []string {
	// Protect known abbreviations with a sentinel that cannot appear in input.
	const dot = "\x00DOT\x00"
	protected := para
	for _, abbr := range []string{
		"e.g.", "i.e.", "etc.", "vs.", "cf.", "al.",
		"Dr.", "Mr.", "Mrs.", "Ms.", "Prof.", "St.",
		"Inc.", "Ltd.", "Co.", "No.", "Fig.", "approx.",
	} {
		protected = strings.ReplaceAll(protected, abbr, strings.ReplaceAll(abbr, ".", dot))
	}
	// A decimal number is not a sentence end either: "3.14" stays whole.
	protected = regexp.MustCompile(`(\d)\.(\d)`).ReplaceAllString(protected, "${1}"+dot+"${2}")

	var out []string
	var cur strings.Builder
	runes := []rune(protected)
	for i := 0; i < len(runes); i++ {
		cur.WriteRune(runes[i])
		if runes[i] != '.' && runes[i] != '!' && runes[i] != '?' {
			continue
		}
		// A boundary needs whitespace (or end of text) after the punctuation,
		// so ellipses and "3.14" stay put.
		if i+1 < len(runes) && !isSpaceRune(runes[i+1]) {
			continue
		}
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

func isSpaceRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// cleanText normalizes a statement for both the model and the embedder: markdown
// noise stripped, whitespace collapsed, speaker prefixes removed. It cleans; it
// never decides worth — that is the extraction stage's job.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\x00DOT\x00", ".")
	s = strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", // smart single quotes
		"\u201c", `"`, "\u201d", `"`, // smart double quotes
		"\u2013", "-", "\u2014", "-", // en/em dash
		"\u00a0", " ", // non-breaking space
	).Replace(s)
	s = strings.TrimSpace(s)
	// Speaker prefixes ("User:", "Assistant:") are conversational plumbing, not
	// content, and they are identical across every turn — noise to an embedder.
	s = speakerPrefix.ReplaceAllString(s, "")
	// Markdown emphasis and heading markers.
	s = markdownNoise.ReplaceAllString(s, "")
	// List bullets at the start of a line.
	s = listBullet.ReplaceAllString(s, "")
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimSpace(s)
}

var (
	speakerPrefix = regexp.MustCompile(`(?i)^\s*(?:user|assistant|system|human|ai)\s*:\s*`)
	markdownNoise = regexp.MustCompile("[`*_~#]+")
	listBullet    = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+\.)\s+`)
)

// mergeShort folds chunks shorter than MinChunkChars into the previous chunk,
// producing shorter statements with more context. A leading short chunk is
// merged forward instead, since there is nothing behind it to absorb it.
func mergeShort(chunks []Chunk) []Chunk {
	if len(chunks) == 0 {
		return nil
	}
	out := make([]Chunk, 0, len(chunks))
	for _, c := range chunks {
		if n := len(out); n > 0 && (len([]rune(c.Text)) < MinChunkChars || len([]rune(out[n-1].Text)) < MinChunkChars) {
			prev := &out[n-1]
			// Rejoin with a period so the merged statement reads as two
			// sentences rather than a run-on.
			prev.Text = strings.TrimSpace(prev.Text + " " + c.Text)
			continue
		}
		out = append(out, c)
	}
	// Renumber after merging: callers index by Chunk.Index.
	for i := range out {
		out[i].Index = i
	}
	return out
}

// truncateWords cuts s to at most max characters on a word boundary.
func truncateWords(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}
