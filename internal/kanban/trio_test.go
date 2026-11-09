package kanban

import (
        "fmt"
        "math/rand"
        "path/filepath"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/vault"
)

// The kanban store's trio. The flagship is the position-invariant oracle:
// after any sequence of ops, positions are dense per column AND the stored
// order matches a naive in-memory model's order — the model re-sorts its
// own slice per op, so a store that left holes, duplicated positions, or
// mis-ordered a column cannot pass.

// newTestVault opens a fresh real vault (the deployment container: WAL,
// busy timeout, the same schema the ops lazily extend).
func newTestVault(t *testing.T) *vault.Vault {
        t.Helper()
        v, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        t.Cleanup(func() { _ = v.Close() })
        return v
}

// intptr is the tests' pointer helper for explicit target indices.
func intptr(i int) *int { return &i }

// ---------- positive ----------

func TestKanbanAddListRoundTrip_Positive(t *testing.T) {
        db := newTestVault(t).DB()

        first, err := Add(db, AddInput{ID: "c1", Col: ColTodo, Title: "Ship the board", Notes: "chunk 3"})
        require.NoError(t, err)
        assert.Equal(t, "c1", first.ID)
        assert.Equal(t, ColTodo, first.Col)
        assert.Equal(t, 0, first.Position)
        assert.False(t, first.CreatedAt.IsZero(), "created_at is set")
        assert.False(t, first.UpdatedAt.IsZero(), "updated_at is set")

        // A second card appends (the nil Position — the zero value — is the
        // natural append); a target index inserts before it.
        second, err := Add(db, AddInput{ID: "c2", Col: ColTodo, Title: "Second"})
        require.NoError(t, err)
        assert.Equal(t, 1, second.Position)
        top, err := Add(db, AddInput{ID: "c3", Col: ColTodo, Title: "Now first", Position: intptr(0)})
        require.NoError(t, err)
        assert.Equal(t, 0, top.Position, "an explicit position 0 inserts at the top")

        // The title is trimmed; notes are optional free text.
        blank, err := Add(db, AddInput{Col: ColDoing, Title: "  padded  "})
        require.NoError(t, err)
        assert.Equal(t, "padded", blank.Title)
        assert.Empty(t, blank.Notes)

        cards, err := List(db)
        require.NoError(t, err)
        require.Len(t, cards, 4)
        // List orders by column then position: doing before todo alphabetically
        // is fine — the widget groups by name; what matters is the order WITHIN
        // each column.
        var todo []Card
        for _, c := range cards {
                if c.Col == ColTodo {
                        todo = append(todo, c)
                }
        }
        require.Len(t, todo, 3)
        assert.Equal(t, []string{"c3", "c1", "c2"}, []string{todo[0].ID, todo[1].ID, todo[2].ID},
                "the insert at 0 pushed the others down, densely")

        // An empty board is a valid board: a fresh vault lists nothing.
        empty := newTestVault(t)
        got, err := List(empty.DB())
        require.NoError(t, err)
        assert.Empty(t, got)
}

