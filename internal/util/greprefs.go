package util

import (
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// skipDirs are directory basenames that should never be walked when scanning
// for source references. They're vendored, hidden, or generated.
var refSkipDirs = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	"node_modules": {},
	"vendor":       {},
	".venv":        {},
	"venv":         {},
	"__pycache__":  {},
	".tox":         {},
	"dist":         {},
	"build":        {},
	"target":       {},
}

// CountGoReferences counts lines under root that match symbol as a
// whole-word Go identifier. The defining file (defFile) is skipped, as are
// *_test.go files — neither counts toward "real" usage. The match is anchored
// to Go identifier boundaries so `Foo` does not match `FooBar` or `myFoo`.
// Comments do not count as references: a `// Foo does things` line means the
// API was talked about, not used, and counting it made apitool report
// commented-out APIs as alive. String literals still count — this is a
// line-oriented usage signal, not a type-checked one.
//
// root is the directory to walk, defFile is the absolute path of the file
// that defines symbol (so its self-references don't inflate the count), and
// symbol is the exported name to search for. The function is Go-native — no
// shell-out — so it works on Windows and any system without a `grep` in PATH.
//
// The returned count is the number of lines that contain at least one
// occurrence of symbol. Multi-occurrence lines count once, matching the
// behavior of `grep -r -c` semantics.
func CountGoReferences(root, defFile, symbol string) (int, error) {
	if symbol == "" {
		return 0, nil
	}
	// Anchor with Go identifier boundaries: (?i) is omitted — Go exported
	// names are case-sensitive, and counting case-insensitively would
	// match the receiver's lowercase helper too.
	re, err := regexp.Compile(`\b` + regexp.QuoteMeta(symbol) + `\b`)
	if err != nil {
		return 0, nil
	}

	defAbs, _ := filepath.Abs(defFile)
	count := 0
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if _, skip := refSkipDirs[d.Name()]; skip {
				return fs.SkipDir
			}
			return nil
		}
		// Only Go files. Other source types may have their own reference
		// graph but this helper exists for the apitool/deadcode Go usage
		// signal, which is Go-only.
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		absPath, _ := filepath.Abs(path)
		if absPath == defAbs {
			return nil
		}
		// Tests don't count as "real" usage for this signal.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		return scanFileForSymbol(path, re, &count)
	})
	return count, walkErr
}

// scanFileForSymbol reads path, blanks out Go comments, and bumps count once
// per line that matches re. Bumping per line (rather than per match) matches
// `grep -r -c` semantics and keeps the number stable when a symbol appears
// multiple times in a single line.
func scanFileForSymbol(path string, re *regexp.Regexp, count *int) error {
	b, err := os.ReadFile(path)
	if err != nil {
		// Files may be unreadable for many reasons (permissions,
		// disappeared mid-walk). Skip silently — the caller still gets
		// a count from the rest of the tree.
		return nil
	}
	for _, line := range strings.Split(string(stripComments(b)), "\n") {
		if re.MatchString(line) {
			*count++
		}
	}
	return nil
}

// stripComments blanks Go comments in src with spaces so the per-line match
// cannot hit symbols that only appear in comments. go/scanner supplies the
// token stream, which means string and raw-string literals are honored — a
// "//" inside a string is data, not a comment opener. Newlines inside block
// comments are preserved so line boundaries (and thus per-line counting) stay
// identical to the raw source. If the scanner reports any error (e.g. invalid
// UTF-8), the original bytes are returned unchanged so matching degrades to
// the historic raw scan instead of skipping the file.
func stripComments(src []byte) []byte {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	sawErr := false
	s.Init(file, src, func(token.Position, string) { sawErr = true }, scanner.ScanComments)

	out := append([]byte(nil), src...)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok != token.COMMENT {
			continue
		}
		// The comment literal spans [offset, offset+len(lit)); overwrite its
		// bytes with spaces, keeping newlines so line numbers survive.
		for i := file.Offset(pos); i < file.Offset(pos)+len(lit); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}
	if sawErr {
		return src
	}
	return out
}
