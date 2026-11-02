package vault

import (
        "bufio"
        "database/sql"
        "encoding/json"
        "fmt"
        "os"
        "path/filepath"
        "strings"
        "time"

        "github.com/damien1141/a1/internal/job"
)

// Migrate is the one-shot importer (Bet 08): on first open it folds the
// legacy file layout under root into rows —
//
//   - personas/personas.json            → personas rows
//   - personas/memory/persona:*.jsonl   → memory_banks rows
//   -   (plus the pre-fix legacy location personas/persona:*.jsonl)
//   - personas/memory/semantic/*.sqlite → mem_p_<id>_* table families
//   - agent/memories.db                 → mem_shared_* table family
//   - session/--*/evidence.jsonl        → evidence_receipts rows
//   - session/--*/*.jsonl               → sessions + session_entries rows
//   - sessions/memory/*.jsonl           → memory_banks rows (session banks)
//
// config.yaml, mcp.json, skills, extensions, and avatars stay files.
// Legacy files are left in place (delete at will); the vault is the
// source of truth from here on. The stamp in vault_meta makes it run
// exactly once; a crash mid-import simply re-runs the missing parts on
// the next open (every import step is INSERT OR IGNORE / idempotent by
// key).
func Migrate(v *Vault, root string) error {
        if v == nil {
                return fmt.Errorf("vault: nil handle")
        }
        if root == "" {
                root = v.Root()
        }
        if root == "" {
                return nil
        }
        if done, _ := v.MetaGet(MetaKeyMigrated); done != "" {
                return nil
        }

        importPersonasFile(v, filepath.Join(root, "personas", "personas.json"))
        importBankFiles(v, filepath.Join(root, "personas", "memory"))
        importBankFiles(v, filepath.Join(root, "personas"))
        importSemanticDir(v, filepath.Join(root, "personas", "memory", "semantic"), PersonaNS)
        importSemanticFile(v, filepath.Join(root, "agent", "memories.db"), SharedNS())
        importEvidenceDirs(v, filepath.Join(root, "session"))
        importSessionDirs(v, filepath.Join(root, "session"))
        importBankFiles(v, filepath.Join(root, "sessions", "memory"))
        importJobDirs(v, filepath.Join(root, "jobs"))

        stamp := time.Now().UTC().Format(time.RFC3339)
        if err := v.MetaSet(MetaKeyMigrated, stamp); err != nil {
                return err
        }
        return nil
}

// importJobDirs folds pre-vault job metas (~/.a1/jobs/<id>/meta.json) into
// jobs rows (Bet 01). The file stays the working store; the row is the
// durable queryable record. INSERT OR IGNORE semantics come free —
// PutJobRow upserts, and re-running the importer only refreshes rows the
// live manager has not written since.
func importJobDirs(v *Vault, root string) {
        entries, err := os.ReadDir(root)
        if err != nil {
                return // no jobs dir yet — nothing to import
        }
        for _, e := range entries {
                if !e.IsDir() {
                        continue
                }
                data, err := os.ReadFile(filepath.Join(root, e.Name(), "meta.json"))
                if err != nil {
                        continue
                }
                var meta job.Meta
                if err := json.Unmarshal(data, &meta); err != nil || meta.ID == "" {
                        continue
                }
                _ = job.PutJobRow(v.DB(), meta)
        }
}

// importPersonasFile reads the old personas.json registry into rows.
func importPersonasFile(v *Vault, path string) {
        data, err := os.ReadFile(path)
        if err != nil {
                return
        }
        var reg struct {
                Active   string `json:"active"`
                Personas []struct {
                        ID                   string    `json:"id"`
                        Name                 string    `json:"name"`
                        Description          string    `json:"description"`
                        Tagline              string    `json:"tagline"`
                        Emoji                string    `json:"emoji"`
                        Hue                  int       `json:"hue"`
                        SystemPrompt         string    `json:"system_prompt"`
                        OverrideSystemPrompt bool      `json:"override_system_prompt"`
                        AvatarExt            string    `json:"avatar_ext"`
                        Builtin              bool      `json:"builtin"`
                        Created              time.Time `json:"created"`
                        Updated              time.Time `json:"updated"`
                        Model                string    `json:"model"`
                        MemoryModel          string    `json:"memory_model"`
                        MemoryEnabled        *bool     `json:"memory_enabled"`
                        PermissionMode       string    `json:"permission_mode"`
                } `json:"personas"`
        }
        if err := json.Unmarshal(data, &reg); err != nil {
                return
        }
        for _, p := range reg.Personas {
                if p.ID == "" {
                        continue
                }
                created, updated := p.Created, p.Updated
                if created.IsZero() {
                        created = time.Now().UTC()
                }
                if updated.IsZero() {
                        updated = created
                }
                _ = v.PutPersona(PersonaRow{
                        ID: p.ID, Name: p.Name, Description: p.Description, Tagline: p.Tagline,
                        Emoji: p.Emoji, Hue: p.Hue, SystemPrompt: p.SystemPrompt,
                        OverrideSystemPrompt: p.OverrideSystemPrompt, AvatarExt: p.AvatarExt,
                        Builtin: p.Builtin, Model: p.Model, MemoryModel: p.MemoryModel,
                        MemoryEnabled: p.MemoryEnabled, PermissionMode: p.PermissionMode,
                        CreatedAt: created, UpdatedAt: updated,
                })
        }
        if reg.Active != "" {
                _ = v.SetActivePersona(reg.Active)
        }
}

