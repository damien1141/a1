package persona

import (
	"fmt"
	"os"

	"github.com/damien1141/a1/internal/memstore"
	"github.com/damien1141/a1/internal/session/memory"
	"github.com/damien1141/a1/internal/vault"
)

// AttachVault switches the store onto the one durable store (Bet 08).
// After this call the registry, memory banks, and per-persona semantic
// stores are rows and namespaced table families inside ~/.a1/a1.db —
// the JSONL registry, bank files, per-persona SQLite files, and the
// legacy-bank adoption shim are all bypassed (the one-shot migration
// has already imported whatever they held). The Store API is unchanged;
// only the address of the bytes changed.
//
// Callers attach once at startup, right after vault.Migrate.
func (s *Store) AttachVault(v *vault.Vault) {
	if v == nil || v.DB() == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vault = v
	// The next load() repopulates from rows.
	s.loaded = false
}

// personaFromRow maps a vault row to the Persona shape.
func personaFromRow(r vault.PersonaRow) Persona {
	return Persona{
		ID:                   r.ID,
		Name:                 r.Name,
		Description:          r.Description,
		Tagline:              r.Tagline,
		Emoji:                r.Emoji,
		Hue:                  r.Hue,
		SystemPrompt:         r.SystemPrompt,
		OverrideSystemPrompt: r.OverrideSystemPrompt,
		AvatarExt:            r.AvatarExt,
		Builtin:              r.Builtin,
		Created:              r.CreatedAt,
		Updated:              r.UpdatedAt,
		Model:                r.Model,
		MemoryModel:          r.MemoryModel,
		MemoryEnabled:        r.MemoryEnabled,
		PermissionMode:       r.PermissionMode,
	}
}

// rowFromPersona maps a Persona to the vault row shape.
func rowFromPersona(p Persona) vault.PersonaRow {
	return vault.PersonaRow{
		ID:                   p.ID,
		Name:                 p.Name,
		Description:          p.Description,
		Tagline:              p.Tagline,
		Emoji:                p.Emoji,
		Hue:                  p.Hue,
		SystemPrompt:         p.SystemPrompt,
		OverrideSystemPrompt: p.OverrideSystemPrompt,
		AvatarExt:            p.AvatarExt,
		Builtin:              p.Builtin,
		Model:                p.Model,
		MemoryModel:          p.MemoryModel,
		MemoryEnabled:        p.MemoryEnabled,
		PermissionMode:       p.PermissionMode,
		CreatedAt:            p.Created,
		UpdatedAt:            p.Updated,
	}
}

// loadVault repopulates the in-memory registry from vault rows. An empty
// vault (fresh install) seeds the default persona, exactly like a
// missing personas.json did.
func (s *Store) loadVault() error {
	rows, err := s.vault.ListPersonas()
	if err != nil {
		return fmt.Errorf("persona: load from vault: %w", err)
	}
	reg := registry{Active: DefaultPersonaID}
	hasDefault := false
	for _, r := range rows {
		if r.ID == DefaultPersonaID {
			hasDefault = true
		}
		reg.Personas = append(reg.Personas, personaFromRow(r))
	}
	if !hasDefault {
		reg.Personas = append([]Persona{defaultPersona()}, reg.Personas...)
	}
	if active, err := s.vault.ActivePersona(); err == nil && active != "" {
		reg.Active = active
	}
	s.reg = reg
	s.loaded = true
	return nil
}

// saveVault mirrors the registry into rows: upsert every persona in the
// registry, delete rows that left it (with their banks and semantic
// families), and remember the active id. This is the deletability the
// file layout never had — one transaction, no orphans.
func (s *Store) saveVault() error {
	current := make(map[string]bool, len(s.reg.Personas))
	for _, p := range s.reg.Personas {
		current[p.ID] = true
		if err := s.vault.PutPersona(rowFromPersona(p)); err != nil {
			return err
		}
	}
	if err := s.vault.SetActivePersona(s.reg.Active); err != nil {
		return err
	}
	rows, err := s.vault.ListPersonas()
	if err != nil {
		return err
	}
	for _, r := range rows {
		if !current[r.ID] {
			if err := s.vault.DeletePersona(r.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteVault removes a persona end-to-end: vault cascade (registry row,
// bank rows, semantic family) plus the avatar directory, which stays a
// file (user-facing artifact).
func (s *Store) deleteVault(id string) error {
	if err := s.vault.DeletePersona(id); err != nil {
		return err
	}
	_ = os.RemoveAll(s.dir(id))
	return nil
}

// vaultBank opens the persona's memory bank as vault rows.
func (s *Store) vaultBank(p Persona) (*memory.Bank, error) {
	return memory.OpenVaultBank(vault.PersonaBank(p.ID), s.vault)
}

// vaultSemantic opens the persona's semantic store as a namespaced table
// family in the vault.
func (s *Store) vaultSemantic(p Persona, emb memstore.Embedder, params memstore.Params) (*memstore.Store, error) {
	return memstore.OpenIn(s.vault.DB(), vault.PersonaNS(p.ID), emb, params)
}
