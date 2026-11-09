package scripttool

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- differential ----------

// naiveCapStdout rebuilds the 40/60 head-tail contract by hand: 20480 head
// bytes, the truncation marker, 30720 tail bytes, and the spill path derived
// from an independent sha256 of the text.
func naiveCapStdout(text string) (inline, spillPath string) {
	const capBytes = 50 * 1024
	if len(text) <= capBytes {
		return text, ""
	}
	const head = 20480 // 40% of the 50 KiB inline budget
	const tail = 30720 // the remaining 60%
	sum := sha256.Sum256([]byte(text))
	spill := filepath.Join(os.TempDir(), "a1-script-spill", "stdout-"+hex.EncodeToString(sum[:6])+".txt")
	marker := "[script stdout truncated: showing " + strconv.Itoa(capBytes) + " of " +
		strconv.Itoa(len(text)) + " bytes — full output: " + spill +
		" — read it back with read(path=" + strconv.Quote(spill) + ", offset=...) instead of re-running]"
	return text[:head] + marker + text[len(text)-tail:], spill
}

// oracle: naive reference implementation — the head/tail split, marker text,
// and content-digest spill path all recomputed independently (crypto/sha256
// directly, byte arithmetic by hand) and compared byte for byte.
func TestCapStdout_MatchesNaiveReference_Differential(t *testing.T) {
	// Keep the spill write hermetic: TMPDIR points at the test's own dir for
	// both the tool and the reference.
	t.Setenv("TMPDIR", t.TempDir())

	t.Run("exactly at the cap passes through", func(t *testing.T) {
		text := strings.Repeat("x", 50*1024)
		inline, spill := capStdout(text)
		wantInline, wantSpill := naiveCapStdout(text)
		assert.Equal(t, wantInline, inline)
		assert.Empty(t, spill)
		assert.Empty(t, wantSpill)
	})

	t.Run("one byte over truncates", func(t *testing.T) {
		text := strings.Repeat("y", 50*1024+1)
		inline, spill := capStdout(text)
		wantInline, wantSpill := naiveCapStdout(text)
		assert.Equal(t, wantInline, inline)
		assert.Equal(t, wantSpill, spill)
	})

	t.Run("large output with distinct head and tail", func(t *testing.T) {
		var b strings.Builder
		for i := 0; b.Len() <= 64*1024; i++ {
			fmt.Fprintf(&b, "row-%05d\n", i)
		}
		text := b.String()
		inline, spill := capStdout(text)
		wantInline, wantSpill := naiveCapStdout(text)
		assert.Equal(t, wantInline, inline)
		assert.Equal(t, wantSpill, spill)
		spilled, err := os.ReadFile(spill)
		require.NoError(t, err)
		assert.Equal(t, text, string(spilled), "the spill holds the full text")
	})
}

// oracle: round-trip through two distinct renderings — the generated stub
// module is parsed back (def lines, docstrings, _call targets) and the parsed
// signatures must appear verbatim in the tool description's function index,
// so the model-facing docs cannot drift from the module the scripts import.
func TestStubDocsMatchStubModule_Differential(t *testing.T) {
	lines := strings.Split(stubModule(), "\n")
	type fn struct{ name, stub, doc string }
	var fns []fn
	for i := 0; i+2 < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "def ") || !strings.HasSuffix(lines[i], "):") {
			continue
		}
		body := lines[i+2]
		if !strings.HasPrefix(body, `    return _call(`) {
			continue // helper, not an exposed tool
		}
		sig := strings.TrimSuffix(strings.TrimPrefix(lines[i], "def "), ":")
		open := strings.Index(sig, "(")
		name := sig[:open]
		stub := sig[open+1 : len(sig)-1]
		docLine := lines[i+1]
		require.True(t, strings.HasPrefix(docLine, `    """`), "docstring expected after def %s", name)
		require.True(t, strings.HasSuffix(docLine, `"""`), "docstring must close on one line for %s", name)
		doc := strings.TrimSuffix(strings.TrimPrefix(docLine, `    """`), `"""`)
		require.Contains(t, body, fmt.Sprintf("return _call(%q,", name), "stub must dispatch to its own name")
		fns = append(fns, fn{name: name, stub: stub, doc: doc})
	}

	names := make([]string, 0, len(fns))
	for _, f := range fns {
		names = append(names, f.name)
	}
	assert.Equal(t, []string{"read", "write", "edit", "search", "bash", "fetch"}, names)

	desc := ScriptTool().Definition.Description
	for _, f := range fns {
		assert.Contains(t, desc, fmt.Sprintf("  %s(%s)\n    %s\n", f.name, f.stub, f.doc),
			"description must document %s exactly as the stub module defines it", f.name)
	}
}