// importBankFiles imports every <bank>.jsonl in a directory as rows.
// Files whose bank already has rows are skipped (idempotent re-runs).
func importBankFiles(v *Vault, dir string) {
        entries, err := os.ReadDir(dir)
        if err != nil {
                return
        }
        for _, e := range entries {
                if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
                        continue
                }
                bank := strings.TrimSuffix(e.Name(), ".jsonl")
                if existing, err := v.LoadBank(bank); err != nil || len(existing) > 0 {
                        continue
                }
                path := filepath.Join(dir, e.Name())
                f, err := os.Open(path) //nolint:gosec // path from the local .a1 tree
                if err != nil {
                        continue
                }
                sc := bufio.NewScanner(f)
                sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
                for sc.Scan() {
                        line := strings.TrimSpace(sc.Text())
                        if line == "" {
                                continue
                        }
                        var entry struct {
                                SessionID string    `json:"session_id"`
                                Timestamp time.Time `json:"timestamp"`
                                Type      string    `json:"type"`
                                Content   string    `json:"content"`
                        }
                        if err := json.Unmarshal([]byte(line), &entry); err != nil {
                                continue
                        }
                        ts := entry.Timestamp
                        if ts.IsZero() {
                                ts = time.Now().UTC()
                        }
                        _ = v.AppendBankEntry(bank, BankEntry{
                                Type: entry.Type, Content: entry.Content,
                                Timestamp: ts, SessionID: entry.SessionID,
                        })
                }
                f.Close()
        }
}

// importSemanticDir imports one standalone semantic SQLite per file in a
// directory (the per-persona files) into namespaced families.
func importSemanticDir(v *Vault, dir string, nsFor func(personaID string) string) {
        entries, err := os.ReadDir(dir)
        if err != nil {
                return
        }
        for _, e := range entries {
                if e.IsDir() || !strings.HasSuffix(e.Name(), ".sqlite") {
                        continue
                }
                id := strings.TrimSuffix(e.Name(), ".sqlite")
                importSemanticFile(v, filepath.Join(dir, e.Name()), nsFor(id))
        }
}

