package webserver

import (
        "encoding/json"
        "io"
        "net/http"
        "net/http/httptest"
        "strings"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "github.com/damien1141/a1/internal/kanban"
        "github.com/damien1141/a1/internal/session"
        "github.com/damien1141/a1/internal/vault"
)

// The kanban endpoints' trio (dashboard chunk 3). The store's own trio —
// the position-invariant oracle — lives in internal/kanban; this file
// pins the HTTP layer: the gate + guard composition, the hostile-input
// refusals, the honest no-vault 503, and endpoint-vs-library parity over
// an identical mutation sequence.
//
// The fixture servers boot a real vault (newServer installs it as the
// process default), so the handlers' session.DefaultVault() resolves the
// test's own a1.db — the same handle the library side of the differential
// reads.

// doKanban fires one request at the kanban API; a 200 body decodes into
// dst when non-nil. No Origin header — same-origin, the guard's pass path.
func doKanban(t *testing.T, srv *Server, method, path, body string, dst any) int {
        t.Helper()
        var rd io.Reader
        if body != "" {
                rd = strings.NewReader(body)
        }
        req := httptest.NewRequest(method, path, rd)
        if body != "" {
                req.Header.Set("Content-Type", "application/json")
        }
        rec := httptest.NewRecorder()
        srv.routes().ServeHTTP(rec, req)
        if dst != nil && rec.Code == http.StatusOK {
                require.NoError(t, json.Unmarshal(rec.Body.Bytes(), dst), "decode body: %s", rec.Body.String())
        }
        return rec.Code
}

// kanbanState is the board's semantic content — everything but the ids and
// timestamps — which is what the endpoint-vs-library differential compares:
// ids are minted per side and are identity, not state.
type kanbanState struct {
        Col      string
        Position int
        Title    string
        Notes    string
}

func boardState(cards []kanbanCardJSON) []kanbanState {
        out := make([]kanbanState, 0, len(cards))
        for _, c := range cards {
                out = append(out, kanbanState{Col: c.Col, Position: c.Position, Title: c.Title, Notes: c.Notes})
        }
        return out
}

func listState(cards []kanban.Card) []kanbanState {
        out := make([]kanbanState, 0, len(cards))
        for _, c := range cards {
                out = append(out, kanbanState{Col: c.Col, Position: c.Position, Title: c.Title, Notes: c.Notes})
        }
        return out
}

// ---------- positive ----------

func TestKanbanCRUDRoundTrip_Positive(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")

        // Add: col defaults to todo, an absent position appends.
        var first kanbanCardResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"first","notes":"the backlog"}`, &first))
        assert.Equal(t, kanban.ColTodo, first.Card.Col)
        assert.Equal(t, 0, first.Card.Position)
        assert.NotEmpty(t, first.Card.ID)
        assert.NotEmpty(t, first.Card.CreatedAt, "the stored truth comes back, not the caller's input")

        var second kanbanCardResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"second"}`, &second))
        assert.Equal(t, 1, second.Card.Position, "appended after the first")

        var board kanbanBoardJSON
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodGet, "/api/personal/kanban", "", &board))
        assert.Equal(t, []string{kanban.ColTodo, kanban.ColDoing, kanban.ColDone}, board.Columns,
                "the product ruling's three columns, in board order")
        require.Len(t, board.Cards, 2)

        // Within-column reorder: col absent keeps the column. Each PUT
        // decodes into a FRESH value: the response omits empty notes
        // (omitempty), and unmarshal leaves absent fields at their stale
        // values — reusing the struct would smuggle the add-time notes
        // into the edit's response.
        var moved kanbanCardResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+second.Card.ID+`","position":0}`, &moved))
        assert.Equal(t, 0, moved.Card.Position)

        // Hostile positions clamp, never error — a widget's drop index is
        // not a place to teach anyone a lesson.
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+second.Card.ID+`","position":9999}`, &moved))
        assert.Equal(t, 1, moved.Card.Position, "an oversize target clamps to the end")
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+second.Card.ID+`","position":-7}`, &moved))
        assert.Equal(t, 1, moved.Card.Position, "a negative target appends too")

        // Edit: title patched, notes cleared by presence, not emptiness.
        var edited kanbanCardResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+first.Card.ID+`","title":"first!","notes":""}`, &edited))
        assert.Equal(t, "first!", edited.Card.Title)
        assert.Empty(t, edited.Card.Notes, "an empty notes string clears")

        // Cross-column move into a specific slot.
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+first.Card.ID+`","col":"done","position":0}`, &moved))
        assert.Equal(t, kanban.ColDone, moved.Card.Col)

        // Delete: gone, and the column behind it stays dense.
        var del kanbanDeleteResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodDelete,
                "/api/personal/kanban/card?id="+first.Card.ID, "", &del))
        assert.True(t, del.Deleted)
        assert.Equal(t, first.Card.ID, del.ID)
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodGet, "/api/personal/kanban", "", &board))
        require.Len(t, board.Cards, 1)
        assert.Equal(t, kanban.ColTodo, board.Cards[0].Col)
        assert.Equal(t, 0, board.Cards[0].Position, "the survivor closes the gap")
}