func TestKanbanMoveEditDelete_Positive(t *testing.T) {
        db := newTestVault(t).DB()
        for i, id := range []string{"a", "b", "c"} {
                _, err := Add(db, AddInput{ID: id, Col: ColTodo, Title: fmt.Sprintf("card %s", id), Position: intptr(i)})
                require.NoError(t, err)
        }

        // Cross-column move: both columns end dense.
        moved, err := Move(db, MoveInput{ID: "b", Col: ColDoing, Position: intptr(0)})
        require.NoError(t, err)
        assert.Equal(t, ColDoing, moved.Col)
        assert.Equal(t, 0, moved.Position)
        cards, err := List(db)
        require.NoError(t, err)
        byID := map[string]Card{}
        for _, c := range cards {
                byID[c.ID] = c
        }
        assert.Equal(t, 0, byID["a"].Position)
        assert.Equal(t, 1, byID["c"].Position, "the source column closed the gap")

        // Within-column reorder: "" column means stay; target 0 hoists to top.
        hoisted, err := Move(db, MoveInput{ID: "c", Position: intptr(0)})
        require.NoError(t, err)
        assert.Equal(t, ColTodo, hoisted.Col)
        assert.Equal(t, 0, hoisted.Position)
        cards, err = List(db)
        require.NoError(t, err)
        byID = map[string]Card{}
        for _, c := range cards {
                byID[c.ID] = c
        }
        assert.Equal(t, 1, byID["a"].Position, "the hoist pushed the old top down")

        // Edit patches only what is given.
        notes := "with notes now"
        title := "card a (edited)"
        edited, err := Edit(db, "a", EditInput{Title: &title, Notes: &notes})
        require.NoError(t, err)
        assert.Equal(t, title, edited.Title)
        assert.Equal(t, notes, edited.Notes)
        assert.Equal(t, 1, edited.Position, "edit never moves")

        // A non-nil empty notes clears; title nil keeps the stored title.
        cleared, err := Edit(db, "a", EditInput{Notes: new(string)})
        require.NoError(t, err)
        assert.Empty(t, cleared.Notes)
        assert.Equal(t, title, cleared.Title)

        // Delete closes the gap.
        require.NoError(t, Delete(db, "a"))
        cards, err = List(db)
        require.NoError(t, err)
        require.Len(t, cards, 2)
        for _, c := range cards {
                assert.LessOrEqual(t, c.Position, 0, "one card per remaining column, at the top")
        }
}

// ---------- negative ----------

func TestKanbanBadInputs_Negative(t *testing.T) {
        db := newTestVault(t).DB()

        for _, tc := range []struct {
                name    string
                fn      func() error
                wantErr string
        }{
                {"add unknown column", func() error {
                        _, err := Add(db, AddInput{Col: "wip", Title: "x"})
                        return err
                }, `unknown column "wip"`},
                {"add blank title", func() error {
                        _, err := Add(db, AddInput{Col: ColTodo, Title: "   "})
                        return err
                }, "needs a title"},
                {"move unknown id", func() error {
                        _, err := Move(db, MoveInput{ID: "ghost", Col: ColDone})
                        return err
                }, ErrNoCard.Error()},
                {"move unknown column", func() error {
                        _, err := Move(db, MoveInput{ID: "x", Col: "backlog"})
                        return err
                }, `unknown column "backlog"`},
                {"move without id", func() error {
                        _, err := Move(db, MoveInput{Col: ColDone})
                        return err
                }, "requires a card id"},
                {"edit unknown id", func() error {
                        s := "t"
                        _, err := Edit(db, "ghost", EditInput{Title: &s})
                        return err
                }, ErrNoCard.Error()},
                {"edit changes nothing", func() error {
                        _, err := Edit(db, "x", EditInput{})
                        return err
                }, "changes nothing"},
                {"edit blank title", func() error {
                        s := "  "
                        _, err := Edit(db, "x", EditInput{Title: &s})
                        return err
                }, "needs a title"},
                {"delete unknown id", func() error {
                        return Delete(db, "ghost")
                }, ErrNoCard.Error()},
                {"delete without id", func() error {
                        return Delete(db, " ")
                }, "requires a card id"},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        err := tc.fn()
                        require.Error(t, err)
                        assert.Contains(t, err.Error(), tc.wantErr)
                })
        }

        // Validation errors leave the board untouched — nothing was created
        // as a side effect of the rejected calls above.
        cards, err := List(db)
        require.NoError(t, err)
        assert.Empty(t, cards)
}

func TestKanbanNilVault_Negative(t *testing.T) {
        // A nil handle is no store, not an empty board — every op says so.
        _, err := List(nil)
        require.ErrorIs(t, err, ErrNoVault)
        _, err = Add(nil, AddInput{Col: ColTodo, Title: "x"})
        require.ErrorIs(t, err, ErrNoVault)
        _, err = Move(nil, MoveInput{ID: "x"})
        require.ErrorIs(t, err, ErrNoVault)
        _, err = Edit(nil, "x", EditInput{Notes: new(string)})
        require.ErrorIs(t, err, ErrNoVault)
        require.ErrorIs(t, Delete(nil, "x"), ErrNoVault)
        // EnsureKanban itself is nil-tolerant (a boot site with no vault
        // skips silently, the EnsureJobRows precedent).
        require.NoError(t, EnsureKanban(nil))
}

