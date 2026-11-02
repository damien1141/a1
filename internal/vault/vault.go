// Package vault owns the one durable store (Bet 08): ~/.a1/a1.db.
//
// Before the vault, a1 persisted onto two and a half systems — session
// trees as JSONL files, persona memory banks as other JSONL files, the
// persona registry as a JSON blob, one semantic SQLite file per persona,
// a shared semantic SQLite under agent/, and evidence receipts as JSONL
// beside each project's sessions. Deleting anything meant chasing files;
// backing up meant copying a directory tree nobody could name completely.
//
// The vault is one SQLite database with namespaced tables: personas and
// their memory banks are rows, evidence receipts are rows keyed by the
// project scope they belong to, session trees are rows keyed
// (session, seq, parent), and per-persona semantic stores live as
// namespaced table families (mem_<ns>_*) inside the same file.
// config.yaml, mcp.json, skills, extensions, and avatars stay files —
// things a user edits or ships as artifacts. Everything else is rows.
//
// modernc.org/sqlite (pure Go, FTS5) is the driver; foreign_keys is on
// via the DSN so persona→memory and session→entry cascades actually fire.
package vault

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // driver name "sqlite"; FTS5 included; pure Go
)

// MetaKeyMigrated is the vault_meta key holding the migration stamp.
const MetaKeyMigrated = "migrated"

// schema is the core DDL. Semantic-memory table families are created
// lazily by the memstore namespace helper (see memstore.OpenIn).
const schema = `
CREATE TABLE IF NOT EXISTS vault_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS personas (
  id                    TEXT PRIMARY KEY,
  name                  TEXT NOT NULL,
  description           TEXT NOT NULL DEFAULT '',
  tagline               TEXT NOT NULL DEFAULT '',
  emoji                 TEXT NOT NULL DEFAULT '',
  hue                   TEXT NOT NULL DEFAULT '',
  system_prompt         TEXT NOT NULL DEFAULT '',
  override_system_prompt INTEGER NOT NULL DEFAULT 0,
  avatar_ext            TEXT NOT NULL DEFAULT '',
  builtin               INTEGER NOT NULL DEFAULT 0,
  model                 TEXT NOT NULL DEFAULT '',
  memory_model          TEXT NOT NULL DEFAULT '',
  memory_enabled        INTEGER,
  permission_mode       TEXT NOT NULL DEFAULT '',
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL
);

-- One row per persona memory-bank entry (the old persona:<id>.jsonl
-- banks). Explicit deletes in DeletePersona keep the set consistent —
-- the table is deliberately not FK-bound so non-persona banks (session
-- banks, should they move here) can share it.
CREATE TABLE IF NOT EXISTS memory_banks (
  bank       TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  type       TEXT NOT NULL,
  content    TEXT NOT NULL,
  timestamp  TEXT NOT NULL,
  session_id TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (bank, seq)
);

-- Evidence receipts, keyed by the project session-dir scope (the same
-- scope the old evidence.jsonl file covered: one per project, shared by
-- every session in it, so the protected ratchet survives resume).
CREATE TABLE IF NOT EXISTS evidence_receipts (
  scope   TEXT NOT NULL,
  seq     INTEGER NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY (scope, seq)
);

-- Session trees: one row per session, entries keyed (session, seq,
-- parent). The payload column holds the byte-faithful JSON line the
-- JSONL backing wrote, so replay is decode-in-order — exactly the old
-- OpenSession walk, minus the file IO.
CREATE TABLE IF NOT EXISTS sessions (
  id             TEXT PRIMARY KEY,
  project        TEXT NOT NULL,
  ts             TEXT NOT NULL,
  parent_session TEXT NOT NULL DEFAULT '',
  cwd            TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project, created_at);

-- Episodic memory (Bet 06): compaction summaries (which already embed
-- the harness evidence record) as retrievable "what happened last time"
-- rows, keyed by the project scope.
CREATE TABLE IF NOT EXISTS episodes (
  id      INTEGER PRIMARY KEY,
  project TEXT NOT NULL,
  kind    TEXT NOT NULL,
  content TEXT NOT NULL,
  at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_episodes_project ON episodes(project, at);

CREATE TABLE IF NOT EXISTS session_entries (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL,
  parent_seq INTEGER,
  kind       TEXT NOT NULL,
  payload    TEXT NOT NULL,
  PRIMARY KEY (session_id, seq)
);
`

// Vault is a handle on the one durable store. Safe for concurrent use;
// the underlying *sql.DB pools connections (WAL mode, busy timeout).
type Vault struct {
	db   *sql.DB
	root string
}

// Open opens (creating if needed) the vault at <root>/a1.db. Root is
// typically the a1 global root (~/.a1); tests pass a temp dir.
func Open(root string) (*Vault, error) {
	if root == "" {
		return nil, fmt.Errorf("vault: root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("vault: create root: %w", err)
	}
	path := filepath.Join(root, "a1.db")
	db, err := sql.Open("sqlite",
		"file:"+filepath.Clean(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("vault: open %s: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("vault: schema: %w", err)
	}
	return &Vault{db: db, root: root}, nil
}

// OpenDB adopts an existing database handle (tests, or a caller that
// manages the connection itself). The schema is ensured.
func OpenDB(db *sql.DB, root string) (*Vault, error) {
	if db == nil {
		return nil, fmt.Errorf("vault: db is required")
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("vault: schema: %w", err)
	}
	return &Vault{db: db, root: root}, nil
}

// Close releases the database handle.
func (v *Vault) Close() error {
	if v == nil || v.db == nil {
		return nil
	}
	return v.db.Close()
}

// DB exposes the underlying handle for subsystems that own row access
// (persona, evidence, session, memory). Callers must not close it.
func (v *Vault) DB() *sql.DB {
	if v == nil {
		return nil
	}
	return v.db
}

// Root returns the vault's root directory (~/.a1 or a test dir).
func (v *Vault) Root() string {
	if v == nil {
		return ""
	}
	return v.root
}

// Path returns the database file path (<root>/a1.db).
func (v *Vault) Path() string {
	if v == nil {
		return ""
	}
	return filepath.Join(v.root, "a1.db")
}

// MetaGet reads a vault_meta value ("" when unset).
func (v *Vault) MetaGet(key string) (string, error) {
	var val string
	err := v.db.QueryRow(`SELECT value FROM vault_meta WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("vault: meta get %s: %w", key, err)
	}
	return val, nil
}

// MetaSet writes a vault_meta value.
func (v *Vault) MetaSet(key, value string) error {
	if _, err := v.db.Exec(
		`INSERT INTO vault_meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		return fmt.Errorf("vault: meta set %s: %w", key, err)
	}
	return nil
}

// Boot opens the vault at root and runs the one-shot migration — the
// single call every surface makes at startup before installing the
// handle anywhere (session.SetDefaultVault, persona.AttachVault, the
// shared memory store). A boot failure is logged-and-degraded by the
// caller: the process keeps running on the legacy file layout.
func Boot(root string) (*Vault, error) {
	v, err := Open(root)
	if err != nil {
		return nil, err
	}
	if err := Migrate(v, root); err != nil {
		v.Close()
		return nil, err
	}
	return v, nil
}