// importSemanticFile copies a standalone semantic store (legacy schema,
// unprefixed tables) into a namespaced family in the vault. Idempotent:
// an existing non-empty family is left alone.
func importSemanticFile(v *Vault, path, ns string) {
        if _, err := os.Stat(path); err != nil {
                return
        }
        if has, _ := semanticNSHasRows(v.DB(), ns); has {
                return
        }
        src, err := sql.Open("sqlite",
                "file:"+filepath.Clean(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
        if err != nil {
                return
        }
        defer src.Close()
        if _, err := v.DB().Exec(schemaForRaw(ns)); err != nil {
                return
        }
        copyTable := func(table string, cols string, placeholders string, scan func(*sql.Rows) []any) {
                rows, err := src.Query(`SELECT ` + cols + ` FROM ` + table)
                if err != nil {
                        return
                }
                defer rows.Close()
                stmt, err := v.DB().Prepare(`INSERT OR IGNORE INTO ` + nsTable(ns, table) + ` (` + cols + `) VALUES (` + placeholders + `)`)
                if err != nil {
                        return
                }
                defer stmt.Close()
                for rows.Next() {
                        vals := scan(rows)
                        if vals != nil {
                                _, _ = stmt.Exec(vals...)
                        }
                }
        }
        copyTable("memories",
                `id, content, kind, scope, project_path, thread, session_id, valence, salience, cool_passes, embed_model, embedding, verbatim_ref, encoding_ctx, created_at, status`,
                `?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?`,
                func(r *sql.Rows) []any {
                        var id int64
                        var content, kind, scope, embedModel, encCtx, created, status string
                        var project, thread, verbatim any
                        var sessionID any
                        var valence any
                        var salience float64
                        var cool int64
                        var embedding []byte
                        if err := r.Scan(&id, &content, &kind, &scope, &project, &thread, &sessionID,
                                &valence, &salience, &cool, &embedModel, &embedding, &verbatim, &encCtx, &created, &status); err != nil {
                                return nil
                        }
                        return []any{id, content, kind, scope, project, thread, sessionID, valence, salience, cool, embedModel, embedding, verbatim, encCtx, created, status}
                })
        copyTable("rehearsals", `memory_id, at`, `?,?`, func(r *sql.Rows) []any {
                var id int64
                var at string
                if err := r.Scan(&id, &at); err != nil {
                        return nil
                }
                return []any{id, at}
        })
        copyTable("corrections", `id, memory_id, old_content, new_content, at, source`, `?,?,?,?,?,?`, func(r *sql.Rows) []any {
                var id, mid int64
                var oldC, newC, at string
                var source any
                if err := r.Scan(&id, &mid, &oldC, &newC, &at, &source); err != nil {
                        return nil
                }
                return []any{id, mid, oldC, newC, at, source}
        })
        copyTable("triggers", `id, thread, kind, fire_at, pattern, fires, last_fired, status`, `?,?,?,?,?,?,?,?`, func(r *sql.Rows) []any {
                var id, fires int64
                var thread, kind, status string
                var fireAt, pattern, lastFired any
                if err := r.Scan(&id, &thread, &kind, &fireAt, &pattern, &fires, &lastFired, &status); err != nil {
                        return nil
                }
                return []any{id, thread, kind, fireAt, pattern, fires, lastFired, status}
        })
}

func semanticNSHasRows(db *sql.DB, ns string) (bool, error) {
        var one int
        err := db.QueryRow(`SELECT 1 FROM ` + nsTable(ns, "memories") + ` LIMIT 1`).Scan(&one)
        if err == sql.ErrNoRows {
                return false, nil
        }
        if err != nil {
                return false, err
        }
        return true, nil
}

// importEvidenceDirs walks the session base for project dirs and imports
// each one's evidence.jsonl into receipt rows keyed by the dir path.
func importEvidenceDirs(v *Vault, sessionBase string) {
        projects, err := os.ReadDir(sessionBase)
        if err != nil {
                return
        }
        for _, p := range projects {
                if !p.IsDir() {
                        continue
                }
                dir := filepath.Join(sessionBase, p.Name())
                path := filepath.Join(dir, "evidence.jsonl")
                if existing, err := v.LoadEvidenceReceipts(dir); err != nil || len(existing) > 0 {
                        continue
                }
                data, err := os.ReadFile(path)
                if err != nil {
                        continue
                }
                for _, line := range strings.Split(string(data), "\n") {
                        line = strings.TrimSpace(line)
                        if line == "" {
                                continue
                        }
                        _ = v.AppendEvidenceReceipt(dir, line)
                }
        }
}

// importSessionDirs imports every session JSONL under every project dir
// into sessions + session_entries rows. The payload is the raw line, so
// replay stays byte-identical to the file era.
func importSessionDirs(v *Vault, sessionBase string) {
        projects, err := os.ReadDir(sessionBase)
        if err != nil {
                return
        }
        for _, p := range projects {
                if !p.IsDir() {
                        continue
                }
                project := filepath.Join(sessionBase, p.Name())
                entries, err := os.ReadDir(project)
                if err != nil {
                        continue
                }
                for _, e := range entries {
                        if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
                                continue
                        }
                        importSessionFile(v, project, filepath.Join(project, e.Name()))
                }
        }
}

func importSessionFile(v *Vault, project, path string) {
        // Filename: <ts>_<sessionID>.jsonl — the identity the synthetic path
        // reproduces.
        base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
        idx := strings.Index(base, "_")
        if idx <= 0 {
                return
        }
        ts, id := base[:idx], base[idx+1:]
        if has, _ := v.HasSession(id); has {
                return
        }
        f, err := os.Open(path) //nolint:gosec // path from the local .a1 tree
        if err != nil {
                return
        }
        defer f.Close()

        var (
                rows    []SessionEntryRow
                seq     int64
                idToSeq = map[string]int64{}
                header  struct {
                        ID            string `json:"id"`
                        Cwd           string `json:"cwd"`
                        ParentSession string `json:"parentSession"`
                }
                createdAt = time.Now().UTC()
        )
        sc := bufio.NewScanner(f)
        sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
        for sc.Scan() {
                line := strings.TrimSpace(sc.Text())
                if line == "" {
                        continue
                }
                var probe struct {
                        Type string `json:"type"`
                        ID   string `json:"id"`
                }
                if err := json.Unmarshal([]byte(line), &probe); err != nil {
                        continue
                }
                cur := seq
                seq++
                idToSeq[probe.ID] = cur
                rows = append(rows, SessionEntryRow{SessionID: id, Seq: cur, Kind: probe.Type, Payload: line})
                if probe.Type == "EntrySession" || probe.Type == "session" {
                        _ = json.Unmarshal([]byte(line), &header)
                        createdAt = time.Now().UTC()
                }
        }
        if header.ID == "" || len(rows) == 0 {
                return
        }
        // Second pass: parent_seq from the payload's parent id reference.
        for i := range rows {
                var probe struct {
                        ID       string  `json:"id"`
                        ParentID *string `json:"parentID"`
                }
                if err := json.Unmarshal([]byte(rows[i].Payload), &probe); err == nil && probe.ParentID != nil {
                        if ps, ok := idToSeq[*probe.ParentID]; ok {
                                psCopy := ps
                                rows[i].ParentSeq = &psCopy
                        }
                }
        }
        _ = v.PutSession(SessionRow{
                ID: id, Project: project, Ts: ts,
                ParentSession: header.ParentSession, Cwd: header.Cwd, CreatedAt: createdAt,
        })
        _ = v.AppendSessionEntries(rows)
}

// schemaForRaw renders the namespaced memstore schema using the vault's
// own table-naming convention (mirrors memstore.SchemaFor; duplicated
// here so the vault package can create families during migration without
// importing memstore).
func schemaForRaw(ns string) string {
        // Delegate the string rendering to the same rules the memstore uses.
        const tmpl = `
CREATE TABLE IF NOT EXISTS %s (
  id           INTEGER PRIMARY KEY,
  content      TEXT NOT NULL,
  kind         TEXT NOT NULL,
  scope        TEXT NOT NULL,
  project_path TEXT,
  thread       TEXT,
  session_id   INTEGER,
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
CREATE INDEX IF NOT EXISTS %s ON %s(scope, project_path, status);
CREATE INDEX IF NOT EXISTS %s ON %s(thread);
CREATE TABLE IF NOT EXISTS %s (
  memory_id INTEGER NOT NULL,
  at        TEXT NOT NULL,
  PRIMARY KEY (memory_id, at)
);
CREATE TABLE IF NOT EXISTS %s (
  id          INTEGER PRIMARY KEY,
  memory_id   INTEGER NOT NULL,
  old_content TEXT NOT NULL,
  new_content TEXT NOT NULL,
  at          TEXT NOT NULL,
  source      TEXT
);
CREATE TABLE IF NOT EXISTS %s (
  id         INTEGER PRIMARY KEY,
  thread     TEXT NOT NULL,
  kind       TEXT NOT NULL,
  fire_at    TEXT,
  pattern    TEXT,
  fires      INTEGER NOT NULL DEFAULT 0,
  last_fired TEXT,
  status     TEXT NOT NULL DEFAULT 'armed'
);
CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
  content, content='%s', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS %s AFTER INSERT ON %s BEGIN
  INSERT INTO %s(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS %s AFTER DELETE ON %s BEGIN
  INSERT INTO %s(%s, rowid, content) VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER IF NOT EXISTS %s AFTER UPDATE ON %s BEGIN
  INSERT INTO %s(%s, rowid, content) VALUES ('delete', old.id, old.content);
  INSERT INTO %s(rowid, content) VALUES (new.id, new.content);
END;
`
        mem := nsTable(ns, "memories")
        fts := nsTable(ns, "memories_fts")
        reh := nsTable(ns, "rehearsals")
        cor := nsTable(ns, "corrections")
        trg := nsTable(ns, "triggers")
        return fmt.Sprintf(tmpl,
                mem,
                nsTable(ns, "idx_mem_scope"), mem,
                nsTable(ns, "idx_mem_thread"), mem,
                reh, cor, trg,
                fts, mem,
                nsTable(ns, "mem_ai"), mem, fts,
                nsTable(ns, "mem_ad"), mem, fts, fts,
                nsTable(ns, "mem_au"), mem, fts, fts, fts)
}
