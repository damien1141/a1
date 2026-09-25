package session

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/llm"
)

// TestManagerConcurrentAccess guards the mutex: concurrent append/read
// must not race or corrupt the tree. Run with -race.
func TestManagerConcurrentAccess(t *testing.T) {
	m, err := NewSessionManager(t.TempDir(), WithShouldFlush(false))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for w := range 8 {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := range 50 {
				msg := llm.Message{
					Role:    llm.RoleUser,
					Content: fmt.Sprintf("w%d-%d", w, i),
				}
				id, err := m.Append(msg)
				assert.NoError(t, err)
				if err != nil {
					return
				}
				_ = m.BuildContext()
				_ = m.GetBranch(id)
				_ = m.Len()
			}
		}(w)
	}
	wg.Wait()

	require.GreaterOrEqual(t, m.Len(), 8*50)
}
