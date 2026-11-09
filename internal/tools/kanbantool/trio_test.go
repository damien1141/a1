package kanbantool

import (
        "context"
        "database/sql"
        "encoding/json"
        "testing"
        "time"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/kanban"
        "github.com/damien1141/a1/internal/vault"
)

// The trio for the personal_kanban facade. The unit under test is the
// FACADE, not the store (internal/kanban owns that trio): these tests pin
// the call-time flag enforcement, the argument surface, and the
// facade-vs-library parity.

// fixtureDeps builds a tool over a fresh vault. allowed are live funcs so
// a test can flip consent mid-flight — exactly the mis-mount scenario the
// call-time gate exists for.
type fixtureDeps struct {
        db          *sql.DB
        readAllowed bool
        writeOn     bool
}

func newFixtureDeps(t *testing.T) *fixtureDeps {
        t.Helper()
        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        t.Cleanup(func() { _ = v.Close() })
        return &fixtureDeps{db: v.DB(), readAllowed: true, writeOn: true}
}

// runTool invokes the facade with the given JSON args.
func runTool(t *testing.T, f *fixtureDeps, args string) (map[string]any, error) {
        t.Helper()
        tool := KanbanTool(Deps{
                DB:          func() *sql.DB { return f.db },
                ReadAllowed: func() bool { return f.readAllowed },
                WriteAllowed: func() bool { return f.writeOn },
        })
        res, err := tool.Run(context.Background(), json.RawMessage(args))
        if err != nil {
                return nil, err
        }
        var body map[string]any
        require.NoError(t, json.Unmarshal([]byte(res.Content), &body))
        return body, nil
}

// ---------- positive ----------

func TestKanbanToolListShowsBoard_Positive(t *testing.T) {
        f := newFixtureDeps(t)
        _, err := kanban.Add(f.db, kanban.AddInput{ID: "a", Col: kanban.ColTodo, Title: "First"})
        require.NoError(t, err)
        _, err = kanban.Add(f.db, kanban.AddInput{ID: "b", Col: kanban.ColDoing, Title: "Second"})
        require.NoError(t, err)

        body, err := runTool(t, f, `{"action":"list"}`)
        require.NoError(t, err)
        cards := body["cards"].([]any)
        require.Len(t, cards, 2)
        // The board arrives grouped by column; within a column, order is
        // position order. The facade adds nothing and hides nothing.
        first := cards[0].(map[string]any)
        assert.Equal(t, "b", first["id"], "doing sorts before todo in the store's row order")
        assert.Equal(t, "doing", first["col"])
        assert.Equal(t, float64(0), first["position"])
        assert.NotContains(t, first, "file", "no filesystem notions leak into the model's view")
}

func TestKanbanToolWriteRoundTrip_Positive(t *testing.T) {
        f := newFixtureDeps(t)

        added, err := runTool(t, f, `{"action":"add","title":"Ship chunk 3","col":"todo","notes":"drag & drop too"}`)
        require.NoError(t, err)
        assert.Equal(t, true, added["added"])
        card := added["card"].(map[string]any)
        id, ok := card["id"].(string)
        require.True(t, ok, "add returns the minted id")
        assert.Equal(t, "todo", card["col"])
        assert.Equal(t, float64(0), card["position"], "an omitted position appends")

        // Move by id, to a named column at the top of it.
        moved, err := runTool(t, f, `{"action":"move","id":"`+id+`","col":"doing","position":0}`)
        require.NoError(t, err)
        assert.Equal(t, true, moved["moved"])
        card = moved["card"].(map[string]any)
        assert.Equal(t, "doing", card["col"])

        // Edit patches only what is given; an empty notes string clears it.
        edited, err := runTool(t, f, `{"action":"edit","id":"`+id+`","title":"Ship chunk 3 (done)","notes":""}`)
        require.NoError(t, err)
        assert.Equal(t, true, edited["edited"])
        card = edited["card"].(map[string]any)
        assert.Equal(t, "Ship chunk 3 (done)", card["title"])
        assert.NotContains(t, card, "notes", "an explicit empty string clears the notes")

        // The default column is todo.
        defaulted, err := runTool(t, f, `{"action":"add","title":"No column given"}`)
        require.NoError(t, err)
        card = defaulted["card"].(map[string]any)
        assert.Equal(t, "todo", card["col"], "add without a col lands in todo")

        // Delete round-trips: the board is one card lighter.
        deleted, err := runTool(t, f, `{"action":"delete","id":"`+id+`"}`)
        require.NoError(t, err)
        assert.Equal(t, true, deleted["deleted"])
        body, err := runTool(t, f, `{"action":"list"}`)
        require.NoError(t, err)
        assert.Equal(t, float64(1), body["count"])
}

// ---------- negative ----------

