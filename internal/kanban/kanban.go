// Package kanban is the personal kanban board's store (dashboard chunk 3):
// one vault-owned table, three columns, positions kept dense. The board is
// deliberately NOT a config surface — there is no kanban_dir — it lives in
// a1.db beside the sessions and tool runs, created lazily by this package
// (the jobs-table pattern: subsystems own their DDL, the vault is never
// hand-migrated).
//
// The position contract, pinned by the trio's oracle: after every op, each
// column's cards sit at dense indices 0..n-1 in board order. Callers pass a
// TARGET INDEX — where the card should land among the destination column's
// cards — and the store clamps it into range; a negative index appends.
// Move renumbers source and destination in one transaction, so a crashed
// move can never leave a hole behind.
package kanban

import (
        "crypto/rand"
        "database/sql"
        "encoding/hex"
        "errors"
        "fmt"
        "strings"
        "time"
)

// The three columns, in board order. The set is the product ruling
// (todo/doing/done); the DDL's CHECK constraint mirrors it as a backstop,
// but every op validates in Go first so callers see friendly errors, never
// SQLite's.
const (
        ColTodo  = "todo"
        ColDoing = "doing"
        ColDone  = "done"
)

// Columns is the board order the widget renders.
var Columns = []string{ColTodo, ColDoing, ColDone}

// ValidColumn reports whether col is one of the three board columns.
func ValidColumn(col string) bool {
        switch col {
        case ColTodo, ColDoing, ColDone:
                return true
        }
        return false
}

var (
        // ErrNoVault: the ops need a vault-backed handle. A nil db is not an
        // empty board — that would tell the reader "no cards" when the truth
        // is "no store" — so it errors, and the surfaces branch on it.
        ErrNoVault = errors.New("kanban: no vault is available (the board lives in a1.db)")
        // ErrNoCard: the id does not exist (wrapped, with the id, by the ops).
        ErrNoCard = errors.New("kanban: no such card")
)

// kanbanDDL creates the kanban_cards table lazily on the caller's handle —
// the vault pattern for subsystem-owned rows (see jobsDDL). No UNIQUE
// constraint on (col, position): renumbering shifts ranges through
// transient overlaps inside a transaction, and density is the ops' own
// invariant, pinned by the oracle differential — not the database's.
const kanbanDDL = `
CREATE TABLE IF NOT EXISTS kanban_cards (
  id         TEXT PRIMARY KEY,
  col        TEXT NOT NULL CHECK (col IN ('todo','doing','done')),
  position   INTEGER NOT NULL,
  title      TEXT NOT NULL,
  notes      TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kanban_cards_col ON kanban_cards(col, position);
`

// EnsureKanban creates the kanban_cards table on the handle when missing.
// Nil-tolerant like EnsureJobRows (a boot site with no vault skips); every
// op also ensures, so any entry path — web handler, agent tool, tests —
// works against a fresh vault without boot-order coupling. The idempotent
// no-op costs one parsed statement per call.
func EnsureKanban(db *sql.DB) error {
        if db == nil {
                return nil
        }
        if _, err := db.Exec(kanbanDDL); err != nil {
                return fmt.Errorf("kanban: ensure: %w", err)
        }
        return nil
}

// Card is one board card. Position is the card's dense index within its
// column (0 = top).
type Card struct {
        ID        string
        Col       string
        Position  int
        Title     string
        Notes     string
        CreatedAt time.Time
        UpdatedAt time.Time
}

// AddInput is an Add. Position is the target index among the column's
// cards (0 = top, clamped to the end); nil — the zero value — appends,
// which is what every "+ add card" affordance wants. ID is optional —
// set it only for deterministic fixtures or imports; "" mints one.
type AddInput struct {
        ID       string
        Col      string
        Title    string
        Notes    string
        Position *int
}

// MoveInput is a Move; Position is the card's target index among the
// DESTINATION column's cards (after the move — drop-before-card-i lands at
// i, so drag & drop maps 1:1). nil appends. Col is optional: "" keeps
// the card's current column (a within-column reorder).
type MoveInput struct {
        ID       string
        Col      string
        Position *int
}

// EditInput patches a card's text. Nil fields are untouched; a non-nil
// empty Notes clears the notes (the calendar patch semantics — presence,
// not emptiness, carries intent).
type EditInput struct {
        Title *string
        Notes *string
}

// IsEmpty reports an edit that changes nothing.
func (e EditInput) IsEmpty() bool { return e.Title == nil && e.Notes == nil }

// List returns every card ordered by column then position. An empty board
// is a valid board (nil slice, no error).
func List(db *sql.DB) ([]Card, error) {
        if db == nil {
                return nil, ErrNoVault
        }
        if err := EnsureKanban(db); err != nil {
                return nil, err
        }
        rows, err := db.Query(`SELECT id, col, position, title, notes, created_at, updated_at
                FROM kanban_cards ORDER BY col, position`)
        if err != nil {
                return nil, fmt.Errorf("kanban: list: %w", err)
        }
        defer rows.Close()
        var out []Card
        for rows.Next() {
                c, err := scanCard(rows)
                if err != nil {
                        return nil, err
                }
                out = append(out, c)
        }
        return out, rows.Err()
}

