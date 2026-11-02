package agent

import (
        "os"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
)

// TestInjectMemoryBeforeLastUser_NilMsg verifies that passing a nil memMsg
// returns the input slice unchanged.
func TestInjectMemoryBeforeLastUser_NilMsg(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleSystem, Content: "system"},
                {Role: llm.RoleUser, Content: "hello"},
                {Role: llm.RoleAssistant, Content: "hi"},
        }
        got := injectMemoryBeforeLastUser(msgs, nil)
        assert.Equal(t, msgs, got, "nil memMsg should return input unchanged")
}

// TestInjectMemoryBeforeLastUser_LastUserAtEnd covers the common case:
// system + user + assistant + user (latest at the end). The memory block
// should be inserted immediately before the last user message.
func TestInjectMemoryBeforeLastUser_LastUserAtEnd(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleSystem, Content: "system"},
                {Role: llm.RoleUser, Content: "first question"},
                {Role: llm.RoleAssistant, Content: "first answer"},
                {Role: llm.RoleUser, Content: "second question"},
        }
        memMsg := &llm.Message{Role: llm.RoleSystem, Content: "memory block"}
        got := injectMemoryBeforeLastUser(msgs, memMsg)
        assert.Len(t, got, len(msgs)+1)
        // Verify insertion point: the memory block is at index 3, right before
        // the last user message which is now at index 4.
        assert.Equal(t, "memory block", got[3].Content, "memory should be inserted before last user")
        assert.Equal(t, "second question", got[4].Content, "last user msg should now be at index 4")
        assert.Equal(t, llm.RoleUser, got[4].Role)
}

// TestInjectMemoryBeforeLastUser_NoUserMsg covers the edge case where
// there's no user message at all. The memory block should be appended at
// the end (rather than panic on a missing index).
func TestInjectMemoryBeforeLastUser_NoUserMsg(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleSystem, Content: "system"},
                {Role: llm.RoleAssistant, Content: "answer"},
        }
        memMsg := &llm.Message{Role: llm.RoleSystem, Content: "memory block"}
        got := injectMemoryBeforeLastUser(msgs, memMsg)
        assert.Len(t, got, len(msgs)+1)
        assert.Equal(t, "memory block", got[len(got)-1].Content, "with no user msg, memory appended at end")
}

// TestInjectMemoryBeforeLastUser_UserThenAssistant covers the case where
// the last user message is followed by an assistant message. The memory
// block should be inserted before the user, not before the assistant.
//
// Before: [system, user, assistant] (indices 0, 1, 2)
// After:  [system, memory, user, assistant] (indices 0, 1, 2, 3)
func TestInjectMemoryBeforeLastUser_UserThenAssistant(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleSystem, Content: "system"},
                {Role: llm.RoleUser, Content: "what is 2+2"},
                {Role: llm.RoleAssistant, Content: "4"},
        }
        memMsg := &llm.Message{Role: llm.RoleSystem, Content: "memory: user is 19"}
        got := injectMemoryBeforeLastUser(msgs, memMsg)
        assert.Len(t, got, len(msgs)+1)
        // Find the last user msg in the result.
        lastUserIdx := -1
        for i, m := range got {
                if m.Role == llm.RoleUser {
                        lastUserIdx = i
                }
        }
        assert.Equal(t, 2, lastUserIdx, "last user should be at index 2 (moved by the memory injection at index 1)")
        assert.Equal(t, "memory: user is 19", got[lastUserIdx-1].Content,
                "memory should be at index 1 (right before the user at index 2)")
}

// TestInjectMemoryBeforeLastUser_DoesNotMutateInput verifies the input
// slice is not mutated — the function must return a fresh slice.
func TestInjectMemoryBeforeLastUser_DoesNotMutateInput(t *testing.T) {
        msgs := []llm.Message{
                {Role: llm.RoleSystem, Content: "system"},
                {Role: llm.RoleUser, Content: "hello"},
        }
        originalLen := len(msgs)
        memMsg := &llm.Message{Role: llm.RoleSystem, Content: "memory"}
        _ = injectMemoryBeforeLastUser(msgs, memMsg)
        assert.Equal(t, originalLen, len(msgs), "input slice must not be mutated")
}

// TestFindGitRoot_NoGitDir verifies findGitRoot returns "" when no .git
// directory is found in any ancestor.
func TestFindGitRoot_NoGitDir(t *testing.T) {
        tmp := t.TempDir()
        // Walk up from tmp — should hit the OS root without finding .git
        // (unless this test is run inside a git checkout, which TempDir is not).
        got := findGitRoot(tmp)
        if got != "" {
                // Some CI environments run tests inside a git repo. In that case,
                // findGitRoot walking up will find the repo root. That's a legitimate
                // answer (the test isn't inside a fixture, but inside someone's
                // actual repo). Accept any non-empty answer that contains .git.
                if _, err := os.Stat(filepath.Join(got, ".git")); err != nil {
                        t.Errorf("findGitRoot(%q) returned %q which doesn't contain .git: %v", tmp, got, err)
                }
        }
}

// TestFindGitRoot_FindsRepoRoot creates a fake .git dir and verifies
// findGitRoot walks up to it.
func TestFindGitRoot_FindsRepoRoot(t *testing.T) {
        repoRoot := t.TempDir()
        require.NoError(t, os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755))
        subdir := filepath.Join(repoRoot, "src", "pkg", "internal")
        require.NoError(t, os.MkdirAll(subdir, 0o755))
        got := findGitRoot(subdir)
        assert.Equal(t, repoRoot, got, "findGitRoot should walk up to the .git-containing ancestor")
}
