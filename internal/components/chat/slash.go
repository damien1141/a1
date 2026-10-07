package chat

import "strings"

// ActiveSlash reports whether the cursor sits in a leading `/command` token.
// Only the first token of the composer value participates (slash commands are
// whole-line). query is the text after '/' up to the cursor.
// start/end are byte offsets to replace on accept (from '/' through cursor).
func ActiveSlash(value string, cursor int) (query string, start, end int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	if !strings.HasPrefix(value, "/") {
		return "", 0, 0, false
	}
	if cursor < 1 {
		return "", 0, 0, false
	}
	// First whitespace ends the command token; picker only while editing it.
	for i := 1; i < len(value); i++ {
		c := value[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			if cursor > i {
				return "", 0, 0, false
			}
			break
		}
	}
	return value[1:cursor], 0, cursor, true
}

// ActiveSlashArgs reports whether the cursor sits in a slash command's arg
// tokens (after the first whitespace). command is the slash command name (no
// leading '/'). query is the current unfinished arg token text. argStart/argEnd
// are byte offsets for the current token.
func ActiveSlashArgs(value string, cursor int) (command, query string, cmdStart, cmdEnd, argStart, argEnd int, ok bool) {
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(value) {
		cursor = len(value)
	}
	if !strings.HasPrefix(value, "/") {
		return "", "", 0, 0, 0, 0, false
	}
	if cursor < 2 {
		return "", "", 0, 0, 0, 0, false
	}
	// Find end of command token.
	cmdEnd = 1
	for cmdEnd < len(value) && value[cmdEnd] != ' ' && value[cmdEnd] != '\t' && value[cmdEnd] != '\n' && value[cmdEnd] != '\r' {
		cmdEnd++
	}
	if cmdEnd >= len(value) || (value[cmdEnd] != ' ' && value[cmdEnd] != '\t') {
		return "", "", 0, 0, 0, 0, false
	}
	if cursor < cmdEnd {
		// Cursor is inside the command token; caller should use command completion.
		return "", "", 0, 0, 0, 0, false
	}
	// Tokenize args.
	argTokens := strings.Fields(value[cmdEnd+1:])
	if len(argTokens) == 0 {
		return value[1:cmdEnd], "", 1, cmdEnd, len(value), len(value), true
	}
	// Walk to the arg token that contains or receives the cursor.
	pos := cmdEnd + 1
	for pos < len(value) && (value[pos] == ' ' || value[pos] == '\t') {
		pos++
	}
	for _, token := range argTokens {
		tokenStart := pos
		tokenEnd := tokenStart + len(token)
		if cursor < tokenStart {
			return value[1:cmdEnd], "", 1, cmdEnd, tokenStart, tokenStart, true
		}
		if cursor <= tokenEnd {
			qStart := tokenStart
			if cursor > tokenEnd {
				qStart = tokenEnd
			}
			return value[1:cmdEnd], value[qStart:cursor], 1, cmdEnd, tokenStart, tokenEnd, true
		}
		pos = tokenEnd
		for pos < len(value) && (value[pos] == ' ' || value[pos] == '\t') {
			pos++
		}
	}
	// Cursor is after the last token.
	return value[1:cmdEnd], "", 1, cmdEnd, pos, pos, true
}
