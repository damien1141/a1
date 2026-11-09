package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/tools"
	"github.com/damien1141/a1/internal/tools/calendartool"
	"github.com/damien1141/a1/internal/tools/kanbantool"
	"github.com/damien1141/a1/internal/tools/mailtool"
	"github.com/damien1141/a1/internal/tools/memtool"
	"github.com/damien1141/a1/internal/tools/sessiontool"
	"github.com/damien1141/a1/internal/tools/skilltool"
)

// TestMain removes the directory holding the once-built binary after all
// tests; the binary itself outlives individual tests by design (one build
// per test-binary run), so no single test's TempDir can own it.
func TestMain(m *testing.M) {
	code := m.Run()
	if binPath != "" {
		_ = os.RemoveAll(filepath.Dir(binPath))
	}
	os.Exit(code)
}

// auditRow and auditDoc mirror the -json output shape of the command.
type auditRow struct {
	Name      string `json:"name"`
	DescTok   int    `json:"desc_tokens"`
	SchemaTok int    `json:"schema_tokens"`
	TotalTok  int    `json:"total_tokens"`
}

type auditDoc struct {
	Tools []auditRow `json:"tools"`
	Total int        `json:"total_tokens"`
}

var (
	buildOnce sync.Once
	binPath   string
	buildErr  error
)

// toolauditBin builds the command once per test-binary run and returns its
// path. `go build` (not `go run`) so every test exercises the same binary;
// the once keeps the cost at one build for the whole package. The build
// inherits the environment of whoever ran `go test` — the go toolchain is
// expected on PATH, same as for the outer test run.
func toolauditBin(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			buildErr = fmt.Errorf("runtime.Caller failed to locate the test file")
			return
		}
		repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
		dir, err := os.MkdirTemp("", "toolaudit-test-")
		if err != nil {
			buildErr = err
			return
		}
		binPath = filepath.Join(dir, "toolaudit")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}
		cmd := exec.CommandContext(t.Context(), "go", "build", "-o", binPath, "./cmd/toolaudit")
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("go build ./cmd/toolaudit: %w\n%s", err, out)
			return
		}
	})
	require.NoError(t, buildErr)
	return binPath
}

// runToolaudit runs the built binary and returns stdout, stderr, and the
// run error.
func runToolaudit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), toolauditBin(t), args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

func TestToolauditJSON_Positive(t *testing.T) {
	stdout, _, err := runToolaudit(t, "-json")
	require.NoError(t, err)

	var doc auditDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))
	require.NotEmpty(t, doc.Tools)

	sum := 0
	for i, r := range doc.Tools {
		assert.NotEmpty(t, r.Name, "row %d needs a tool name", i)
		assert.Equal(t, r.DescTok+r.SchemaTok, r.TotalTok, "row %d (%s): total must be desc+schema", i, r.Name)
		if i > 0 {
			assert.GreaterOrEqual(t, doc.Tools[i-1].TotalTok, r.TotalTok,
				"rows must be sorted by total_tokens descending (row %d)", i)
		}
		sum += r.TotalTok
	}
	assert.Positive(t, doc.Total)
	assert.Equal(t, sum, doc.Total, "top-level total must be the sum of the row totals")
}

func TestToolauditBadFlag_Negative(t *testing.T) {
	stdout, stderr, err := runToolaudit(t, "-bogus")
	require.Error(t, err)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 2, exitErr.ExitCode(), "flag.ExitOnError exits 2 on an unknown flag")
	assert.Contains(t, stderr, "flag provided but not defined: -bogus")
	assert.Contains(t, stderr, "-json", "usage must list the one real flag")
	assert.Empty(t, stdout, "no table may be emitted for a rejected invocation")
}

// oracle: naive reimplementation — the test independently recomputes the
// audit table from the same definition sources main.go imports
// (tools.DefaultTools, skilltool.ListSkillsTool("skills"),
// memtool.Tools(nil), and the personal trio + session_search under stub
// deps), applying the same len/4 token heuristic, and compares it to the
// binary's -json output. Rows are compared as a sorted multiset because
// the command's sort.Slice is unstable under ties. Table mode's TOTAL
// line is then cross-checked against the -json total so both output
// paths of the same program must agree.
func TestToolauditTokens_Differential(t *testing.T) {
	all := append(tools.DefaultTools(), skilltool.ListSkillsTool("skills"))
	all = append(all, memtool.Tools(nil)...)
	all = append(all,
		calendartool.CalendarTool(calendartool.Deps{
			CalendarDir: func() string { return "" },
		}),
		kanbantool.KanbanTool(kanbantool.Deps{
			DB: func() *sql.DB { return nil },
		}),
		mailtool.MailTool(mailtool.Deps{
			MailDir: func() string { return "" },
		}),
		sessiontool.SessionSearchTool(".", ""),
	)

	wantRows := make([]string, 0, len(all))
	wantTotal := 0
	for _, tl := range all {
		schema, err := json.Marshal(tl.Definition.Params)
		require.NoError(t, err)
		desc := len(tl.Definition.Description) / 4
		schemaTok := len(schema) / 4
		wantTotal += desc + schemaTok
		wantRows = append(wantRows, fmt.Sprintf("%s %d %d %d", tl.Definition.Name, desc, schemaTok, desc+schemaTok))
	}
	sort.Strings(wantRows)

	stdout, _, err := runToolaudit(t, "-json")
	require.NoError(t, err)
	var doc auditDoc
	require.NoError(t, json.Unmarshal([]byte(stdout), &doc))

	gotRows := make([]string, 0, len(doc.Tools))
	for _, r := range doc.Tools {
		gotRows = append(gotRows, fmt.Sprintf("%s %d %d %d", r.Name, r.DescTok, r.SchemaTok, r.TotalTok))
	}
	sort.Strings(gotRows)

	assert.Equal(t, wantRows, gotRows, "per-tool numbers must match the naive recomputation")
	assert.Equal(t, wantTotal, doc.Total, "-json total must match the naive recomputation")

	// Table mode is the second output path of the same program; its TOTAL
	// line must agree with the JSON total.
	tableOut, _, err := runToolaudit(t)
	require.NoError(t, err)
	tableTotal, ok := tableTotalLine(tableOut)
	require.True(t, ok, "table output must contain a TOTAL line:\n%s", tableOut)
	assert.Equal(t, doc.Total, tableTotal, "table TOTAL must equal the -json total")
}

// tableTotalLine extracts the number from the trailing "TOTAL" row.
func tableTotalLine(out string) (int, bool) {
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "TOTAL" {
			if n, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
				return n, true
			}
		}
	}
	return 0, false
}
