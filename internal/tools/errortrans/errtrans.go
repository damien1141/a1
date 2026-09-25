package errortrans

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damien1141/a1/internal/tools/tooldef"
	"github.com/damien1141/a1/internal/llm"
)

const (
	errtransDefaultLimit = 20
)

var errtransDescription = `Multi-language error pattern translator.

Maps language-specific compiler, runtime, and shell errors to a normalized
representation plus actionable fixes. Supports Rust, Python, Bash, Lua,
TypeScript, Go, and build-system errors.`

// ErrtransTool returns the error translator tool definition + handler.
func ErrtransTool() tooldef.Tool {
	return tooldef.Tool{
		Definition: llm.ToolDefinition{
			Name:        "errtrans",
			Description: errtransDescription,
			Params: &llm.FunctionParameters{
				Type: "object",
				Properties: llm.Object{
					"text": llm.Object{
						"type":        "string",
						"description": "Error message, compiler output, or stack trace to translate.",
					},
					"lang": llm.Object{
						"type":        "string",
						"description": "Optional language hint. Example: rust, python, bash, lua, ts, go",
					},
					"limit": llm.Object{
						"type":        "integer",
						"description": fmt.Sprintf("Maximum translations to return. Example: 10 (default: %d)", errtransDefaultLimit),
					},
				},
				Required: []string{"text"},
			},
			Readable: true,
		},
		DetailFromArgs: func(input json.RawMessage) string {
			var in errtransInput
			_ = json.Unmarshal(input, &in)
			t := strings.TrimSpace(in.Text)
			if len(t) > 40 {
				t = t[:40] + "..."
			}
			return fmt.Sprintf("errtrans %s", t)
		},
		Run: runErrtrans,
	}
}

