package util

import (
	"regexp"
	"strconv"
)

// grepLineRe matches a `grep -n` output line: "path:line:content".
// The path is greedy so Windows drive letters survive — a naive
// SplitN(line, ":", 3) takes "C" as the path for "C:\path\file.go:5:content".
var grepLineRe = regexp.MustCompile(`^(.*):(\d+):(.*)$`)

// SplitGrepLine parses one `grep -n` output line. ok is false when the line
// does not match the "path:line:content" shape (e.g. blank lines).
func SplitGrepLine(line string) (path string, lineNum int, content string, ok bool) {
	m := grepLineRe.FindStringSubmatch(line)
	if m == nil {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, "", false
	}
	return m[1], n, m[3], true
}

// stackFrameRe matches a stack frame "path:line" where path may be a Windows
// absolute path (C:\...\file.go) or a UNC path (\\server\share\file.go).
// A naive [^\s:]+ stops at the first colon and captures only the drive
// letter on Windows.
var stackFrameRe = regexp.MustCompile(`([A-Za-z]:\\[^\s]+|[^\s:]+):(\d+)`)

// SplitStackFrame parses a "path:line" frame. ok is false on no match.
func SplitStackFrame(s string) (path string, lineNum int, ok bool) {
	m := stackFrameRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0, false
	}
	n, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0, false
	}
	return m[1], n, true
}
