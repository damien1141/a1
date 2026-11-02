package vault

import (
        "database/sql"
        "errors"
        "fmt"
        "strings"
        "time"
)

// PersonaRow is the vault's storage shape for a persona. The persona
// package maps between this and its own Persona struct; the vault owns
// the schema, so it owns the row type.
type PersonaRow struct {
        ID                   string
        Name                 string
        Description          string
        Tagline              string
        Emoji                string
        Hue                  int
        SystemPrompt         string
        OverrideSystemPrompt bool
        AvatarExt            string
        Builtin              bool
        Model                string
        MemoryModel          string
        // MemoryEnabled is nil = unset (default-on), otherwise the explicit
        // toggle value.
        MemoryEnabled  *bool
        PermissionMode string
        CreatedAt      time.Time
        UpdatedAt      time.Time
}

// PutPersona upserts one persona row.
func (v *Vault) PutPersona(p PersonaRow) error {
        var memEnabled any
        if p.MemoryEnabled != nil {
                memEnabled = *p.MemoryEnabled
        }
        _, err := v.db.Exec(`INSERT INTO personas (
                id, name, description, tagline, emoji, hue, system_prompt,
                override_system_prompt, avatar_ext, builtin, model, memory_model,
                memory_enabled, permission_mode, created_at, updated_at
        ) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
        ON CONFLICT(id) DO UPDATE SET
                name=excluded.name, description=excluded.description,
                tagline=excluded.tagline, emoji=excluded.emoji, hue=excluded.hue,
                system_prompt=excluded.system_prompt,
                override_system_prompt=excluded.override_system_prompt,
                avatar_ext=excluded.avatar_ext, builtin=excluded.builtin,
                model=excluded.model, memory_model=excluded.memory_model,
                memory_enabled=excluded.memory_enabled,
                permission_mode=excluded.permission_mode,
                updated_at=excluded.updated_at`,
                p.ID, p.Name, p.Description, p.Tagline, p.Emoji, p.Hue, p.SystemPrompt,
                boolInt(p.OverrideSystemPrompt), p.AvatarExt, boolInt(p.Builtin), p.Model, p.MemoryModel,
                memEnabled, p.PermissionMode, p.CreatedAt.UTC().Format(time.RFC3339Nano), p.UpdatedAt.UTC().Format(time.RFC3339Nano))
        if err != nil {
                return fmt.Errorf("vault: put persona %s: %w", p.ID, err)
        }
        return nil
}

// ListPersonas returns all persona rows sorted by CreatedAt.
func (v *Vault) ListPersonas() ([]PersonaRow, error) {
        rows, err := v.db.Query(`SELECT
                id, name, description, tagline, emoji, hue, system_prompt,
                override_system_prompt, avatar_ext, builtin, model, memory_model,
                memory_enabled, permission_mode, created_at, updated_at
                FROM personas ORDER BY created_at, id`)
        if err != nil {
                return nil, fmt.Errorf("vault: list personas: %w", err)
        }
        defer rows.Close()
        var out []PersonaRow
        for rows.Next() {
                p, err := scanPersona(rows)
                if err != nil {
                        return nil, err
                }
                out = append(out, p)
        }
        return out, rows.Err()
}

// GetPersona returns one persona row (ok=false when absent).
func (v *Vault) GetPersona(id string) (PersonaRow, bool, error) {
        row := v.db.QueryRow(`SELECT
                id, name, description, tagline, emoji, hue, system_prompt,
                override_system_prompt, avatar_ext, builtin, model, memory_model,
                memory_enabled, permission_mode, created_at, updated_at
                FROM personas WHERE id = ?`, id)
        p, err := scanPersona(row)
        if errors.Is(err, sql.ErrNoRows) {
                return PersonaRow{}, false, nil
        }
        if err != nil {
                return PersonaRow{}, false, err
        }
        return p, true, nil
}

