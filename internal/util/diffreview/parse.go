package diffreview

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Document is a parsed unified diff, optionally with a git-show preamble.
type Document struct {
	Preamble []string
	Files    []File
	Metadata Metadata
}

// Metadata describes the source commit when the input came from git show.
type Metadata struct {
	SourceKind string
	CommitID   string
}

// File is one diff --git section.
type File struct {
	Preamble []string
	Header   []string
	OldName  string
	NewName  string
	Hunks    []Hunk
	Metadata Metadata
}

// Hunk is one @@ range plus its lines.
type Hunk struct {
	Header   string
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []Line
}

// Line is one hunk body line.
type Line struct {
	Kind    LineKind
	OldLine int
	NewLine int
	Text    string
}

// LineKind classifies a hunk body line.
type LineKind int

const (
	Context LineKind = iota
	Add
	Delete
	NoNewline
)

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse reads a unified diff (optionally with git-show commit headers).
func Parse(input string) (Document, error) {
	var doc Document
	var currentFile *File
	var currentHunk *Hunk
	var pendingPreamble []string
	var pendingMetadata Metadata
	oldLine := 0
	newLine := 0

	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Buffer(make([]byte, 1024), 1024*1024*8)

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if isCommitHeader(line) && currentFile != nil {
			currentFile = nil
			currentHunk = nil
		}
		if currentFile == nil {
			if metadata, ok := metadataFromCommitHeader(line); ok {
				if doc.Metadata.CommitID == "" {
					doc.Metadata = metadata
				}
				pendingMetadata = metadata
			}
		}

		if strings.HasPrefix(line, "diff --git ") {
			doc.Files = append(doc.Files, File{
				Preamble: pendingPreamble,
				Header:   []string{line},
				Metadata: pendingMetadata,
			})
			pendingPreamble = nil
			currentFile = &doc.Files[len(doc.Files)-1]
			currentHunk = nil
			continue
		}

		if currentFile == nil {
			if len(doc.Files) == 0 {
				doc.Preamble = append(doc.Preamble, line)
			} else {
				pendingPreamble = append(pendingPreamble, line)
			}
			continue
		}

		if hunkHeader.MatchString(line) {
			hunk, err := parseHunkHeader(line)
			if err != nil {
				return Document{}, err
			}
			currentFile.Hunks = append(currentFile.Hunks, hunk)
			currentHunk = &currentFile.Hunks[len(currentFile.Hunks)-1]
			oldLine = currentHunk.OldStart
			newLine = currentHunk.NewStart
			continue
		}

		if currentHunk != nil && hunkComplete(*currentHunk, oldLine, newLine) && !strings.HasPrefix(line, `\`) {
			currentHunk = nil
		}

		if currentHunk == nil {
			currentFile.Header = append(currentFile.Header, line)
			if rest, ok := strings.CutPrefix(line, "--- "); ok {
				currentFile.OldName = rest
			}
			if rest, ok := strings.CutPrefix(line, "+++ "); ok {
				currentFile.NewName = rest
			}
			continue
		}

		switch {
		case strings.HasPrefix(line, "+"):
			currentHunk.Lines = append(currentHunk.Lines, Line{
				Kind:    Add,
				NewLine: newLine,
				Text:    line,
			})
			newLine++
		case strings.HasPrefix(line, "-"):
			currentHunk.Lines = append(currentHunk.Lines, Line{
				Kind:    Delete,
				OldLine: oldLine,
				Text:    line,
			})
			oldLine++
		case strings.HasPrefix(line, `\`):
			currentHunk.Lines = append(currentHunk.Lines, Line{
				Kind: NoNewline,
				Text: line,
			})
		default:
			currentHunk.Lines = append(currentHunk.Lines, Line{
				Kind:    Context,
				OldLine: oldLine,
				NewLine: newLine,
				Text:    line,
			})
			oldLine++
			newLine++
		}
	}

	if err := scanner.Err(); err != nil {
		return Document{}, err
	}

	return doc, nil
}

func metadataFromCommitHeader(line string) (Metadata, bool) {
	if isCommitHeader(line) {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return Metadata{SourceKind: "show", CommitID: fields[1]}, true
		}
	}
	return Metadata{}, false
}

func isCommitHeader(line string) bool {
	if !strings.HasPrefix(line, "commit ") {
		return false
	}
	fields := strings.Fields(line)
	return len(fields) >= 2
}

func parseHunkHeader(line string) (Hunk, error) {
	matches := hunkHeader.FindStringSubmatch(line)
	if matches == nil {
		return Hunk{}, fmt.Errorf("invalid hunk header: %q", line)
	}

	oldStart, err := strconv.Atoi(matches[1])
	if err != nil {
		return Hunk{}, err
	}
	oldCount, err := parseCount(matches[2])
	if err != nil {
		return Hunk{}, err
	}
	newStart, err := strconv.Atoi(matches[3])
	if err != nil {
		return Hunk{}, err
	}
	newCount, err := parseCount(matches[4])
	if err != nil {
		return Hunk{}, err
	}

	return Hunk{
		Header:   line,
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
	}, nil
}

func parseCount(value string) (int, error) {
	if value == "" {
		return 1, nil
	}
	return strconv.Atoi(value)
}

func hunkComplete(hunk Hunk, oldLine, newLine int) bool {
	return oldLine >= hunk.OldStart+hunk.OldCount && newLine >= hunk.NewStart+hunk.NewCount
}

func rowKind(kind LineKind) RowKind {
	switch kind {
	case Add:
		return RowAdd
	case Delete:
		return RowDelete
	case NoNewline:
		return RowNoNewline
	default:
		return RowContext
	}
}

func fileName(file File) string {
	oldName := displayFileName(file.OldName)
	newName := displayFileName(file.NewName)
	switch {
	case oldName != "" && newName != "" && oldName != newName && oldName != "/dev/null" && newName != "/dev/null":
		return oldName + " -> " + newName
	case newName != "" && newName != "/dev/null":
		return newName
	case oldName != "" && oldName != "/dev/null":
		return oldName
	case len(file.Header) > 0:
		return file.Header[0]
	default:
		return "(unknown file)"
	}
}

func displayFileName(name string) string {
	if name == "" || name == "/dev/null" {
		return name
	}
	return stripDiffPathPrefix(name)
}

func syntaxFileName(file File) string {
	switch {
	case file.NewName != "" && file.NewName != "/dev/null":
		return stripDiffPathPrefix(file.NewName)
	case file.OldName != "" && file.OldName != "/dev/null":
		return stripDiffPathPrefix(file.OldName)
	default:
		return fileName(file)
	}
}

func stripDiffPathPrefix(name string) string {
	switch {
	case strings.HasPrefix(name, "a/"), strings.HasPrefix(name, "b/"):
		return name[2:]
	default:
		return name
	}
}
