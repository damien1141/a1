package engineaccess

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/permission"
)

// stubEngine is the concrete Engine these tests store and read back. Gate
// is nil-receiver safe, mirroring agent.Engine.Gate, because a typed-nil
// engine can still be registered at this boundary and callers that forward
// one must cope.
type stubEngine struct {
	gate permission.Gate
}

func (e *stubEngine) Gate() permission.Gate {
	if e == nil {
		return nil
	}
	return e.gate
}

// resetActiveEngine swaps in a fresh atomic.Value so every test starts
// from "nothing registered" regardless of what earlier tests left behind;
// the in-package access keeps the reset independent of the public clear
// path it also exercises. Tests must not run in parallel while sharing this
// global.
func resetActiveEngine(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { activeEngine = atomic.Value{} })
	activeEngine = atomic.Value{}
}

func TestSetActiveEngine_Positive(t *testing.T) {
	resetActiveEngine(t)

	first := &stubEngine{}
	SetActiveEngine(first)
	got := ActiveEngine()
	require.NotNil(t, got)
	assert.Same(t, first, got, "the registered engine must read back unchanged")

	// The registry holds one engine: a later registration overwrites.
	second := &stubEngine{}
	SetActiveEngine(second)
	assert.Same(t, second, ActiveEngine())
	assert.NotSame(t, first, ActiveEngine())
}

func TestActiveEngine_Negative(t *testing.T) {
	t.Run("nothing registered returns nil", func(t *testing.T) {
		resetActiveEngine(t)
		assert.Nil(t, ActiveEngine())
	})

	t.Run("nil store clears the registry", func(t *testing.T) {
		resetActiveEngine(t)
		first := &stubEngine{}
		SetActiveEngine(first)
		require.NotNil(t, ActiveEngine())
		// SetActiveEngine is documented "Pass nil to clear": the engine is
		// stored inside a box, so atomic.Value's nil-interface rejection
		// never fires and the registry must read back empty.
		assert.NotPanics(t, func() { SetActiveEngine(nil) })
		assert.Nil(t, ActiveEngine(), "the nil store must clear the registry")
	})

	t.Run("typed nil clears nothing at this boundary", func(t *testing.T) {
		resetActiveEngine(t)
		// A typed nil (*stubEngine)(nil) is a non-nil Engine interface, so it
		// registers and reads back as "occupied" here — a store is a store,
		// whatever the pointer inside holds. Callers cope by keeping Gate
		// nil-receiver safe; the production clear path
		// (agent.SetActiveEngine(nil)) now forwards a literal nil instead.
		SetActiveEngine((*stubEngine)(nil))
		got := ActiveEngine()
		// testify's NotNil would unwrap to the nil pointer, so assert
		// the interface itself is non-nil — that is the trap callers hit
		// if they forward a typed nil.
		require.True(t, got != nil, "a typed-nil engine must not read back as a nil Engine")
		var gate permission.Gate
		require.NotPanics(t, func() { gate = got.Gate() })
		assert.Nil(t, gate)
	})
}

// oracle: naive reference registry — a plain variable holding the current
// engine, updated in lockstep with every SetActiveEngine call; the real
// atomic.Value-backed registry must return exactly what the naive one holds
// after each step of the set / overwrite / typed-nil store / literal-nil
// clear sequence.
func TestEngineRegistry_Differential(t *testing.T) {
	resetActiveEngine(t)

	a := &stubEngine{}
	b := &stubEngine{}

	var ref Engine // naive reference: the last stored engine, nil = empty
	assert.Nil(t, ActiveEngine())
	assert.Equal(t, ref, ActiveEngine())

	for i, e := range []Engine{a, b, a, (*stubEngine)(nil), b, nil, a, nil} {
		SetActiveEngine(e)
		ref = e
		assert.Equal(t, ref, ActiveEngine(), "step %d: registry must match the naive reference", i)
	}
}