// TestKanbanToolRefusedWhenFlagsOff_Negative is the flagship: the flags
// are read at CALL time, so a tool that was mounted under one consent
// state refuses the moment the live state says no — and a refused write
// leaves the board untouched.
func TestKanbanToolRefusedWhenFlagsOff_Negative(t *testing.T) {
        f := newFixtureDeps(t)
        _, err := kanban.Add(f.db, kanban.AddInput{ID: "seed", Col: kanban.ColTodo, Title: "Seed"})
        require.NoError(t, err)

        // Sanity: the tool works while consent holds.
        _, err = runTool(t, f, `{"action":"list"}`)
        require.NoError(t, err)

        // Consent flips (config save, model switch — any path). Reads refuse.
        f.readAllowed = false
        _, err = runTool(t, f, `{"action":"list"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "personal_read")

        // Reads still work; writes refuse, and nothing is written.
        f.readAllowed = true
        f.writeOn = false
        for _, args := range []string{
                `{"action":"add","title":"Nope"}`,
                `{"action":"move","id":"seed","col":"done"}`,
                `{"action":"edit","id":"seed","title":"Nope"}`,
                `{"action":"delete","id":"seed"}`,
        } {
                _, err = runTool(t, f, args)
                require.Error(t, err, "%s", args)
                assert.Contains(t, err.Error(), "personal_write", "%s", args)
        }
        cards, err := kanban.List(f.db)
        require.NoError(t, err)
        require.Len(t, cards, 1, "a refused write changed nothing")
        assert.Equal(t, "Seed", cards[0].Title, "the seed card is intact")
}

func TestKanbanToolNoVault_Negative(t *testing.T) {
        f := &fixtureDeps{db: nil, readAllowed: true, writeOn: true}
        _, err := runTool(t, f, `{"action":"list"}`)
        require.Error(t, err)
        assert.Contains(t, err.Error(), "no vault")
}

func TestKanbanToolBadInputs_Negative(t *testing.T) {
        f := newFixtureDeps(t)

        for _, tc := range []struct{ name, args, wantErr string }{
                {"unknown action", `{"action":"shrink"}`, "unknown action"},
                {"add without title", `{"action":"add"}`, "requires a title"},
                {"add blank title", `{"action":"add","title":"   "}`, "requires a title"},
                {"add unknown column", `{"action":"add","title":"X","col":"wip"}`, "unknown column"},
                {"move without id", `{"action":"move","col":"done"}`, "requires an id"},
                {"move changes nothing", `{"action":"move","id":"x"}`, "changes nothing"},
                {"move unknown column", `{"action":"move","id":"x","col":"backlog"}`, "unknown column"},
                {"move unknown id", `{"action":"move","id":"ghost","col":"done"}`, "no such card"},
                {"edit without id", `{"action":"edit","title":"X"}`, "requires an id"},
                {"edit changes nothing", `{"action":"edit","id":"x"}`, "changes nothing"},
                {"edit blank title", `{"action":"edit","id":"x","title":"  "}`, "needs a title"},
                {"edit unknown id", `{"action":"edit","id":"ghost","title":"X"}`, "no such card"},
                {"delete without id", `{"action":"delete"}`, "requires an id"},
                {"delete unknown id", `{"action":"delete","id":"ghost"}`, "no such card"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        _, err := runTool(t, f, tc.args)
                        require.Error(t, err)
                        assert.Contains(t, err.Error(), tc.wantErr)
                })
        }

        // The rejected calls left no trace.
        cards, err := kanban.List(f.db)
        require.NoError(t, err)
        assert.Empty(t, cards)
}

// ---------- differential ----------

// TestKanbanToolFacadeMatchesLibrary_Differential: the facade must be a
// faithful projection of the store, nothing more. After driving every
// write action through the facade (add, move, edit — delete covered in the
// round trip), the tool's list is compared card-by-card against
// kanban.List's independent view of the same vault — two code paths, one
// truth.
func TestKanbanToolFacadeMatchesLibrary_Differential(t *testing.T) {
        f := newFixtureDeps(t)
        _, err := kanban.Add(f.db, kanban.AddInput{ID: "seed", Col: kanban.ColTodo, Title: "Seeded directly"})
        require.NoError(t, err)

        // Every write path through the facade.
        _, err = runTool(t, f, `{"action":"add","title":"From the tool","col":"doing","notes":"n1"}`)
        require.NoError(t, err)
        _, err = runTool(t, f, `{"action":"move","id":"seed","col":"done","position":0}`)
        require.NoError(t, err)
        _, err = runTool(t, f, `{"action":"edit","id":"seed","title":"Edited via tool"}`)
        require.NoError(t, err)

        body, err := runTool(t, f, `{"action":"list"}`)
        require.NoError(t, err)
        rawCards, err := json.Marshal(body["cards"])
        require.NoError(t, err)
        var got []cardJSON
        require.NoError(t, json.Unmarshal(rawCards, &got))

        want, err := kanban.List(f.db)
        require.NoError(t, err)
        require.Equal(t, len(want), len(got), "facade and library agree on count")

        byID := map[string]kanban.Card{}
        for _, c := range want {
                byID[c.ID] = c
        }
        for _, g := range got {
                w, ok := byID[g.ID]
                require.True(t, ok, "facade card %q exists in the library's view", g.ID)
                assert.Equal(t, w.Col, g.Col, g.ID)
                assert.Equal(t, w.Position, g.Position, g.ID)
                assert.Equal(t, w.Title, g.Title, g.ID)
                assert.Equal(t, w.Notes, g.Notes, g.ID)
                assert.True(t, w.CreatedAt.Equal(mustTime(t, g.CreatedAt)), "%s: created_at %s vs %s", g.ID, g.CreatedAt, w.CreatedAt)
                assert.True(t, w.UpdatedAt.Equal(mustTime(t, g.UpdatedAt)), "%s: updated_at %s vs %s", g.ID, g.UpdatedAt, w.UpdatedAt)
        }
        // The seeded card carried the edit through both views.
        assert.Equal(t, "Edited via tool", byID["seed"].Title)
}

func mustTime(t *testing.T, s string) (r time.Time) {
        t.Helper()
        var err error
        if r, err = time.Parse(time.RFC3339, s); err != nil {
                t.Fatalf("bad time %q: %v", s, err)
        }
        return r
}