// ---------- negative ----------

func TestKanbanGatedOff_Negative(t *testing.T) {
        // The master switch is enforced server-side: every route 403s
        // before any handler (or the vault) is touched.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: false\n")
        for _, tc := range []struct{ method, path, body string }{
                {http.MethodGet, "/api/personal/kanban", ""},
                {http.MethodPost, "/api/personal/kanban", `{"title":"nope"}`},
                {http.MethodPut, "/api/personal/kanban/card", `{"id":"x","col":"done"}`},
                {http.MethodDelete, "/api/personal/kanban/card?id=x", ""},
        } {
                assert.Equal(t, http.StatusForbidden, doKanban(t, srv, tc.method, tc.path, tc.body, nil),
                        "%s %s", tc.method, tc.path)
        }
}

func TestKanbanCrossOriginWriteRefused_Negative(t *testing.T) {
        // The mutating verbs compose guard(): a cross-origin page must not
        // be able to rearrange the board. The gate passes (enabled) — this
        // pins the guard's composition, not the gate's.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")
        for _, tc := range []struct{ method, path, body string }{
                {http.MethodPost, "/api/personal/kanban", `{"title":"x"}`},
                {http.MethodPut, "/api/personal/kanban/card", `{"id":"x","col":"done"}`},
                {http.MethodDelete, "/api/personal/kanban/card?id=x", ""},
        } {
                req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
                req.Header.Set("Origin", "http://evil.example")
                rec := httptest.NewRecorder()
                srv.routes().ServeHTTP(rec, req)
                assert.Equal(t, http.StatusForbidden, rec.Code, "%s is guarded", tc.method)
                assert.Contains(t, rec.Body.String(), "origin", "%s says why", tc.method)
        }
        var board kanbanBoardJSON
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodGet, "/api/personal/kanban", "", &board))
        assert.Empty(t, board.Cards, "every refused write left the board untouched")
}

func TestKanbanHostileInputs_Negative(t *testing.T) {
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")

        for _, tc := range []struct{ name, method, path, body string }{
                {"bad json", http.MethodPost, "/api/personal/kanban", `{"title":`},
                {"missing title", http.MethodPost, "/api/personal/kanban", `{"notes":"no title"}`},
                {"blank title", http.MethodPost, "/api/personal/kanban", `{"title":"   "}`},
                {"unknown column", http.MethodPost, "/api/personal/kanban", `{"title":"x","col":"later"}`},
                {"missing id on put", http.MethodPut, "/api/personal/kanban/card", `{"col":"done"}`},
                {"no-op put", http.MethodPut, "/api/personal/kanban/card", `{"id":"kb-x"}`},
                {"unknown column on put", http.MethodPut, "/api/personal/kanban/card", `{"id":"kb-x","col":"somewhere"}`},
                {"empty title on put", http.MethodPut, "/api/personal/kanban/card", `{"id":"kb-x","title":""}`},
                {"missing id on delete", http.MethodDelete, "/api/personal/kanban/card", ""},
                {"unknown id on put", http.MethodPut, "/api/personal/kanban/card", `{"id":"kb-nobody","col":"done"}`},
                {"unknown id on delete", http.MethodDelete, "/api/personal/kanban/card?id=kb-nobody", ""},
        } {
                t.Run(tc.name, func(t *testing.T) {
                        want := http.StatusBadRequest
                        if strings.Contains(tc.name, "unknown id") {
                                want = http.StatusNotFound
                        }
                        assert.Equal(t, want, doKanban(t, srv, tc.method, tc.path, tc.body, nil))
                })
        }

        // The 64KB body cap: a body that is VALID JSON past the cap must
        // still fail to decode — truncated by the limit, not by syntax.
        pad := strings.Repeat("a", 70<<10)
        assert.Equal(t, http.StatusBadRequest,
                doKanban(t, srv, http.MethodPost, "/api/personal/kanban", `{"title":"`+pad+`"}`, nil),
                "an oversized body is refused, not parsed")

        // Nothing above created or moved a card.
        var board kanbanBoardJSON
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodGet, "/api/personal/kanban", "", &board))
        assert.Empty(t, board.Cards, "every refused write left the board untouched")
}

