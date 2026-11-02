package vault

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"
)

// BankEntry is one memory-bank row (the JSONL Entry shape, stored as
// columns). Timestamps keep RFC3339Nano precision.
type BankEntry struct {
	Seq       int64
	Type      string
	Content   string
	Timestamp time.Time
	SessionID string
}

// AppendBankEntry appends one entry to a bank, assigning the next seq.
func (v *Vault) AppendBankEntry(bank string, e BankEntry) error {
	if bank == "" {
		return fmt.Errorf("vault: bank key is required")
	}
	_, err := v.db.Exec(`INSERT INTO memory_banks (bank, seq, type, content, timestamp, session_id)
                VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM memory_banks WHERE bank = ?), ?, ?, ?, ?)`,
		bank, bank, e.Type, e.Content, e.Timestamp.UTC().Format(time.RFC3339Nano), e.SessionID)
	if err != nil {
		return fmt.Errorf("vault: append bank entry: %w", err)
	}
	return nil
}

// LoadBank returns a bank's entries oldest-first.
func (v *Vault) LoadBank(bank string) ([]BankEntry, error) {
	rows, err := v.db.Query(`SELECT seq, type, content, timestamp, session_id
                FROM memory_banks WHERE bank = ? ORDER BY seq`, bank)
	if err != nil {
		return nil, fmt.Errorf("vault: load bank %s: %w", bank, err)
	}
	defer rows.Close()
	var out []BankEntry
	for rows.Next() {
		var e BankEntry
		var ts string
		if err := rows.Scan(&e.Seq, &e.Type, &e.Content, &ts, &e.SessionID); err != nil {
			return nil, fmt.Errorf("vault: scan bank entry: %w", err)
		}
		e.Timestamp = parseTime(ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ReplaceBank rewrites a bank's entries in one transaction (the
// DeleteAt path: the in-memory list is authoritative, the rows follow).
func (v *Vault) ReplaceBank(bank string, entries []BankEntry) error {
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("vault: replace bank %s: %w", bank, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM memory_banks WHERE bank = ?`, bank); err != nil {
		return fmt.Errorf("vault: clear bank: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO memory_banks (bank, seq, type, content, timestamp, session_id)
                VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("vault: prepare bank insert: %w", err)
	}
	defer stmt.Close()
	for i, e := range entries {
		if _, err := stmt.Exec(bank, int64(i), e.Type, e.Content,
			e.Timestamp.UTC().Format(time.RFC3339Nano), e.SessionID); err != nil {
			return fmt.Errorf("vault: insert bank entry %d: %w", i, err)
		}
	}
	return tx.Commit()
}

// AppendEvidenceReceipt appends one receipt payload under a scope.
func (v *Vault) AppendEvidenceReceipt(scope, payload string) error {
	if scope == "" {
		return fmt.Errorf("vault: evidence scope is required")
	}
	_, err := v.db.Exec(`INSERT INTO evidence_receipts (scope, seq, payload)
                VALUES (?, (SELECT COALESCE(MAX(seq), 0) + 1 FROM evidence_receipts WHERE scope = ?), ?)`,
		scope, scope, payload)
	if err != nil {
		return fmt.Errorf("vault: append evidence receipt: %w", err)
	}
	return nil
}

// LoadEvidenceReceipts returns a scope's receipt payloads in order.
func (v *Vault) LoadEvidenceReceipts(scope string) ([]string, error) {
	rows, err := v.db.Query(`SELECT payload FROM evidence_receipts WHERE scope = ? ORDER BY seq`, scope)
	if err != nil {
		return nil, fmt.Errorf("vault: load evidence receipts: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("vault: scan receipt: %w", err)
		}
		out = append(out, payload)
	}
	return out, rows.Err()
}

// ReplaceEvidenceReceipts rewrites a scope's receipts in one transaction
// (the prune path: minimal replay-equivalent set, same ratchet guarantee
// the JSONL rewrite gave).
func (v *Vault) ReplaceEvidenceReceipts(scope string, payloads []string) error {
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("vault: replace receipts %s: %w", scope, err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM evidence_receipts WHERE scope = ?`, scope); err != nil {
		return fmt.Errorf("vault: clear receipts: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO evidence_receipts (scope, seq, payload) VALUES (?,?,?)`)
	if err != nil {
		return fmt.Errorf("vault: prepare receipt insert: %w", err)
	}
	defer stmt.Close()
	for i, payload := range payloads {
		if _, err := stmt.Exec(scope, int64(i), payload); err != nil {
			return fmt.Errorf("vault: insert receipt %d: %w", i, err)
		}
	}
	return tx.Commit()
}

// DeleteEvidence removes a scope's receipts entirely (session/project
// deletability).
func (v *Vault) DeleteEvidence(scope string) error {
	if _, err := v.db.Exec(`DELETE FROM evidence_receipts WHERE scope = ?`, scope); err != nil {
		return fmt.Errorf("vault: delete evidence %s: %w", scope, err)
	}
	return nil
}

// SessionRow is one persisted session.
type SessionRow struct {
	ID            string
	Project       string
	Ts            string // filename timestamp prefix ("2006-01-02T15-04-05")
	ParentSession string
	Cwd           string
	CreatedAt     time.Time
}

// PutSession inserts one session row (no-op conflict — sessions are
// immutable once created).
func (v *Vault) PutSession(s SessionRow) error {
	_, err := v.db.Exec(`INSERT OR IGNORE INTO sessions (id, project, ts, parent_session, cwd, created_at)
                VALUES (?,?,?,?,?,?)`,
		s.ID, s.Project, s.Ts, s.ParentSession, s.Cwd, s.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("vault: put session %s: %w", s.ID, err)
	}
	return nil
}

// SessionEntryRow is one persisted session entry. Payload is the
// byte-faithful JSON line; Kind is the entry discriminant ("header",
// "message", "compaction") for cheap filtering without decoding.
type SessionEntryRow struct {
	SessionID string
	Seq       int64
	ParentSeq *int64
	Kind      string
	Payload   string
}

// AppendSessionEntries appends entries in one transaction. The session
// row must already exist (the FK cascade enforces the tree).
func (v *Vault) AppendSessionEntries(rows []SessionEntryRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := v.db.Begin()
	if err != nil {
		return fmt.Errorf("vault: append session entries: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT INTO session_entries (session_id, seq, parent_seq, kind, payload)
                VALUES (?,?,?,?,?)`)
	if err != nil {
		return fmt.Errorf("vault: prepare entry insert: %w", err)
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.SessionID, r.Seq, r.ParentSeq, r.Kind, r.Payload); err != nil {
			return fmt.Errorf("vault: insert entry %s#%d: %w", r.SessionID, r.Seq, err)
		}
	}
	return tx.Commit()
}

// LoadSessionEntries returns a session's entry payloads in seq order.
func (v *Vault) LoadSessionEntries(id string) ([]string, error) {
	rows, err := v.db.Query(`SELECT payload FROM session_entries WHERE session_id = ? ORDER BY seq`, id)
	if err != nil {
		return nil, fmt.Errorf("vault: load session entries %s: %w", id, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("vault: scan entry: %w", err)
		}
		out = append(out, payload)
	}
	return out, rows.Err()
}

// HasSession reports whether the vault holds a session id.
func (v *Vault) HasSession(id string) (bool, error) {
	var one int
	err := v.db.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("vault: has session %s: %w", id, err)
	}
	return true, nil
}

// GetSession returns one session row.
func (v *Vault) GetSession(id string) (SessionRow, bool, error) {
	var s SessionRow
	var created string
	err := v.db.QueryRow(`SELECT id, project, ts, parent_session, cwd, created_at
                FROM sessions WHERE id = ?`, id).
		Scan(&s.ID, &s.Project, &s.Ts, &s.ParentSession, &s.Cwd, &created)
	if err == sql.ErrNoRows {
		return SessionRow{}, false, nil
	}
	if err != nil {
		return SessionRow{}, false, fmt.Errorf("vault: get session %s: %w", id, err)
	}
	s.CreatedAt = parseTime(created)
	return s, true, nil
}

// ListSessions returns session rows for a project scope, newest
// CreatedAt first.
func (v *Vault) ListSessions(project string) ([]SessionRow, error) {
	rows, err := v.db.Query(`SELECT id, project, ts, parent_session, cwd, created_at
                FROM sessions WHERE project = ? ORDER BY created_at DESC, id`, project)
	if err != nil {
		return nil, fmt.Errorf("vault: list sessions %s: %w", project, err)
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var s SessionRow
		var created string
		if err := rows.Scan(&s.ID, &s.Project, &s.Ts, &s.ParentSession, &s.Cwd, &created); err != nil {
			return nil, fmt.Errorf("vault: scan session: %w", err)
		}
		s.CreatedAt = parseTime(created)
		out = append(out, s)
	}
	return out, rows.Err()
}

// DeleteSession removes a session and — via the FK cascade — all of its
// entries, in one statement.
func (v *Vault) DeleteSession(id string) error {
	if _, err := v.db.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("vault: delete session %s: %w", id, err)
	}
	return nil
}

// SessionPath returns the synthetic file path a vault session reports
// as its identity — the same string the JSONL era used, so every
// File()-derived behavior (evidence scope, resume-by-path, job logging)
// keeps working unchanged.
func SessionPath(project, ts, id string) string {
	return filepath.Join(project, ts+"_"+id+".jsonl")
}

// EpisodeRow is one episodic-memory record (Bet 06): a compaction
// summary keyed by project scope.
type EpisodeRow struct {
	ID      int64
	Project string
	Kind    string
	Content string
	At      time.Time
}

// PutEpisode records one episode (fire-and-forget writes are the norm).
func (v *Vault) PutEpisode(project, kind, content string) error {
	_, err := v.db.Exec(`INSERT INTO episodes (project, kind, content, at) VALUES (?,?,?,?)`,
		project, kind, content, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("vault: put episode: %w", err)
	}
	return nil
}

// RecentEpisodes returns the newest n episodes for a project.
func (v *Vault) RecentEpisodes(project string, n int) ([]EpisodeRow, error) {
	if n <= 0 {
		n = 2
	}
	rows, err := v.db.Query(`SELECT id, project, kind, content, at
		FROM episodes WHERE project = ? ORDER BY at DESC, id DESC LIMIT ?`, project, n)
	if err != nil {
		return nil, fmt.Errorf("vault: recent episodes: %w", err)
	}
	defer rows.Close()
	var out []EpisodeRow
	for rows.Next() {
		var e EpisodeRow
		var at string
		if err := rows.Scan(&e.ID, &e.Project, &e.Kind, &e.Content, &at); err != nil {
			return nil, fmt.Errorf("vault: scan episode: %w", err)
		}
		e.At = parseTime(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