func TestKanbanHostilePositions_Negative(t *testing.T) {
        db := newTestVault(t).DB()
        for _, id := range []string{"a", "b", "c"} {
                _, err := Add(db, AddInput{ID: id, Col: ColTodo, Title: id})
                require.NoError(t, err)
        }

        // Absurd target indices clamp to the ends; the board never errors and
        // never corrupts.
        card, err := Add(db, AddInput{ID: "far", Col: ColDoing, Title: "far", Position: intptr(1 << 30)})
        require.NoError(t, err)
        assert.Equal(t, 0, card.Position, "an oversize target clamps to the end of an empty column")
        card, err = Move(db, MoveInput{ID: "far", Col: ColTodo, Position: intptr(-99)})
        require.NoError(t, err)
        assert.Equal(t, 3, card.Position, "a negative target appends")

        // The invariant survived the hostile inputs.
        cards, err := List(db)
        require.NoError(t, err)
        seen := map[string][]int{}
        for _, c := range cards {
                seen[c.Col] = append(seen[c.Col], c.Position)
        }
        assert.Equal(t, []int{0, 1, 2, 3}, seen[ColTodo])
        assert.Empty(t, seen[ColDoing])
}

func TestKanbanDuplicateID_Negative(t *testing.T) {
        db := newTestVault(t).DB()
        _, err := Add(db, AddInput{ID: "dup", Col: ColTodo, Title: "first"})
        require.NoError(t, err)
        _, err = Add(db, AddInput{ID: "dup", Col: ColDoing, Title: "second", Position: intptr(0)})
        require.Error(t, err, "the PK rejects the second card")
        assert.Contains(t, err.Error(), "UNIQUE constraint failed: kanban_cards.id")

        // And the rejected add left nothing behind: no card in doing, and the
        // todo column is exactly the first card.
        cards, err := List(db)
        require.NoError(t, err)
        require.Len(t, cards, 1)
        assert.Equal(t, ColTodo, cards[0].Col)
        assert.Equal(t, 0, cards[0].Position)
}

// ---------- differential ----------

// boardModel is the naive oracle: plain Go slices per column, re-sorted on
// every op by the same intent the store promises. It shares no code with
// the SQL — a store bug (a missed shift, an off-by-one clamp) shows up as
// a divergence.
type boardModel struct {
        cols map[string][]string // col -> card ids in board order
        todo map[string]string   // id -> title, for content assertions
}

func newBoardModel() *boardModel {
        return &boardModel{cols: map[string][]string{}, todo: map[string]string{}}
}

func (m *boardModel) add(id, col, title string, target int) {
        list := m.cols[col]
        if target < 0 || target > len(list) {
                target = len(list)
        }
        list = append(list, "")
        copy(list[target+1:], list[target:])
        list[target] = id
        m.cols[col] = list
        m.todo[id] = title
}

func (m *boardModel) move(id, col string, target int) {
        src := ""
        srcPos := -1
        for c, list := range m.cols {
                for i, x := range list {
                        if x == id {
                                src, srcPos = c, i
                        }
                }
        }
        if src == "" {
                return
        }
        if col == "" {
                col = src
        }
        list := m.cols[src]
        list = append(list[:srcPos], list[srcPos+1:]...)
        m.cols[src] = list
        dest := m.cols[col]
        others := len(dest)
        if target < 0 || target > others {
                target = others
        }
        dest = append(dest, "")
        copy(dest[target+1:], dest[target:])
        dest[target] = id
        m.cols[col] = dest
}

func (m *boardModel) remove(id string) {
        for c, list := range m.cols {
                for i, x := range list {
                        if x == id {
                                m.cols[c] = append(list[:i], list[i+1:]...)
                                return
                        }
                }
        }
}

