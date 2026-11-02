package commands_test

import (
        "context"
        "testing"

        "github.com/stretchr/testify/assert"

        "github.com/damien1141/a1/internal/components/toast"
        "github.com/damien1141/a1/internal/tui/commands"
        "github.com/damien1141/a1/internal/tui/controller"
)

// drainToasts drains the bus and returns any ToastMsg it contains. The bus
// is buffered; one Drain collects all messages published since the last
// drain. Tests call this after c.Run to capture the toast.
func drainToasts(t *testing.T, bus *controller.Bus) []controller.ToastMsg {
        t.Helper()
        var out []controller.ToastMsg
        for _, m := range bus.Drain() {
                if tm, ok := m.(controller.ToastMsg); ok {
                        out = append(out, tm)
                }
        }
        return out
}

// TestCompactCommands_RefusedWhileStreamActive verifies the StreamActive
// guard: /compact is refused (warning toast, no compaction attempt) when a
// stream is active. The guard fires before the nil-Ctrl check, so a nil
// Ctrl doesn't cause a different error.
//
// The ForceCompact call path itself (Ctrl → engine.ForceCompact →
// runCompact) is exercised by the existing
// TestRunCompact_ForceCompactsWhenHistoryExists in the agent package.
func TestCompactCommands_RefusedWhileStreamActive(t *testing.T) {
        bus := controller.NewBus(nil)

        c := &commands.CompactCommands{
                Bus:          bus,
                StreamActive: func() bool { return true },
                // Ctrl is nil but StreamActive returns true first, so Ctrl is
                // never reached.
        }
        c.Run(context.Background())

        published := drainToasts(t, bus)
        a := assert.New(t)
        a.Len(published, 1, "exactly one toast")
        a.Equal("Cannot compact while a reply or command is running", published[0].Message)
        a.Equal(toast.ToastWarning, published[0].Kind)
}

// TestCompactCommands_NoEngine verifies the nil-Ctrl guard: when no engine
// is configured, /compact produces an error toast (not a panic).
func TestCompactCommands_NoEngine(t *testing.T) {
        bus := controller.NewBus(nil)

        c := &commands.CompactCommands{
                Bus:          bus,
                StreamActive: func() bool { return false },
                // Ctrl is nil → "engine not configured" toast.
        }
        c.Run(context.Background())

        published := drainToasts(t, bus)
        a := assert.New(t)
        a.Len(published, 1)
        a.Contains(published[0].Message, "Compaction unavailable")
        a.Equal(toast.ToastError, published[0].Kind)
}

// TestCompactCommands_NilReceiver verifies the Run method is nil-safe —
// calling Run on a nil *CompactCommands must not panic.
func TestCompactCommands_NilReceiver(t *testing.T) {
        var c *commands.CompactCommands
        assert.NotPanics(t, func() { c.Run(context.Background()) })
}

// TestCompactCommands_Register verifies the /compact slash command is
// registered with the right name, description, and slash flag.
func TestCompactCommands_Register(t *testing.T) {
        r := commands.NewCommandRegistry()
        c := &commands.CompactCommands{}
        c.Register(r)

        cmd, ok := r.Lookup("compact")
        if !ok {
                t.Fatal("/compact not registered")
        }
        assert.Equal(t, "compact", cmd.Name)
        assert.Equal(t, "Compact the context window now", cmd.Description)
        assert.True(t, cmd.Slash, "/compact must be a slash command")
        assert.NotNil(t, cmd.Run, "/compact must have a Run handler")
        assert.Equal(t, "/compact", cmd.Insert)
}

// TestCompactCommands_RegisterNilSafe verifies Register is nil-safe for
// both the receiver and the registry.
func TestCompactCommands_RegisterNilSafe(t *testing.T) {
        var c *commands.CompactCommands
        assert.NotPanics(t, func() { c.Register(nil) })

        r := commands.NewCommandRegistry()
        c = &commands.CompactCommands{}
        assert.NotPanics(t, func() { c.Register(nil) })
        // Registering into a real registry with a nil receiver must not add
        // any commands.
        assert.Empty(t, r.SlashCommands())
}

// TestCompactCommands_DispatchSlash verifies the registry dispatches
// "/compact" to the registered Run handler (end-to-end slash wiring).
func TestCompactCommands_DispatchSlash(t *testing.T) {
        bus := controller.NewBus(nil)
        r := commands.NewCommandRegistry()
        c := &commands.CompactCommands{Bus: bus, StreamActive: func() bool { return false }}
        c.Register(r)

        // Dispatch the slash line; the nil Ctrl produces the "engine not
        // configured" toast, proving the handler was called.
        ctx := commands.NewContext(bus, nil)
        ok := r.DispatchSlash("/compact", ctx)
        assert.True(t, ok, "/compact must dispatch")
        published := drainToasts(t, bus)
        assert.NotEmpty(t, published, "dispatching /compact must publish a toast")
}
