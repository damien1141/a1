package scripttool_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/scripttool"
	"github.com/damien1141/a1/internal/tools/tooldef"
)

// Smoke: the REAL tool registry (from tools.DefaultTools) dispatched through
// a real script over the real RPC loop. Not part of the suite's contract —
// a manual verification that the real tool handlers accept the
// stub-generated argument shapes.
func TestSmokeRealTools(t *testing.T) {
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		if _, err2 := os.Stat("/usr/local/bin/python3"); err2 != nil {
			t.Skip("no python")
		}
	}
	ws := t.TempDir()
	require := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	require(os.WriteFile(filepath.Join(ws, "alpha.txt"), []byte("first file\n"), 0o644))
	require(os.WriteFile(filepath.Join(ws, "beta.txt"), []byte("second file\n"), 0o644))

	reg := tools.NewRegistry(tools.DefaultTools())
	dispatch := func(ctx context.Context, name string, args json.RawMessage) (string, bool) {
		tool, ok := reg[name]
		if !ok {
			return "no tool " + name, false
		}
		res, err := tool.Run(tooldef.WithCwd(ctx, ws), args)
		if err != nil {
			return err.Error(), false
		}
		return res.Content, true
	}
	ctx := tooldef.WithScriptDispatch(tooldef.WithCwd(context.Background(), ws), dispatch)
	tool := scripttool.ScriptTool()
	input, err := json.Marshal(map[string]any{
		"code": `
from a1tools import read, search, bash
def body(text):
    # read() prefixes an "@file <path>" header; lines are "  N|content".
    return text.strip().splitlines()[1].split("|", 1)[1]
a = read("alpha.txt")
b = read("beta.txt")
hits = search("file", path=".")
listing = bash("ls -1")
print("A:", body(a))
print("B:", body(b))
print("SEARCH-HITS:", hits.count("alpha.txt") + hits.count("beta.txt"))
print("LS:", " ".join(sorted(line for line in listing.splitlines() if line.strip())))
`,
		"timeout": 30,
	})
	require(err)
	res, err := tool.Run(ctx, input)
	require(err)

	var out struct {
		Status        string `json:"status"`
		Output        string `json:"output"`
		ToolCallsMade int    `json:"tool_calls_made"`
	}
	require(json.Unmarshal([]byte(res.Content), &out))
	t.Logf("status=%s calls=%d\n%s", out.Status, out.ToolCallsMade, out.Output)
	if out.Status != "success" || out.ToolCallsMade != 4 {
		t.Fatalf("status=%s calls=%d output=%s", out.Status, out.ToolCallsMade, out.Output)
	}
	for _, want := range []string{"A: first file", "B: second file", "SEARCH-HITS: 2", "LS: alpha.txt beta.txt"} {
		if !strings.Contains(out.Output, want) {
			t.Fatalf("missing %q in output:\n%s", want, out.Output)
		}
	}
}