func scanPersona(rows interface{ Scan(...any) error }) (PersonaRow, error) {
        var p PersonaRow
        var override, builtin int
        var memEnabled sql.NullBool
        var created, updated string
        if err := rows.Scan(
                &p.ID, &p.Name, &p.Description, &p.Tagline, &p.Emoji, &p.Hue, &p.SystemPrompt,
                &override, &p.AvatarExt, &builtin, &p.Model, &p.MemoryModel,
                &memEnabled, &p.PermissionMode, &created, &updated); err != nil {
                return PersonaRow{}, fmt.Errorf("vault: scan persona: %w", err)
        }
        p.OverrideSystemPrompt = override != 0
        p.Builtin = builtin != 0
        if memEnabled.Valid {
                b := memEnabled.Bool
                p.MemoryEnabled = &b
        }
        p.CreatedAt = parseTime(created)
        p.UpdatedAt = parseTime(updated)
        return p, nil
}

// SetActivePersona remembers the active persona id (vault_meta).
func (v *Vault) SetActivePersona(id string) error {
        return v.MetaSet("active_persona", id)
}

// ActivePersona returns the remembered active persona id.
func (v *Vault) ActivePersona() (string, error) {
        return v.MetaGet("active_persona")
}

// DeletePersona removes a persona end-to-end in one transaction: the
// registry row, every memory-bank row, and the persona's namespaced
// semantic-memory table family. This is the deletability the file layout
// could never provide — one call, no orphans.
func (v *Vault) DeletePersona(id string) error {
        tx, err := v.db.Begin()
        if err != nil {
                return fmt.Errorf("vault: delete persona %s: %w", id, err)
        }
        defer func() { _ = tx.Rollback() }()

        if _, err := tx.Exec(`DELETE FROM personas WHERE id = ?`, id); err != nil {
                return fmt.Errorf("vault: delete persona row: %w", err)
        }
        if _, err := tx.Exec(`DELETE FROM memory_banks WHERE bank = ?`, PersonaBank(id)); err != nil {
                return fmt.Errorf("vault: delete persona bank: %w", err)
        }
        if err := dropSemanticNS(tx, PersonaNS(id)); err != nil {
                return fmt.Errorf("vault: delete persona semantic store: %w", err)
        }
        if err := tx.Commit(); err != nil {
                return fmt.Errorf("vault: delete persona %s: %w", id, err)
        }
        return nil
}

// PersonaBank is the memory-bank key for a persona's JSONL-equivalent
// bank ("persona:<id>" — the same namespace the old bank files used).
func PersonaBank(id string) string {
        return "persona:" + id
}

// PersonaNS is the semantic-store namespace for a persona.
func PersonaNS(id string) string {
        return "p_" + sanitizeNS(id)
}

// SharedNS is the semantic-store namespace for the engine-level shared
// bank (the old ~/.a1/agent/memories.db).
func SharedNS() string {
        return "shared"
}

// sanitizeNS clamps a namespace to the characters safe for SQLite table
// names ([a-z0-9_]); anything else folds to '_'.
func sanitizeNS(ns string) string {
        ns = strings.ToLower(ns)
        var b strings.Builder
        for _, r := range ns {
                switch {
                case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
                        b.WriteRune(r)
                default:
                        b.WriteByte('_')
                }
        }
        out := b.String()
        if out == "" {
                return "_"
        }
        return out
}

// semanticTables are the table families a memstore namespace owns, in
// child-first drop order (SQLite enforces the intra-family foreign keys
// when foreign_keys=ON). The CREATE statements live in memstore
// (schemaFor); the vault only needs the names to drop a namespace.
var semanticTables = []string{
        "rehearsals", "corrections", "triggers", "memories_fts", "memories", "sessions",
}

func dropSemanticNS(tx *sql.Tx, ns string) error {
        for _, t := range semanticTables {
                if _, err := tx.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, nsTable(ns, t))); err != nil {
                        return err
                }
        }
        return nil
}

// NSTable exposes the namespaced table name so memstore and the vault
// agree on the family layout (mem_p_x_memories &c).
func NSTable(ns, table string) string {
        return nsTable(ns, table)
}

func nsTable(ns, table string) string {
        return "mem_" + sanitizeNS(ns) + "_" + table
}

func boolInt(b bool) int {
        if b {
                return 1
        }
        return 0
}

func parseTime(s string) time.Time {
        if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
                return t
        }
        if t, err := time.Parse(time.RFC3339, s); err == nil {
                return t
        }
        return time.Time{}
}
