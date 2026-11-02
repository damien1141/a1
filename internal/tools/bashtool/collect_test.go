package bashtool

import (
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCappedBufferKeepsNewestTail(t *testing.T) {
	cb := newCappedBuffer(32)
	full := strings.Repeat("0123456789", 100)
	for range 100 {
		_, err := cb.Write([]byte("0123456789"))
		require.NoError(t, err)
	}
	require.Equal(t, full[len(full)-32:], cb.String(), "want the newest 32 bytes")
	require.True(t, cb.Truncated(), "want truncated")
}

func TestBashOutputTailKeepsNewestLinesAndBytes(t *testing.T) {
	tail := NewBashOutputTail(3, 64)
	_, err := tail.WriteString("one\ntwo\nthree\nfour\n")
	require.NoError(t, err)
	got, truncated := tail.Snapshot()
	require.Equal(t, "two\nthree\nfour\n", got)
	require.True(t, truncated)

	tail = NewBashOutputTail(100, 10)
	_, err = tail.WriteString("0123456789ABC")
	require.NoError(t, err)
	got, truncated = tail.Snapshot()
	require.Equal(t, "3456789ABC", got)
	require.True(t, truncated, "want newest 10 bytes")
}

func TestCappedBufferNoTruncationUnderLimit(t *testing.T) {
	cb := newCappedBuffer(64)
	data := "hello world"
	_, err := cb.Write([]byte(data))
	require.NoError(t, err)
	require.Equal(t, data, cb.String())
	require.False(t, cb.Truncated())
}

func TestCappedBufferExactLimit(t *testing.T) {
	cb := newCappedBuffer(10)
	data := "0123456789"
	_, err := cb.Write([]byte(data))
	require.NoError(t, err)
	require.Equal(t, data, cb.String())
	require.False(t, cb.Truncated())
}

func TestCappedBufferConcurrentWrites(t *testing.T) {
	// Preserve the collector's defensive concurrency contract even though the
	// current os/exec setup coalesces stdout and stderr onto one writer path.
	cb := newCappedBuffer(1024)
	var wg sync.WaitGroup
	for _, g := range []string{"a", "b"} {
		wg.Add(1)
		go func(g string) {
			defer wg.Done()
			for range 5000 {
				_, _ = cb.Write([]byte(g))
			}
		}(g)
	}
	wg.Wait()
	assert.Len(t, cb.String(), 1024)
	assert.True(t, cb.Truncated())
}