func TestKanbanNoVaultIsHonest_Negative(t *testing.T) {
        // A server whose vault boot degraded answers 503 — never a
        // fabricated empty board, which is a valid state worth protecting.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")
        prev := session.DefaultVault()
        session.SetDefaultVault(nil)
        t.Cleanup(func() { session.SetDefaultVault(prev) })

        for _, tc := range []struct{ method, path, body string }{
                {http.MethodGet, "/api/personal/kanban", ""},
                {http.MethodPost, "/api/personal/kanban", `{"title":"x"}`},
                {http.MethodPut, "/api/personal/kanban/card", `{"id":"x","col":"done"}`},
                {http.MethodDelete, "/api/personal/kanban/card?id=x", ""},
        } {
                var rd io.Reader
                if tc.body != "" {
                        rd = strings.NewReader(tc.body)
                }
                rec := httptest.NewRecorder()
                srv.routes().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, rd))
                require.Equal(t, http.StatusServiceUnavailable, rec.Code, "%s %s", tc.method, tc.path)
                assert.Contains(t, rec.Body.String(), "no vault", "the refusal says why")
        }
}

// ---------- differential ----------

// TestKanbanEndpointMatchesLibrary_Differential: the same mutation
// sequence driven through the HTTP endpoints and through the library must
// land on the same board — col, position, title, notes, compared
// card-for-card in list order — and both must equal the hand-computed
// expectation. All phase-A traffic completes before the phase-B vault is
// installed: the handlers resolve the process-wide default per request.
func TestKanbanEndpointMatchesLibrary_Differential(t *testing.T) {
        // Phase A: the endpoints.
        srv := newPersonalFixtureProject(t, "personal:\n  enabled: true\n")

        var card kanbanCardResponse
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"alpha","notes":"a"}`, &card))
        alpha := card.Card.ID
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"beta"}`, &card))
        beta := card.Card.ID
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"gamma","col":"doing"}`, &card))
        gamma := card.Card.ID
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPost, "/api/personal/kanban",
                `{"title":"delta","position":0}`, &card))

        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+beta+`","col":"done"}`, &card))
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+alpha+`","position":0}`, &card))
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+gamma+`","title":"gamma II","notes":"n"}`, &card))
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+gamma+`","col":"done","position":0}`, &card))
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodDelete,
                "/api/personal/kanban/card?id="+alpha, "", nil))
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodPut, "/api/personal/kanban/card",
                `{"id":"`+beta+`","notes":""}`, &card))

        var viaEndpoint kanbanBoardJSON
        require.Equal(t, http.StatusOK, doKanban(t, srv, http.MethodGet, "/api/personal/kanban", "", &viaEndpoint))

        // Phase B: the library, same sequence, on a fresh vault.
        vB, err := vault.Open(t.TempDir())
        require.NoError(t, err)
        prev := session.DefaultVault()
        session.SetDefaultVault(vB)
        t.Cleanup(func() { session.SetDefaultVault(prev) })
        db := vB.DB()

        top := 0
        strp := func(s string) *string { return &s }
        a, err := kanban.Add(db, kanban.AddInput{Col: kanban.ColTodo, Title: "alpha", Notes: "a"})
        require.NoError(t, err)
        b, err := kanban.Add(db, kanban.AddInput{Col: kanban.ColTodo, Title: "beta"})
        require.NoError(t, err)
        g, err := kanban.Add(db, kanban.AddInput{Col: kanban.ColDoing, Title: "gamma"})
        require.NoError(t, err)
        _, err = kanban.Add(db, kanban.AddInput{Col: kanban.ColTodo, Title: "delta", Position: &top})
        require.NoError(t, err)
        _, err = kanban.Move(db, kanban.MoveInput{ID: b.ID, Col: kanban.ColDone})
        require.NoError(t, err)
        _, err = kanban.Move(db, kanban.MoveInput{ID: a.ID, Position: &top})
        require.NoError(t, err)
        _, err = kanban.Edit(db, g.ID, kanban.EditInput{Title: strp("gamma II"), Notes: strp("n")})
        require.NoError(t, err)
        _, err = kanban.Move(db, kanban.MoveInput{ID: g.ID, Col: kanban.ColDone, Position: &top})
        require.NoError(t, err)
        require.NoError(t, kanban.Delete(db, a.ID))
        _, err = kanban.Edit(db, b.ID, kanban.EditInput{Notes: strp("")})
        require.NoError(t, err)

        viaLibrary, err := kanban.List(db)
        require.NoError(t, err)

        // The two boards agree on state, card for card, in list order.
        assert.Equal(t, listState(viaLibrary), boardState(viaEndpoint.Cards),
                "endpoint and library land on the same board")

        // And both equal the hand-computed sequence: done holds gamma II
        // (notes kept) above beta (notes cleared), doing is empty, todo
        // holds delta alone — list order is (col, position).
        want := []kanbanState{
                {Col: kanban.ColDone, Position: 0, Title: "gamma II", Notes: "n"},
                {Col: kanban.ColDone, Position: 1, Title: "beta"},
                {Col: kanban.ColTodo, Position: 0, Title: "delta"},
        }
        assert.Equal(t, want, boardState(viaEndpoint.Cards))
        assert.Equal(t, want, listState(viaLibrary))
}
