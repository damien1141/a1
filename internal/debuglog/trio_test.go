package debuglog

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetState drops the cached env check and closes/clears the open log file
// so each test observes only its own A1_DEBUG / A1_DEBUG_FILE values. The
// package caches both in unexported globals, so the reset needs in-package
// access.
func resetState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		closeFileLocked()
		enabled = false
		checked = false
	})
	mu.Lock()
	defer mu.Unlock()
	closeFileLocked()
	enabled = false
	checked = false
}

// closeFileLocked closes any open log file and forgets it; t.TempDir cannot
// remove an open file on some platforms, so leaking the handle would break
// cleanup.
func closeFileLocked() {
	if file != nil {
		_ = file.Close()
		file = nil
	}
}

func TestLogf_Positive(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "debug.log")
	t.Setenv("A1_DEBUG", "true")
	t.Setenv("A1_DEBUG_FILE", logPath)
	resetState(t)

	require.True(t, Enabled())
	Logf("tool %s took %dms", "read", 42)
	DumpRunes("weird", "a\n\t\x01中")

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	content := string(data)

	// One session header per opened file, every line timestamped.
	assert.Equal(t, 1, strings.Count(content, "---- debug session "), "one header per opened file")
	assert.Regexp(t, `(?m)^\d{2}:\d{2}:\d{2}\.\d{3} tool read took 42ms$`, content)
	assert.Contains(t, content, "tool read took 42ms\n", "the formatted message lands with the appended newline")

	// DumpRunes renders byte len, rune count, and per-rune annotations:
	// "a\n\t\x01中" is 7 bytes / 5 runes and covers NL, TAB, CTRL, and the
	// CJK wide range.
	assert.Contains(t, content, "weird len=7 runes=5")
	for _, note := range []string{"U+000A", "<NL>", "U+0009", "<TAB>", "U+0001", "<CTRL>", "U+4E2D", "<WIDE?>"} {
		assert.Contains(t, content, note)
	}

	// Enabled is documented "cached after first check": clearing A1_DEBUG
	// later must not silence the already-open log. Empty string is
	// indistinguishable from unset for os.Getenv.
	t.Setenv("A1_DEBUG", "")
	Logf("still on")
	data, err = os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "still on")
}

func TestLogf_Negative(t *testing.T) {
	t.Run("disabled logging writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir) // the default a1-debug.log path would land here
		// Empty string is indistinguishable from unset for os.Getenv.
		t.Setenv("A1_DEBUG", "")
		t.Setenv("A1_DEBUG_FILE", "")
		resetState(t)

		assert.False(t, Enabled())
		Logf("must not appear")
		DumpRunes("label", "x")

		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Empty(t, entries, "no log file may be created while disabled")
	})

	t.Run("unopenable log file disables logging without panic", func(t *testing.T) {
		// A directory path makes the O_WRONLY open fail (EISDIR); the write
		// is dropped and the failure path flips the cached flag off instead
		// of taking the process down.
		t.Setenv("A1_DEBUG", "1")
		t.Setenv("A1_DEBUG_FILE", t.TempDir())
		resetState(t)

		assert.True(t, Enabled(), "env gate is on before the first write")
		require.NotPanics(t, func() {
			Logf("boom %d", 1)
			DumpRunes("label", "x")
		})
		assert.False(t, Enabled(), "the failed open must disable logging")
	})
}

// oracle: direct environment inspection — the naive reference recomputes
// the flag from os.Getenv on every call, with no cache; the real Enabled()
// (after a cache reset) must agree with it for every value.
func TestEnabled_Differential(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "True", "0", "false", "", "yes"} {
		t.Run("A1_DEBUG="+strconv.Quote(v), func(t *testing.T) {
			t.Setenv("A1_DEBUG", v)
			resetState(t)

			want := os.Getenv("A1_DEBUG") == "1" || strings.EqualFold(os.Getenv("A1_DEBUG"), "true")
			assert.Equal(t, want, Enabled())
		})
	}
}
