package permission

import (
        "encoding/json"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"
)

// The personal-data action class (dashboard chunk 2): reads extract as
// ordinary reads, writes as ActionPersonal — consented out of band by the
// config flags, denied only where a mode's contract demands it (readonly:
// "this session writes nothing" includes the user's calendar).

func TestPersonalCalendarExtractMapsActions(t *testing.T) {
        for _, tc := range []struct {
                action string
                want   Action
        }{
                {"agenda", ActionRead},
                {"range", ActionRead},
                {"create", ActionPersonal},
                {"update", ActionPersonal},
                {"AGENDA", ActionRead}, // case-insensitive like the tool's dispatch
                {"delete", ActionPersonal},
                {"", ActionPersonal}, // malformed rides the personal class, never a read
        } {
                req, err := Extract("personal_calendar", json.RawMessage(`{"action":"`+tc.action+`"}`))
                require.NoError(t, err)
                assert.Equal(t, tc.want, req.Action, "action %q", tc.action)
        }
}

func TestPersonalActionAllowedInInteractive(t *testing.T) {
        g, err := NewGate(DefaultPolicy(), t.TempDir())
        require.NoError(t, err)
        dec, reason := g.Check(t.Context(), Request{Action: ActionPersonal, Tool: "personal_calendar"})
        assert.Equal(t, Allow, dec, "consent lives in the config flags, not a per-call prompt: %s", reason)
}

func TestPersonalActionDeniedInReadonly(t *testing.T) {
        g, err := NewGate(Policy{Mode: ModeReadonly}, t.TempDir())
        require.NoError(t, err)
        dec, _ := g.Check(t.Context(), Request{Action: ActionPersonal, Tool: "personal_calendar"})
        assert.Equal(t, Deny, dec, "readonly writes nothing — including calendar events")
}

func TestPersonalKanbanExtractMapsActions(t *testing.T) {
        for _, tc := range []struct {
                action string
                want   Action
        }{
                {"list", ActionRead},
                {"LIST", ActionRead}, // case-insensitive like the tool's dispatch
                {"add", ActionPersonal},
                {"move", ActionPersonal},
                {"edit", ActionPersonal},
                {"delete", ActionPersonal},
                {"", ActionPersonal},  // malformed rides the personal class, never a read
                {"nuke", ActionPersonal}, // unknown actions are not reads
        } {
                req, err := Extract("personal_kanban", json.RawMessage(`{"action":"`+tc.action+`"}`))
                require.NoError(t, err)
                assert.Equal(t, tc.want, req.Action, "action %q", tc.action)
        }
}

func TestPersonalKanbanReadSurvivesReadonly(t *testing.T) {
        g, err := NewGate(Policy{Mode: ModeReadonly}, t.TempDir())
        require.NoError(t, err)
        req, err := Extract("personal_kanban", json.RawMessage(`{"action":"list"}`))
        require.NoError(t, err)
        dec, _ := g.Check(t.Context(), req)
        assert.Equal(t, Allow, dec, "reading the board is a read")
        req, err = Extract("personal_kanban", json.RawMessage(`{"action":"move","id":"x","col":"done"}`))
        require.NoError(t, err)
        dec, _ = g.Check(t.Context(), req)
        assert.Equal(t, Deny, dec, "readonly writes nothing — including board moves")
}

func TestPersonalMailExtractMapsActions(t *testing.T) {
        for _, tc := range []struct {
                action string
                want   Action
        }{
                {"mailboxes", ActionRead},
                {"messages", ActionRead},
                {"headers", ActionRead},
                {"MESSAGES", ActionRead}, // case-insensitive like the tool's dispatch
                {"", ActionPersonal},          // malformed rides the personal class, never a read
                {"send", ActionPersonal},      // there is no write, but unknown is not a read
                {"mark_read", ActionPersonal}, // the ruling's tripwire: marking mail is a write
        } {
                req, err := Extract("personal_mail", json.RawMessage(`{"action":"`+tc.action+`"}`))
                require.NoError(t, err)
                assert.Equal(t, tc.want, req.Action, "action %q", tc.action)
        }
}

func TestPersonalMailReadSurvivesReadonly(t *testing.T) {
        g, err := NewGate(Policy{Mode: ModeReadonly}, t.TempDir())
        require.NoError(t, err)
        req, err := Extract("personal_mail", json.RawMessage(`{"action":"messages","box":"inbox"}`))
        require.NoError(t, err)
        dec, _ := g.Check(t.Context(), req)
        assert.Equal(t, Allow, dec, "reading the envelope is a read — readonly reads fine")
}
