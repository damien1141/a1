package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllThinkModes_ContainsDefaultAndOff(t *testing.T) {
	modes := AllThinkModes()
	assert.Contains(t, modes, Off)
	assert.Contains(t, modes, Default)
	assert.Equal(t, Default, modes[len(modes)-1], "default is the last selectable mode")
}

func TestAllThinkModes_CycleCoversEveryMode(t *testing.T) {
	// Tab cycles through every mode and returns to the start, so the
	// switcher is a closed loop over AllThinkModes().
	modes := AllThinkModes()
	seen := make(map[ThinkMode]bool)
	cur := Off
	for i := 0; i <= len(modes); i++ {
		seen[cur] = true
		idx := 0
		for j, m := range modes {
			if m == cur {
				idx = (j + 1) % len(modes)
				break
			}
		}
		cur = modes[idx]
	}
	assert.Len(t, seen, len(modes))
}