// TestKanbanMatchesNaiveModel_Differential: a seeded-random op sequence
// through the real store, cross-checked after EVERY op against the naive
// model's order — and the density invariant asserted on the SQL side, so
// the oracle catches both wrong order and non-dense positions.
func TestKanbanMatchesNaiveModel_Differential(t *testing.T) {
        db := newTestVault(t).DB()
        model := newBoardModel()
        rng := rand.New(rand.NewSource(42)) // deterministic sequence
        var ids []string

        // check compares the store's truth with the model's.
        check := func(step string) {
                t.Helper()
                cards, err := List(db)
                require.NoError(t, err, "%s", step)
                got := map[string][]string{}
                for _, col := range Columns {
                        got[col] = []string{} // nil vs empty must not fail the comparison
                }
                for _, c := range cards {
                        require.Equal(t, len(got[c.Col]), c.Position,
                                "%s: %s position %d is dense (0..n-1), got col state %v", step, c.ID, c.Position, got[c.Col])
                        got[c.Col] = append(got[c.Col], c.ID)
                        assert.Equal(t, model.todo[c.ID], c.Title, "%s: content follows the model", step)
                }
                for _, col := range Columns {
                        want := model.cols[col]
                        if want == nil {
                                want = []string{} // the model's untouched columns are nil
                        }
                        assert.Equal(t, want, got[col],
                                "%s: column %s order matches the naive model", step, col)
                }
        }

        for step := 0; step < 200; step++ {
                op := rng.Intn(10)
                switch {
                case op < 3: // add
                        id := fmt.Sprintf("card-%d", step)
                        col := Columns[rng.Intn(len(Columns))]
                        target := intptr(rng.Intn(8) - 2) // includes the hostile negatives
                        _, err := Add(db, AddInput{ID: id, Col: col, Title: id, Position: target})
                        require.NoError(t, err)
                        model.add(id, col, id, *target)
                        ids = append(ids, id)
                case op < 7 && len(ids) > 0: // move
                        id := ids[rng.Intn(len(ids))]
                        col := ""
                        if rng.Intn(2) == 0 {
                                col = Columns[rng.Intn(len(Columns))]
                        }
                        target := intptr(rng.Intn(8) - 2)
                        _, err := Move(db, MoveInput{ID: id, Col: col, Position: target})
                        require.NoError(t, err)
                        model.move(id, col, *target)
                case op < 9 && len(ids) > 0: // edit (content parity)
                        id := ids[rng.Intn(len(ids))]
                        title := fmt.Sprintf("%s-rev%d", id, step)
                        _, err := Edit(db, id, EditInput{Title: &title})
                        require.NoError(t, err)
                        model.todo[id] = title
                default: // delete
                        if len(ids) == 0 {
                                continue
                        }
                        i := rng.Intn(len(ids))
                        id := ids[i]
                        ids = append(ids[:i], ids[i+1:]...)
                        require.NoError(t, Delete(db, id))
                        model.remove(id)
                }
                check(fmt.Sprintf("step %d (op %d)", step, op))
        }

        // The final board is non-trivial: the sequence must have exercised real
        // state, or the differential proved nothing.
        final, err := List(db)
        require.NoError(t, err)
        require.NotEmpty(t, final, "the seeded sequence left cards on the board")
}

// TestKanbanEnsureIdempotent_Differential: EnsureKanban twice is the same
// database — the lazy-ensure contract every op depends on. The "reference"
// here is the first ensure's own List result re-read after the second.
func TestKanbanEnsureIdempotent_Differential(t *testing.T) {
        root := t.TempDir()
        v1, err := vault.Open(root)
        require.NoError(t, err)
        db := v1.DB()
        _, err = Add(db, AddInput{ID: "keep", Col: ColDone, Title: "survivor"})
        require.NoError(t, err)
        before, err := List(db)
        require.NoError(t, err)
        require.NoError(t, v1.Close())

        // A second vault over the same file re-ensures and still reads the card.
        v2, err := vault.Open(root)
        require.NoError(t, err)
        defer v2.Close()
        require.NoError(t, EnsureKanban(v2.DB()))
        after, err := List(v2.DB())
        require.NoError(t, err)
        assert.Equal(t, before, after, "re-opening + re-ensuring changes nothing")

        // And the db file sits exactly where the vault put it — subsystems own
        // rows, never files.
        assert.FileExists(t, filepath.Join(root, "a1.db"))
}