type errtransInput struct {
	Text  string `json:"text"`
	Lang  string `json:"lang,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type errtransEntry struct {
	Language   string `json:"language"`
	Pattern    string `json:"pattern"`
	Normalized string `json:"normalized"`
	Fix        string `json:"fix"`
	Confidence string `json:"confidence"`
}

func runErrtrans(ctx context.Context, input json.RawMessage) (tooldef.Result, error) {
	var in errtransInput
	if err := json.Unmarshal(input, &in); err != nil {
		return tooldef.Result{}, fmt.Errorf("failed to parse errtrans arguments: %w", err)
	}

	text := strings.TrimSpace(in.Text)
	if text == "" {
		return tooldef.Result{}, fmt.Errorf("text is required")
	}

	lang := strings.ToLower(strings.TrimSpace(in.Lang))
	limit := in.Limit
	if limit <= 0 {
		limit = errtransDefaultLimit
	}

	entries, err := translateError(text, lang, limit)
	if err != nil {
		return tooldef.Result{}, err
	}

	if len(entries) == 0 {
		return tooldef.Result{Content: "No translations matched", Detail: "0 translations", Output: "No translations matched"}, nil
	}

	content := renderErrtransResults(ctx, entries)
	detail := fmt.Sprintf("%d translations", len(entries))
	return tooldef.Result{Content: content, Detail: detail, Output: content}, nil
}

func translateError(text, langHint string, limit int) ([]errtransEntry, error) {
	lang := detectLanguage(text, langHint)
	entries, err := matchPatterns(text, lang, limit)
	if err != nil {
		return nil, err
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func detectLanguage(text, langHint string) string {
	if langHint != "" {
		return langHint
	}
	textLower := strings.ToLower(text)
	switch {
	case strings.Contains(textLower, "cargo") || strings.Contains(textLower, "rustc") || strings.Contains(textLower, "borrow"):
		return "rust"
	case strings.Contains(textLower, "importerror") || strings.Contains(textLower, "modulenotfounderror") || strings.Contains(textLower, "python"):
		return "python"
	case strings.Contains(textLower, "bash:") || strings.Contains(textLower, "command not found") || strings.Contains(textLower, "syntax error"):
		return "bash"
	case strings.Contains(textLower, "lua") || strings.Contains(textLower, "attempt to index"):
		return "lua"
	case strings.Contains(textLower, "typescript") || strings.Contains(textLower, "ts ") || strings.Contains(textLower, "cannot find name"):
		return "ts"
	case strings.Contains(textLower, "go:") || strings.Contains(textLower, "undefined:") || strings.Contains(textLower, "build constraints"):
		return "go"
	case strings.Contains(textLower, "cmake") || strings.Contains(textLower, "make:") || strings.Contains(textLower, "ninja"):
		return "build"
	default:
		return "generic"
	}
}

func matchPatterns(text, lang string, limit int) ([]errtransEntry, error) {
	patterns := patternLibrary()
	var matches []errtransEntry
	for _, p := range patterns {
		if p.Language != lang && p.Language != "generic" && lang != "generic" {
			continue
		}
		if !strings.Contains(strings.ToLower(text), strings.ToLower(p.Substring)) {
			continue
		}
		matches = append(matches, errtransEntry{
			Language:   p.Language,
			Pattern:    p.Name,
			Normalized: p.Normalized,
			Fix:        p.Fix,
			Confidence: p.Confidence,
		})
	}
	return matches, nil
}

type pattern struct {
	Language   string
	Name       string
	Substring  string
	Normalized string
	Fix        string
	Confidence string
}

func patternLibrary() []pattern {
	return []pattern{
		{Language: "rust", Name: "borrowed data", Substring: "borrowed data", Normalized: "lifetime/borrow violation", Fix: "extend the lifetime with a scoped guard, clone the value, or restructure ownership so the borrow outlives the reference", Confidence: "high"},
		{Language: "rust", Name: "cannot move", Substring: "cannot move", Normalized: "move-semantics violation", Fix: "borrow instead of move, implement Clone, or wrap the value in Rc/Arc if shared ownership is required", Confidence: "high"},
		{Language: "rust", Name: "no field", Substring: "no field", Normalized: "struct-field access error", Fix: "check struct definition and field name casing; verify the field is public if accessed across modules", Confidence: "high"},
		{Language: "rust", Name: "unresolved name", Substring: "unresolved name", Normalized: "undefined symbol", Fix: "add a use declaration, bring the item into scope, or verify the crate feature is enabled", Confidence: "high"},
		{Language: "rust", Name: "mismatched types", Substring: "mismatched types", Normalized: "type mismatch", Fix: "convert with From/Into, parse explicitly, or align the function signature with the expected type", Confidence: "high"},
		{Language: "rust", Name: "borrow checker", Substring: "borrow checker", Normalized: "borrow/lifetime failure", Fix: "refactor into smaller scopes, use elided lifetimes where possible, or introduce owned intermediates", Confidence: "medium"},

		{Language: "python", Name: "importerror", Substring: "importerror", Normalized: "missing dependency or bad import path", Fix: "install the package, adjust PYTHONPATH, or fix relative import usage", Confidence: "high"},
		{Language: "python", Name: "modulenotfounderror", Substring: "modulenotfounderror", Normalized: "module not installed or wrong package name", Fix: "pip install the module or correct the module name and casing", Confidence: "high"},
		{Language: "python", Name: "indentationerror", Substring: "indentationerror", Normalized: "mixed indentation", Fix: "normalize spaces or tabs; 4-space indentation is standard", Confidence: "high"},
		{Language: "python", Name: "keyerror", Substring: "keyerror", Normalized: "dict key missing", Fix: "use .get() with a default, validate keys before access, or add the missing key", Confidence: "high"},
		{Language: "python", Name: "typeerror", Substring: "typeerror", Normalized: "wrong type passed to operation", Fix: "cast explicitly, inspect input types, or add input validation before the call", Confidence: "high"},
		{Language: "python", Name: "attributeerror", Substring: "attributeerror", Normalized: "missing attribute or method", Fix: "check object type, verify the attribute name, or add a guard for optional attributes", Confidence: "high"},
		{Language: "python", Name: "syntaxerror", Substring: "syntaxerror", Normalized: "invalid syntax", Fix: "fix the reported token, check parentheses/brackets, or run a formatter", Confidence: "high"},
		{Language: "python", Name: "recursionerror", Substring: "recursionerror", Normalized: "unbounded recursion", Fix: "add a base case, convert to iteration, or raise the recursion limit only as a last resort", Confidence: "medium"},

		{Language: "bash", Name: "syntax error", Substring: "syntax error", Normalized: "shell syntax failure", Fix: "check quoting, unclosed substitutions, and POSIX compatibility of the construct", Confidence: "high"},
		{Language: "bash", Name: "command not found", Substring: "command not found", Normalized: "missing executable", Fix: "install the command, fix PATH, or correct the command name", Confidence: "high"},
		{Language: "bash", Name: "bad interpreter", Substring: "bad interpreter", Normalized: "wrong shebang or missing interpreter", Fix: "fix the shebang line or install the referenced interpreter", Confidence: "high"},
		{Language: "bash", Name: "no such file", Substring: "no such file", Normalized: "missing file or directory", Fix: "verify path, create the file, or correct relative path usage", Confidence: "high"},
		{Language: "bash", Name: "permission denied", Substring: "permission denied", Normalized: "insufficient permissions", Fix: "adjust mode or ownership, run with sudo if correct, or fix the exec bit", Confidence: "high"},
		{Language: "bash", Name: "unbound variable", Substring: "unbound variable", Normalized: "missing parameter expansion", Fix: "use default expansion or check the variable before set -u scripts", Confidence: "high"},

		{Language: "lua", Name: "attempt to index", Substring: "attempt to index", Normalized: "nil/table access on non-table", Fix: "initialize the table, assert the type, or guard access with a nil check", Confidence: "high"},
		{Language: "lua", Name: "syntax error", Substring: "syntax error", Normalized: "invalid Lua syntax", Fix: "check do/end balance, function syntax, or missing delimiters", Confidence: "high"},
		{Language: "lua", Name: "module not found", Substring: "module not found", Normalized: "missing Lua module or package path", Fix: "set LUA_PATH/LUA_CPATH or install the module", Confidence: "high"},

		{Language: "ts", Name: "cannot find name", Substring: "cannot find name", Normalized: "missing type declaration or import", Fix: "install @types package, add the declaration, or enable dom/lib types", Confidence: "high"},
		{Language: "ts", Name: "type mismatch", Substring: "type mismatch", Normalized: "incompatible types", Fix: "use as const, narrow types, or align generics with the expected shape", Confidence: "high"},
		{Language: "ts", Name: "jsx element", Substring: "jsx element", Normalized: "JSX factory or import missing", Fix: "enable jsx in tsconfig or import the correct factory/function", Confidence: "medium"},

		{Language: "go", Name: "undefined:", Substring: "undefined:", Normalized: "undeclared identifier", Fix: "declare the variable, add a type or import, or fix the package clause", Confidence: "high"},
		{Language: "go", Name: "build constraints", Substring: "build constraints", Normalized: "//go:build conflict", Fix: "align build tags across files or remove contradictory tags", Confidence: "high"},
		{Language: "go", Name: "import cycle", Substring: "import cycle", Normalized: "circular package dependency", Fix: "introduce an internal package, move shared types, or use interfaces to break the cycle", Confidence: "high"},
		{Language: "go", Name: "missing return", Substring: "missing return", Normalized: "function must end with a return", Fix: "add return values on all paths or end with a final return", Confidence: "high"},

		{Language: "build", Name: "undefined reference", Substring: "undefined reference", Normalized: "missing symbol at link time", Fix: "add the library to linker inputs, check symbol visibility, or fix the source file path", Confidence: "high"},
		{Language: "build", Name: "multiple definition", Substring: "multiple definition", Normalized: "duplicate symbol", Fix: "remove duplicate definitions, mark inline, or use static for file-local symbols", Confidence: "high"},
		{Language: "build", Name: "recipe for target", Substring: "recipe for target", Normalized: "missing build rule", Fix: "add the target, fix path references, or include the right makefile", Confidence: "high"},
		{Language: "build", Name: "ninja: error", Substring: "ninja: error", Normalized: "build command failed", Fix: "inspect the underlying compile or link error above the ninja line", Confidence: "medium"},
	}
}

func renderErrtransResults(ctx context.Context, entries []errtransEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\n", e.Language, e.Pattern, e.Normalized, e.Confidence, e.Fix, e.Language))
	}
	return sb.String()
}
