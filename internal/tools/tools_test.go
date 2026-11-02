package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultTools_CollapsedSet locks in the post-collapse tool set.
// Before the collapse (worklog Task ID 3), DefaultTools() had 45 entries;
// after, it had 18. This test guards against silent regressions: if a
// future change re-adds a tool that was folded into a dispatcher (e.g.
// adds `edit` back alongside `write`), the count or name-set assertion
// here will fail and force the author to either drop the addition or
// update this test with a deliberate choice.
//
// `git` (19th entry) is a deliberate Phase 1 close-out addition: one
// facade tool, 12 modes, APPA-gated per mode — not a dispatcher regression.
//
// The expected list is alphabetical so the diff is scannable when this
// test fails. Order isn't part of the contract — only the set.
func TestDefaultTools_CollapsedSet(t *testing.T) {
	tools := DefaultTools()
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Definition.Name] = true
	}

	expected := []string{
		"ask_user",
		"bash",
		"browser",
		"browser_interact",
		"browser_navigate",
		"browser_read",
		"code",
		"context",
		"docsync",
		"document",
		"fetch",
		"git",
		"permission",
		"read",
		"search",
		"secret",
		"test",
		"vector_search",
		"write",
	}
	for _, name := range expected {
		assert.True(t, names[name], "expected tool %q in DefaultTools()", name)
	}
	// No extras: every tool returned must be in the expected set.
	for name := range names {
		require.Contains(t, expected, name, "DefaultTools() returned unexpected tool %q — update the expected list or remove the registration", name)
	}
	assert.Equal(t, len(expected), len(tools), "DefaultTools() count drifted from %d", len(expected))
}

// TestReadonlyTools_CollapsedSet mirrors the above for the read-only tool
// set. ReadonlyTools drops the writable tools (write, anything that runs
// binaries) but keeps everything else. The expected list is the
// DefaultTools set minus `write` — `git` stays because its mutating modes
// are gated to Deny by ModeReadonly at the permission layer.
func TestReadonlyTools_CollapsedSet(t *testing.T) {
	tools := ReadonlyTools()
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Definition.Name] = true
	}

	expected := []string{
		"ask_user",
		"bash",
		"browser",
		"browser_interact",
		"browser_navigate",
		"browser_read",
		"code",
		"context",
		"docsync",
		"document",
		"fetch",
		"git",
		"permission",
		"read",
		"search",
		"secret",
		"test",
		"vector_search",
	}
	for _, name := range expected {
		assert.True(t, names[name], "expected tool %q in ReadonlyTools()", name)
	}
	for name := range names {
		require.Contains(t, expected, name, "ReadonlyTools() returned unexpected tool %q — update the expected list or remove the registration", name)
	}
	assert.Equal(t, len(expected), len(tools), "ReadonlyTools() count drifted from %d", len(expected))
}

// TestReadonlyTools_HasNoWrite confirms the read-only set excludes the
// writable tools. This is a targeted check that would catch a regression
// even if the expected-set list above gets out of date.
func TestReadonlyTools_HasNoWrite(t *testing.T) {
	for _, tool := range ReadonlyTools() {
		assert.NotEqual(t, "write", tool.Definition.Name, "ReadonlyTools must not include `write`")
	}
}

// TestDefaultTools_AllToolsHaveRun confirms every registered tool has a
// non-nil Run handler. A registration with a nil Run would panic on the
// first call from the LLM; this catches that at test time rather than at
// runtime.
func TestDefaultTools_AllToolsHaveRun(t *testing.T) {
	for _, tool := range DefaultTools() {
		require.NotNil(t, tool.Run, "tool %q has nil Run handler", tool.Definition.Name)
	}
	for _, tool := range ReadonlyTools() {
		require.NotNil(t, tool.Run, "readonly tool %q has nil Run handler", tool.Definition.Name)
	}
}

// TestDefaultTools_NoDuplicateNames confirms no tool name is registered
// twice in either set. The dispatcher collapse relies on unique names
// (the LLM picks by name); a duplicate would silently shadow one of the
// registrations.
func TestDefaultTools_NoDuplicateNames(t *testing.T) {
	for _, set := range [][]Tool{DefaultTools(), ReadonlyTools()} {
		seen := make(map[string]bool, len(set))
		for _, tool := range set {
			require.False(t, seen[tool.Definition.Name], "duplicate tool name %q in same set", tool.Definition.Name)
			seen[tool.Definition.Name] = true
		}
	}
}