// Add inserts a card at the target index of its column, making room by
// shifting the followers, all in one transaction.
func Add(db *sql.DB, in AddInput) (Card, error) {
        if db == nil {
                return Card{}, ErrNoVault
        }
        col, err := requireColumn(in.Col)
        if err != nil {
                return Card{}, err
        }
        title := strings.TrimSpace(in.Title)
        if title == "" {
                return Card{}, errors.New("kanban: a card needs a title")
        }
        notes := strings.TrimSpace(in.Notes)
        if err := EnsureKanban(db); err != nil {
                return Card{}, err
        }
        id := strings.TrimSpace(in.ID)
        if id == "" {
                id = mintID()
        }
        now := time.Now().UTC()
        tx, err := db.Begin()
        if err != nil {
                return Card{}, fmt.Errorf("kanban: add: %w", err)
        }
        defer tx.Rollback() // no-op after commit

        var count int
        if err := tx.QueryRow(`SELECT COUNT(*) FROM kanban_cards WHERE col = ?`, col).Scan(&count); err != nil {
                return Card{}, fmt.Errorf("kanban: add: %w", err)
        }
        pos := clampIndex(in.Position, count)
        if pos < count {
                if _, err := tx.Exec(`UPDATE kanban_cards SET position = position + 1 WHERE col = ? AND position >= ?`, col, pos); err != nil {
                        return Card{}, fmt.Errorf("kanban: add: %w", err)
                }
        }
        if _, err := tx.Exec(`INSERT INTO kanban_cards (id, col, position, title, notes, created_at, updated_at)
                VALUES (?,?,?,?,?,?,?)`,
                id, col, pos, title, notes, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
                // A pre-set ID colliding with an existing card lands here — the PK
                // rejects it and the shift above rolls back with the transaction.
                return Card{}, fmt.Errorf("kanban: add: %w", err)
        }
        if err := tx.Commit(); err != nil {
                return Card{}, fmt.Errorf("kanban: add: %w", err)
        }
        return Card{
                ID:        id,
                Col:       col,
                Position:  pos,
                Title:     title,
                Notes:     notes,
                CreatedAt: now,
                UpdatedAt: now,
        }, nil
}

// Move relocates a card — across columns or within one — renumbering both
// the source and the destination in a single transaction.
func Move(db *sql.DB, in MoveInput) (Card, error) {
        if db == nil {
                return Card{}, ErrNoVault
        }
        dest := strings.TrimSpace(in.Col)
        if dest != "" && !ValidColumn(dest) {
                return Card{}, fmt.Errorf("kanban: unknown column %q (want todo, doing, or done)", dest)
        }
        id := strings.TrimSpace(in.ID)
        if id == "" {
                return Card{}, errors.New("kanban: move requires a card id")
        }
        if err := EnsureKanban(db); err != nil {
                return Card{}, err
        }
        tx, err := db.Begin()
        if err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        defer tx.Rollback()

        var srcCol string
        var srcPos int
        if err := tx.QueryRow(`SELECT col, position FROM kanban_cards WHERE id = ?`, id).Scan(&srcCol, &srcPos); err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return Card{}, fmt.Errorf("kanban: move: %q: %w", id, ErrNoCard)
                }
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        if dest == "" {
                dest = srcCol // a within-column reorder
        }

        // Remove from the source ordering: the followers close the gap. The
        // mover keeps its stale position until the final UPDATE — a transient
        // overlap the transaction hides (and the reason there is no UNIQUE
        // constraint on col+position).
        if _, err := tx.Exec(`UPDATE kanban_cards SET position = position - 1 WHERE col = ? AND position > ?`, srcCol, srcPos); err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }

        // The destination count INCLUDES the mover when the columns are the
        // same — the mover's final index ranges over the OTHER cards.
        var stored int
        if err := tx.QueryRow(`SELECT COUNT(*) FROM kanban_cards WHERE col = ?`, dest).Scan(&stored); err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        others := stored
        if dest == srcCol {
                others--
        }
        pos := clampIndex(in.Position, others)
        // Make room among the others — never touching the mover itself, whose
        // stale position may already sit inside the shifted range.
        if _, err := tx.Exec(`UPDATE kanban_cards SET position = position + 1 WHERE col = ? AND position >= ? AND id != ?`, dest, pos, id); err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        if _, err := tx.Exec(`UPDATE kanban_cards SET col = ?, position = ?, updated_at = ? WHERE id = ?`,
                dest, pos, time.Now().UTC().Format(time.RFC3339), id); err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        if err := tx.Commit(); err != nil {
                return Card{}, fmt.Errorf("kanban: move: %w", err)
        }
        return getCard(db, id)
}

