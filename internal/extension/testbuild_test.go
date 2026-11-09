package extension

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A checkout must test clean wherever it lives — including paths the go.mod
// tokenizer would shred: spaces, parentheses (a browser's duplicate-download
// " (1)" suffix), non-ASCII. The scratch extension module's replace
// directive quotes the SDK path for exactly that reason; unquoted, a tree at
// ".../workspace-584a6da6 (1)/download/a1_extracted" broke every
// extension-building test at "usage: replace module/path ...".
func TestExtensionGoModQuotesSDKPath(t *testing.T) {
	dir := t.TempDir()
	hostile := filepath.Join(dir, "workspace-584a6da6 (1) Café")
	if err := os.MkdirAll(hostile, 0o755); err != nil {
		t.Fatal(err)
	}
	sdk := filepath.Join(hostile, "ext", "go")

	mod := extensionGoMod("test", sdk)

	// The replace line must carry the SDK path as one quoted Go string that
	// unquotes back to the exact path.
	var quoted string
	for _, line := range strings.Split(mod, "\n") {
		rest, ok := strings.CutPrefix(line, "replace github.com/damien1141/a1/ext/go => ")
		if !ok {
			continue
		}
		quoted = strings.TrimSpace(rest)
	}
	if quoted == "" {
		t.Fatalf("no replace directive in generated go.mod:\n%s", mod)
	}
	got, err := strconv.Unquote(quoted)
	if err != nil {
		t.Fatalf("replace path is not a quoted Go string (%q): %v", quoted, err)
	}
	if want := filepath.ToSlash(sdk); got != want {
		t.Fatalf("unquoted replace path %q != SDK dir %q", got, want)
	}

	// And the toolchain must agree: the generated file parses, and the
	// replace target comes back out as the whole path — token-breakers and
	// all. go mod edit is purely syntactic, so the target need not exist.
	if err := os.WriteFile(filepath.Join(hostile, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "mod", "edit", "-json", "go.mod")
	cmd.Dir = hostile
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go mod edit rejected the generated go.mod: %v\n%s", err, mod)
	}
	var parsed struct {
		Replace []struct {
			New struct {
				Path string `json:"Path"`
			} `json:"New"`
		} `json:"Replace"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("go mod edit -json output: %v", err)
	}
	if len(parsed.Replace) != 1 || parsed.Replace[0].New.Path != filepath.ToSlash(sdk) {
		t.Fatalf("parsed replace targets %+v, want the whole SDK dir %q", parsed.Replace, filepath.ToSlash(sdk))
	}
}
