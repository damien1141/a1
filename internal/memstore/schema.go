package memstore

import (
	"fmt"
	"strings"
)

// schema is the SQLite DDL from §2 of the spec for the legacy standalone
// store (own file, unprefixed tables). Deviation: corrections columns
// are old_content/new_content (before/after are SQLite trigger keywords).
const schema = `
PRAGMA journal_mode=WAL;

CREATE TABLE IF NOT EXISTS sessions (
  id           INTEGER PRIMARY KEY,
  started_at   TEXT NOT NULL,
  ended_at      TEXT,
  project_path TEXT,
  summary      TEXT,
  embedding    BLOB,
  valence      REAL
);

CREATE TABLE IF NOT EXISTS memories (
  id           INTEGER PRIMARY KEY,
  content      TEXT NOT NULL,
  kind         TEXT NOT NULL,
  scope        TEXT NOT NULL,
  project_path TEXT,
  thread       TEXT,
  session_id   INTEGER REFERENCES sessions(id),
  valence      REAL,
  salience     REAL NOT NULL DEFAULT 0.3,
  cool_passes  INTEGER NOT NULL DEFAULT 0,
  embed_model  TEXT NOT NULL,
  embedding    BLOB NOT NULL,
  verbatim_ref TEXT,
  encoding_ctx TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'active'
);
CREATE INDEX IF NOT EXISTS idx_mem_scope  ON memories(scope, project_path, status);
CREATE INDEX IF NOT EXISTS idx_mem_thread ON memories(thread);

CREATE TABLE IF NOT EXISTS rehearsals (
  memory_id INTEGER NOT NULL REFERENCES memories(id),
  at        TEXT NOT NULL,
  PRIMARY KEY (memory_id, at)
);

CREATE TABLE IF NOT EXISTS corrections (
  id          INTEGER PRIMARY KEY,
  memory_id   INTEGER NOT NULL,
  old_content TEXT NOT NULL,
  new_content TEXT NOT NULL,
  at          TEXT NOT NULL,
  source      TEXT
);

CREATE TABLE IF NOT EXISTS triggers (
  id         INTEGER PRIMARY KEY,
  thread     TEXT NOT NULL,
  kind       TEXT NOT NULL,
  fire_at    TEXT,
  pattern    TEXT,
  fires      INTEGER NOT NULL DEFAULT 0,
  last_fired TEXT,
  status     TEXT NOT NULL DEFAULT 'armed'
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
  content, content='memories', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS mem_ai AFTER INSERT ON memories BEGIN
  INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS mem_ad AFTER DELETE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, content)
  VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER IF NOT EXISTS mem_au AFTER UPDATE ON memories BEGIN
  INSERT INTO memories_fts(memories_fts, rowid, content)
  VALUES ('delete', old.id, old.content);
  INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
`

// SchemaFor renders the same DDL for a namespaced table family inside a
// shared database (the vault, Bet 08): every table, index, and trigger
// is prefixed mem_<ns>_ so one SQLite file holds many isolated stores —
// one family per persona plus the shared engine bank — without a single
// WHERE clause leaking between them. ns must already be sanitized
// ([a-z0-9_]; see vault.sanitizeNS).
func SchemaFor(ns string) string {
	if ns == "" || ns == "main" {
		return schema
	}
	r := strings.NewReplacer(
		"memories_fts", "mem_"+ns+"_memories_fts",
		"memories", "mem_"+ns+"_memories",
		"sessions", "mem_"+ns+"_sessions",
		"rehearsals", "mem_"+ns+"_rehearsals",
		"corrections", "mem_"+ns+"_corrections",
		"triggers", "mem_"+ns+"_triggers",
		"idx_mem_scope", "mem_"+ns+"_idx_mem_scope",
		"idx_mem_thread", "mem_"+ns+"_idx_mem_thread",
		"mem_ai", "mem_"+ns+"_mem_ai",
		"mem_ad", "mem_"+ns+"_mem_ad",
		"mem_au", "mem_"+ns+"_mem_au",
	)
	return r.Replace(schema)
}

// MemTable returns the namespaced memories table for a store namespace.
func MemTable(ns string) string {
	if ns == "" {
		return "memories"
	}
	return fmt.Sprintf("mem_%s_memories", ns)
}
