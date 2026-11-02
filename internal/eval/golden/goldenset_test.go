package golden

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTasksValidate(t *testing.T) {
	require.GreaterOrEqual(t, len(Tasks), 20, "the pinned set holds ~20 tasks")
	ids := map[string]bool{}
	langs := map[string]int{}
	for _, task := range Tasks {
		require.NoError(t, task.Validate(), "task %s must be well-formed", task.ID)
		assert.False(t, ids[task.ID], "duplicate task id %s", task.ID)
		ids[task.ID] = true
		langs[task.Language]++
	}
	assert.Equal(t, len(Tasks), len(ids), "ids are unique")
	for _, lang := range []string{"go", "rust", "python"} {
		assert.Positive(t, langs[lang], "the set covers %s", lang)
	}
}

func TestTaskFixturesCompileShape(t *testing.T) {
	// Structural sanity the grammar cannot express: every go task has a
	// go.mod, every rust task a Cargo.toml — the check commands (go test,
	// cargo test) cannot even run without them.
	for _, task := range Tasks {
		switch task.Language {
		case "go":
			assert.Contains(t, task.Fixture, "go.mod", "%s: go fixture needs go.mod", task.ID)
		case "rust":
			assert.Contains(t, task.Fixture, "Cargo.toml", "%s: rust fixture needs Cargo.toml", task.ID)
			assert.Contains(t, task.Fixture, filepath.Join("src", "lib.rs"), "%s: rust fixture needs src/lib.rs", task.ID)
		}
	}
}

func TestByIDAndIDs(t *testing.T) {
	for _, id := range IDs() {
		task, ok := ByID(id)
		require.True(t, ok, "IDs() lists %s but ByID misses it", id)
		assert.Equal(t, id, task.ID)
	}
	_, ok := ByID("no-such-task")
	assert.False(t, ok)
}

func TestValidateRejectsBadTask(t *testing.T) {
	assert.Error(t, Task{ID: "x", Language: "go"}.Validate(), "no prompt/fixture/checks")
	assert.Error(t, Task{ID: "x", Language: "go", Prompt: "p", Fixture: map[string]string{"a": "b"},
		Checks: []string{"make-it-pass"}}.Validate(), "check id outside the grammar")
}