// Edit patches a card's title and/or notes (nil = untouched), bumping
// updated_at. Moving is Move's job.
func Edit(db *sql.DB, id string, in EditInput) (Card, error) {
        if db == nil {
                return Card{}, ErrNoVault
        }
        id = strings.TrimSpace(id)
        if id == "" {
                return Card{}, errors.New("kanban: edit requires a card id")
        }
        if in.IsEmpty() {
                return Card{}, errors.New("kanban: edit changes nothing (pass title and/or notes)")
        }
        // The stored values are the TRIMMED ones; nil fields pass SQL NULL and
        // COALESCE keeps the stored value.
        var titleArg, notesArg any
        if in.Title != nil {
                title := strings.TrimSpace(*in.Title)
                if title == "" {
                        return Card{}, errors.New("kanban: a card needs a title")
                }
                titleArg = title
        }
        if in.Notes != nil {
                notesArg = strings.TrimSpace(*in.Notes)
        }
        if err := EnsureKanban(db); err != nil {
                return Card{}, err
        }
        res, err := db.Exec(`UPDATE kanban_cards
                SET title = COALESCE(?, title), notes = COALESCE(?, notes), updated_at = ?
                WHERE id = ?`,
                titleArg, notesArg, time.Now().UTC().Format(time.RFC3339), id)
        if err != nil {
                return Card{}, fmt.Errorf("kanban: edit: %w", err)
        }
        n, err := res.RowsAffected()
        if err != nil {
                return Card{}, fmt.Errorf("kanban: edit: %w", err)
        }
        if n == 0 {
                return Card{}, fmt.Errorf("kanban: edit: %q: %w", id, ErrNoCard)
        }
        return getCard(db, id)
}

// Delete removes a card and closes the gap behind it.
func Delete(db *sql.DB, id string) error {
        if db == nil {
                return ErrNoVault
        }
        id = strings.TrimSpace(id)
        if id == "" {
                return errors.New("kanban: delete requires a card id")
        }
        if err := EnsureKanban(db); err != nil {
                return err
        }
        tx, err := db.Begin()
        if err != nil {
                return fmt.Errorf("kanban: delete: %w", err)
        }
        defer tx.Rollback()

        var col string
        var pos int
        if err := tx.QueryRow(`SELECT col, position FROM kanban_cards WHERE id = ?`, id).Scan(&col, &pos); err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return fmt.Errorf("kanban: delete: %q: %w", id, ErrNoCard)
                }
                return fmt.Errorf("kanban: delete: %w", err)
        }
        if _, err := tx.Exec(`DELETE FROM kanban_cards WHERE id = ?`, id); err != nil {
                return fmt.Errorf("kanban: delete: %w", err)
        }
        if _, err := tx.Exec(`UPDATE kanban_cards SET position = position - 1 WHERE col = ? AND position > ?`, col, pos); err != nil {
                return fmt.Errorf("kanban: delete: %w", err)
        }
        return tx.Commit()
}

// ---------- internals ----------

// requireColumn validates and normalizes a column name.
func requireColumn(col string) (string, error) {
        col = strings.TrimSpace(col)
        if !ValidColumn(col) {
                return "", fmt.Errorf("kanban: unknown column %q (want todo, doing, or done)", col)
        }
        return col, nil
}

// clampIndex bounds a target index to [0, max]; nil (or out of range)
// appends (max). The nil case is the zero value, so a caller that never
// thinks about position gets the natural append.
func clampIndex(target *int, max int) int {
        if target == nil || *target < 0 || *target > max {
                return max
        }
        return *target
}

// mintID mints a card id: timestamp + entropy, the calendar uid's shape.
func mintID() string {
        var b [8]byte
        if _, err := rand.Read(b[:]); err != nil {
                // crypto/rand failing means the OS entropy pool is gone; the
                // timestamp alone is still unique within this process.
                return fmt.Sprintf("kb-%d", time.Now().UnixNano())
        }
        return fmt.Sprintf("kb-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b[:]))
}

// getCard re-reads one card after a mutation, so every write returns the
// stored truth rather than the caller's input.
func getCard(db *sql.DB, id string) (Card, error) {
        row := db.QueryRow(`SELECT id, col, position, title, notes, created_at, updated_at FROM kanban_cards WHERE id = ?`, id)
        c, err := scanCard(row)
        if err != nil {
                return Card{}, fmt.Errorf("kanban: read back %q: %w", id, err)
        }
        return c, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanCard(sc scanner) (Card, error) {
        var c Card
        var created, updated string
        if err := sc.Scan(&c.ID, &c.Col, &c.Position, &c.Title, &c.Notes, &created, &updated); err != nil {
                if errors.Is(err, sql.ErrNoRows) {
                        return Card{}, sql.ErrNoRows
                }
                return Card{}, fmt.Errorf("kanban: scan card: %w", err)
        }
        c.CreatedAt = parseTs(created)
        c.UpdatedAt = parseTs(updated)
        return c, nil
}

func parseTs(s string) time.Time {
        if s == "" {
                return time.Time{}
        }
        t, err := time.Parse(time.RFC3339, s)
        if err != nil {
                return time.Time{}
        }
        return t
}
