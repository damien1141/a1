package commands

import (
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/llm"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/tui/controller"
)

func TestSessionCommands_ShowOpensPicker(t *testing.T) {
        dir := t.TempDir()
        m, err := session.NewSessionManager(dir, session.WithSessionDir(dir), session.WithShouldFlush(true))
        require.NoError(t, err)
        _, err = m.Append(llm.Message{Role: llm.RoleUser, Content: "hello world"})
        require.NoError(t, err)
        _, err = m.Append(llm.Message{Role: llm.RoleAssistant, Content: "ok"})
        require.NoError(t, err)

        var got []session.SessionMeta
        var current string
        bus := controller.NewBus(nil)
        s := &SessionCommands{
                Bus:        bus,
                SessionDir: func() string { return dir },
                SessionID:  func() string { return m.ID() },
                OpenPicker: func(items []session.SessionMeta, currentID string) {
                        got = items
                        current = currentID
                },
        }
        s.Show()
        require.Len(t, got, 1)
        assert.Equal(t, m.ID(), got[0].ID)
        assert.Equal(t, m.ID(), current)
        assert.Equal(t, "hello world", got[0].Preview)
}

func TestSessionCommands_ShowEmptyToasts(t *testing.T) {
        bus := controller.NewBus(nil)
        opened := false
        s := &SessionCommands{
                Bus:        bus,
                SessionDir: func() string { return t.TempDir() },
                OpenPicker: func(list []session.SessionMeta, _ string) {
                        opened = true
                        assert.Empty(t, list, "no persisted sessions in a fresh temp dir")
                },
        }
        s.Show()
        // The picker opens even on an empty directory — the "New session"
        // row is always present. The old behavior (toast "No sessions" and
        // bail) hid the New session entry from the user.
        assert.True(t, opened, "picker must open so the user can pick New session")
        assert.Empty(t, drainToast(t, bus), "no toast on empty — the picker handles it")
}

func TestSessionCommands_AcceptBlocksWhenBusy(t *testing.T) {
        bus := controller.NewBus(nil)
        s := &SessionCommands{
                Bus:          bus,
                StreamActive: func() bool { return true },
        }
        s.Accept("abc")
        assert.Contains(t, drainToast(t, bus), "Cannot resume")
}

func TestSessionCommands_ClearBlocksWhenBusy(t *testing.T) {
        bus := controller.NewBus(nil)
        s := &SessionCommands{
                Bus:          bus,
                StreamActive: func() bool { return true },
        }
        s.Clear()
        assert.Contains(t, drainToast(t, bus), "Cannot clear")
}
