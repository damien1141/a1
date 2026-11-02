// Package persona stores user-defined agent personas: distinct personalities
// with their own memory banks and (optionally) their own system-prompt
// override and avatar image.
//
// Storage (Bet 08): personas, their memory banks, and their per-persona
// semantic stores live in the one durable store (~/.a1/a1.db) once
// AttachVault runs at startup — registry rows, memory_banks rows, and
// mem_p_<id>_* table families. Avatars stay files under personas/<id>/.
// Without a vault attached (tests, degraded boots) the legacy file layout
// applies: personas.json registry, memory/persona:<id>.jsonl banks, and
// per-persona semantic SQLite files.
//
// The default persona ("default", friendly neutral assistant) is created on
// first load so the store is never empty.
package persona

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/damien1141/a1/internal/memstore"

	"github.com/damien1141/a1/internal/vault"
)

// DefaultPersonaID is the ID of the always-present neutral persona.
const DefaultPersonaID = "default"

// Limits keep hand-edited registries (and over-enthusiastic API callers)
// from producing unusable UI rows or megabyte system prompts. Mirrors the
// limits the original PersonaManager UI enforced client-side.
const (
	MaxName    = 40
	MaxTagline = 100
	MaxEmoji   = 16 // runes; ZWJ emoji sequences are longer than one rune
	MaxPrompt  = 4000
)

// Persona is one user-defined agent persona.
type Persona struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description,omitempty"`
	Tagline              string    `json:"tagline,omitempty"`
	Emoji                string    `json:"emoji,omitempty"`
	Hue                  int       `json:"hue,omitempty"`
	SystemPrompt         string    `json:"system_prompt,omitempty"`
	OverrideSystemPrompt bool      `json:"override_system_prompt,omitempty"` // this persona's prompt replaces the default system prompt
	AvatarExt            string    `json:"avatar_ext,omitempty"`             // "png" | "jpeg" | "webp" | "gif" | ""
	Builtin              bool      `json:"builtin,omitempty"`
	Created              time.Time `json:"created"`
	Updated              time.Time `json:"updated"`

	// Per-persona settings. Empty/nil values fall back to engine defaults,
	// so existing personas (and the default persona) keep working unchanged.

	// Model, when non-empty, overrides the engine's configured LLM model
	// for this persona. Useful for routing different personas to different
	// local or API models (e.g. work persona → 70B for reasoning, chat
	// persona → 8B for speed).
	Model string `json:"model,omitempty"`

	// MemoryModel, when non-empty, routes memory-consolidation traffic to
	// a dedicated (typically smaller/faster) model while the user-facing
	// assistant stays on Model — the persona-editor toggle (Bet 06). Empty
	// = consolidation uses the main model.
	MemoryModel string `json:"memory_model,omitempty"`

	// MemoryEnabled gates the auto-injection + auto-consolidation of the
	// persona's semantic memory store. Pointer so the zero value (nil) is
	// distinguishable from explicit false: nil = default-on, false = off.
	// When off, the persona still has its semantic SQLite file (preserved
	// for future turn-on), but no inlet retrieval and no outlet
	// consolidation fires. The remember/recall tools remain registered so
	// the model can still explicitly query when it wants to.
	MemoryEnabled *bool `json:"memory_enabled,omitempty"`

	// PermissionMode, when non-empty, sets the APPA permission mode this
	// persona starts in (interactive | readonly | autopilot | headless-strict).
	// Empty = use the engine's current mode (no override on persona switch).
	PermissionMode string `json:"permission_mode,omitempty"`
}

// MemoryEnabledOrDefault returns the effective memory-enabled flag for this
// persona: the explicit setting when present, true when nil (the default).
// Callers should use this rather than dereferencing MemoryEnabled directly
// to handle the unset case.
func (p Persona) MemoryEnabledOrDefault() bool {
	if p.MemoryEnabled == nil {
		return true
	}
	return *p.MemoryEnabled
}

// ReplacesSystemPrompt reports whether this persona's prompt should fully
// replace the agent's default system prompt instead of being injected
// alongside it. An override persona with an empty prompt keeps the default —
// replacing the base prompt with nothing would silently lobotomize the agent.
func (p Persona) ReplacesSystemPrompt() bool {
	return p.OverrideSystemPrompt && strings.TrimSpace(p.SystemPrompt) != ""
}

// HasAvatar reports whether the persona has an uploaded avatar file.
func (p Persona) HasAvatar() bool { return p.AvatarExt != "" }

// registry is the on-disk JSON layout for personas.json.
type registry struct {
	Active   string    `json:"active"`
	Personas []Persona `json:"personas"`
}

// Store is the persona registry. It is safe for concurrent use.
type Store struct {
	root      string
	memoryDir string
	mu        sync.Mutex
	reg       registry
	loaded    bool

	// vault, when attached (AttachVault, Bet 08), is the one durable
	// store: registry rows, row-backed memory banks, namespaced
	// semantic families. Nil = the legacy file layout (tests, or a
	// pre-migration process).
	vault *vault.Vault

	// Per-persona SQLite semantic memory stores, opened lazily by
	// SemanticStore() and closed by CloseSemanticStores(). Cached so
	// repeated calls for the same persona ID reuse the same handle.
	semanticStores map[string]*memstore.Store
	semanticMu     sync.Mutex
}

// NewStore returns a store rooted at dir (typically project.Global().PersonasDir()).
// The directory is created on first write if it does not exist. Persona memory
// banks live under <dir>/memory so they stay inside the store's tree (tests
// rooted at a temp dir never leak into the real ~/.a1).
func NewStore(dir string) *Store {
	return &Store{
		root:      dir,
		memoryDir: filepath.Join(dir, "memory"),
	}
}

// defaultPersona is the always-present neutral assistant persona.
func defaultPersona() Persona {
	now := time.Now().UTC()
	return Persona{
		ID:          DefaultPersonaID,
		Name:        "a1",
		Description: "The neutral, default assistant. Plainly helpful and concise.",
		Emoji:       "\U0001F9ED", // compass — the neutral mark
		Hue:         200,
		Builtin:     true,
		Created:     now,
		Updated:     now,
	}
}

func (s *Store) path() string { return filepath.Join(s.root, "personas.json") }

func (s *Store) dir(id string) string { return filepath.Join(s.root, id) }

// AvatarPath returns the absolute path to the persona's avatar file, or ""
// when the persona has no avatar.
func (s *Store) AvatarPath(p Persona) string {
	if !p.HasAvatar() {
		return ""
	}
	return filepath.Join(s.dir(p.ID), "avatar."+p.AvatarExt)
}

// load reads the registry from disk if it has not been loaded yet. Missing
// file is treated as the empty registry (and a default persona is created on
// first save). The default persona is always present in memory even if the
// on-disk registry was somehow emptied.
func (s *Store) load() error {
	if s.loaded {
		return nil
	}
	if s.vault != nil {
		return s.loadVault()
	}
	data, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			s.reg = registry{Active: DefaultPersonaID, Personas: []Persona{defaultPersona()}}
			s.loaded = true
			return nil
		}
		return fmt.Errorf("persona: read registry: %w", err)
	}
	if err := json.Unmarshal(data, &s.reg); err != nil {
		return fmt.Errorf("persona: parse registry: %w", err)
	}
	// Guarantee the default persona exists.
	hasDefault := false
	for _, p := range s.reg.Personas {
		if p.ID == DefaultPersonaID {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		s.reg.Personas = append([]Persona{defaultPersona()}, s.reg.Personas...)
	}
	if s.reg.Active == "" {
		s.reg.Active = DefaultPersonaID
	}
	s.loaded = true
	return nil
}

// save writes the registry (rows when the vault is attached, otherwise
// the JSON file), creating the parent directory first.
func (s *Store) save() error {
	if s.vault != nil {
		return s.saveVault()
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("persona: create store dir: %w", err)
	}
	data, err := json.MarshalIndent(s.reg, "", "  ")
	if err != nil {
		return fmt.Errorf("persona: marshal registry: %w", err)
	}
	if err := os.WriteFile(s.path(), data, 0o644); err != nil { //nolint:gosec // G306: registry is meant to be user-readable
		return fmt.Errorf("persona: write registry: %w", err)
	}
	return nil
}

// List returns all personas sorted by Created timestamp. The default persona
// is always present (created on first load if missing).
func (s *Store) List() ([]Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := append([]Persona(nil), s.reg.Personas...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out, nil
}

// Get returns the persona with the given ID. Returns ErrNotFound if absent.
func (s *Store) Get(id string) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	for _, p := range s.reg.Personas {
		if p.ID == id {
			return p, nil
		}
	}
	return Persona{}, ErrNotFound
}

// Active returns the active persona ID (defaults to "default").
func (s *Store) Active() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return DefaultPersonaID, err
	}
	return s.reg.Active, nil
}

// SetActive changes the active persona. The persona must already exist.
func (s *Store) SetActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	for _, p := range s.reg.Personas {
		if p.ID == id {
			s.reg.Active = id
			return s.save()
		}
	}
	return ErrNotFound
}

// ActivePersona returns the currently active persona.
func (s *Store) ActivePersona() (Persona, error) {
	id, err := s.Active()
	if err != nil {
		return Persona{}, err
	}
	return s.Get(id)
}

// Input is the writable subset for CreateInput and UpdateProfile. All
// fields are optional on update (zero values keep the stored value); Name
// is required on create.
type Input struct {
	Name                 string
	Tagline              string
	Emoji                string
	Hue                  int
	Prompt               string // system-prompt override
	SystemPrompt         string // alias for Prompt (either is accepted)
	OverrideSystemPrompt *bool  // replace the default system prompt with this persona's prompt
}

// prompt returns the effective prompt override from either alias field.
func (in Input) prompt() string {
	if in.Prompt != "" {
		return in.Prompt
	}
	return in.SystemPrompt
}

// validate enforces the field limits. Called with the merged values that
// will actually be stored.
func validateProfile(name, tagline, emoji, prompt string) error {
	if utf8.RuneCountInString(name) > MaxName {
		return fmt.Errorf("persona: name exceeds %d characters", MaxName)
	}
	if utf8.RuneCountInString(tagline) > MaxTagline {
		return fmt.Errorf("persona: tagline exceeds %d characters", MaxTagline)
	}
	if utf8.RuneCountInString(emoji) > MaxEmoji {
		return fmt.Errorf("persona: emoji exceeds %d characters", MaxEmoji)
	}
	if utf8.RuneCountInString(prompt) > MaxPrompt {
		return fmt.Errorf("persona: prompt exceeds %d characters", MaxPrompt)
	}
	return nil
}

// CreateInput adds a new persona with the full profile (name, tagline,
// emoji, hue, prompt override). ID is derived from Name. The new persona
// becomes the active persona.
func (s *Store) CreateInput(in Input) (Persona, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return Persona{}, ErrNameRequired
	}
	tagline := strings.TrimSpace(in.Tagline)
	emoji := strings.TrimSpace(in.Emoji)
	prompt := strings.TrimSpace(in.prompt())
	if err := validateProfile(name, tagline, emoji, prompt); err != nil {
		return Persona{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	id := slugify(name)
	if id == "" {
		id = "persona"
	}
	id = s.uniqueID(id)
	now := time.Now().UTC()
	p := Persona{
		ID:                   id,
		Name:                 name,
		Tagline:              tagline,
		Description:          tagline, // legacy field kept in sync for older readers
		Emoji:                emoji,
		Hue:                  in.Hue,
		SystemPrompt:         prompt,
		OverrideSystemPrompt: in.OverrideSystemPrompt != nil && *in.OverrideSystemPrompt,
		Created:              now,
		Updated:              now,
	}
	s.reg.Personas = append(s.reg.Personas, p)
	s.reg.Active = id
	if err := s.save(); err != nil {
		return Persona{}, err
	}
	return p, nil
}

// Create adds a new persona (name + description + system-prompt form,
// kept for the TUI command and older callers). ID is derived from Name.
// The new persona becomes the active persona.
func (s *Store) Create(name, description, systemPrompt string) (Persona, error) {
	return s.CreateInput(Input{
		Name:         name,
		Tagline:      description,
		SystemPrompt: systemPrompt,
	})
}

// Update mutates an existing persona's name / description / system-prompt
// fields (legacy signature kept for the TUI command and tests). AvatarExt
// is managed separately via SetAvatar. For the full profile form use
// UpdateProfile.
func (s *Store) Update(id, name, description, systemPrompt string) (Persona, error) {
	return s.UpdateProfile(id, &Input{
		Name:         name,
		Tagline:      description,
		SystemPrompt: systemPrompt,
	})
}

// UpdateProfile mutates an existing persona from a partial Input: zero-
// value fields keep the stored value (matching the old PersonaManager's
// update contract). The default persona's name is locked to "a1" but its
// tagline / emoji / hue / prompt are still editable.
func (s *Store) UpdateProfile(id string, in *Input) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	for i := range s.reg.Personas {
		if s.reg.Personas[i].ID != id {
			continue
		}
		p := &s.reg.Personas[i]
		// Partial-update contract: a missing name keeps the stored value.
		// An explicitly-empty name on a non-default persona is an error.
		if in.Name != "" {
			if id == DefaultPersonaID {
				// The default persona's name is locked to "a1" but the rest
				// of the profile is editable so users who want a different
				// default feel can tweak it.
				p.Name = "a1"
			} else {
				name := strings.TrimSpace(in.Name)
				if name == "" {
					return Persona{}, ErrNameRequired
				}
				p.Name = name
			}
		}
		if in.Tagline != "" {
			p.Tagline = strings.TrimSpace(in.Tagline)
			p.Description = p.Tagline // keep the legacy field in sync
		}
		if in.Emoji != "" {
			p.Emoji = strings.TrimSpace(in.Emoji)
		}
		if in.Hue != 0 {
			p.Hue = in.Hue
		}
		if prompt := strings.TrimSpace(in.prompt()); prompt != "" {
			p.SystemPrompt = prompt
		}
		// Pointer tri-state: nil keeps the stored value, so a client
		// can turn the override OFF with an explicit false (a plain
		// bool could not tell "unset" from "off").
		if in.OverrideSystemPrompt != nil {
			p.OverrideSystemPrompt = *in.OverrideSystemPrompt
		}
		if err := validateProfile(p.Name, p.Tagline, p.Emoji, p.SystemPrompt); err != nil {
			return Persona{}, err
		}
		p.Updated = time.Now().UTC()
		out := *p
		if err := s.save(); err != nil {
			return Persona{}, err
		}
		return out, nil
	}
	return Persona{}, ErrNotFound
}

// UpdateSettings updates the per-persona settings (Model, MemoryEnabled,
// PermissionMode) without touching the persona's identity fields (name,
// tagline, etc.). Used by the apply-on-switch apply path; the TUI/web UI
// persona editor doesn't expose these yet (they can be added to Input in
// a follow-up).
//
// Empty values are no-ops:
//   - model=""        → Model field unchanged
//   - memoryEnabled=nil → MemoryEnabled field unchanged
//   - permissionMode="" → PermissionMode field unchanged
//
// This makes it safe to call with partial updates — the caller only sets
// what they want to change. Returns the updated Persona.
func (s *Store) UpdateSettings(id, model string, memoryEnabled *bool, permissionMode string) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	for i := range s.reg.Personas {
		if s.reg.Personas[i].ID != id {
			continue
		}
		p := &s.reg.Personas[i]
		if model != "" {
			p.Model = model
		}
		if memoryEnabled != nil {
			p.MemoryEnabled = memoryEnabled
		}
		if permissionMode != "" {
			p.PermissionMode = permissionMode
		}
		p.Updated = time.Now().UTC()
		if err := s.save(); err != nil {
			return Persona{}, err
		}
		return *p, nil
	}
	return Persona{}, fmt.Errorf("persona: %s not found", id)
}

// SetSettings sets per-persona settings with explicit-pointer semantics:
// a non-nil pointer sets the field EXACTLY as given (including "" to clear
// an override); nil leaves the field untouched. This is the editor-facing
// counterpart of UpdateSettings — UpdateSettings's "empty string means
// keep" contract cannot express clearing an override, and the persona
// editor's "Default" dropdown entry needs exactly that.
func (s *Store) SetSettings(id string, model, permissionMode *string, memoryEnabled *bool) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	for i := range s.reg.Personas {
		if s.reg.Personas[i].ID != id {
			continue
		}
		p := &s.reg.Personas[i]
		if model != nil {
			p.Model = *model
		}
		if memoryEnabled != nil {
			p.MemoryEnabled = memoryEnabled
		}
		if permissionMode != nil {
			p.PermissionMode = *permissionMode
		}
		p.Updated = time.Now().UTC()
		if err := s.save(); err != nil {
			return Persona{}, err
		}
		return *p, nil
	}
	return Persona{}, fmt.Errorf("persona: %s not found", id)
}

// SetMemoryModel sets the persona's dedicated memory-ops model (Bet 06):
// consolidation traffic routes to it while the user-facing assistant
// stays on Model. Empty = use the main model.
func (s *Store) SetMemoryModel(id, model string) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	for i := range s.reg.Personas {
		if s.reg.Personas[i].ID == id {
			s.reg.Personas[i].MemoryModel = strings.TrimSpace(model)
			s.reg.Personas[i].Updated = time.Now().UTC()
			out := s.reg.Personas[i]
			return out, s.save()
		}
	}
	return Persona{}, fmt.Errorf("persona: %s not found", id)
}

// Delete removes a persona. The default persona cannot be deleted. If the
// active persona is deleted, the active persona reverts to default.
func (s *Store) Delete(id string) error {
	if id == DefaultPersonaID {
		return ErrCannotDeleteDefault
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return err
	}
	if s.vault != nil {
		found := false
		next := make([]Persona, 0, len(s.reg.Personas))
		for _, p := range s.reg.Personas {
			if p.ID == id {
				found = true
				continue
			}
			next = append(next, p)
		}
		if !found {
			return ErrNotFound
		}
		if err := s.deleteVault(id); err != nil {
			return err
		}
		s.reg.Personas = next
		if s.reg.Active == id {
			s.reg.Active = DefaultPersonaID
		}
		return s.saveVault()
	}
	next := s.reg.Personas[:0]
	for _, p := range s.reg.Personas {
		if p.ID == id {
			// Drop the persona's data directory along with the registry entry.
			_ = os.RemoveAll(s.dir(id))
			continue
		}
		next = append(next, p)
	}
	if len(next) == len(s.reg.Personas) {
		return ErrNotFound
	}
	s.reg.Personas = next
	if s.reg.Active == id {
		s.reg.Active = DefaultPersonaID
	}
	return s.save()
}

// SetAvatar writes the avatar image to the persona's directory, updating
// the avatar_ext field. Pass an empty body to clear the avatar.
func (s *Store) SetAvatar(id string, mimeType string, body []byte) (Persona, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Persona{}, err
	}
	idx := -1
	for i := range s.reg.Personas {
		if s.reg.Personas[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Persona{}, ErrNotFound
	}
	p := &s.reg.Personas[idx]
	// Clear existing avatar first if extension is changing.
	if p.AvatarExt != "" {
		_ = os.Remove(filepath.Join(s.dir(p.ID), "avatar."+p.AvatarExt))
	}
	if len(body) == 0 {
		p.AvatarExt = ""
	} else {
		ext, err := extForMIME(mimeType)
		if err != nil {
			return Persona{}, err
		}
		if err := os.MkdirAll(s.dir(p.ID), 0o755); err != nil {
			return Persona{}, fmt.Errorf("persona: create avatar dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(s.dir(p.ID), "avatar."+ext), body, 0o644); err != nil { //nolint:gosec // G306: avatar is user-supplied image
			return Persona{}, fmt.Errorf("persona: write avatar: %w", err)
		}
		p.AvatarExt = ext
	}
	p.Updated = time.Now().UTC()
	out := *p
	if err := s.save(); err != nil {
		return Persona{}, err
	}
	return out, nil
}

// uniqueID returns id if unused, or id-N for the smallest N that makes it unused.
func (s *Store) uniqueID(id string) string {
	used := map[string]bool{}
	for _, p := range s.reg.Personas {
		used[p.ID] = true
	}
	if !used[id] {
		return id
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", id, n)
		if !used[candidate] {
			return candidate
		}
	}
}

// Errors. Errors are values; do not wrap them so callers can use errors.Is.
var (
	ErrNotFound            = errors.New("persona: not found")
	ErrNameRequired        = errors.New("persona: name is required")
	ErrCannotDeleteDefault = errors.New("persona: the default persona cannot be deleted")
)

// slugify produces a lowercase kebab-case ID from a name. Non-ASCII letters
// are kept as-is (Go file paths are UTF-8); only control characters and
// path-separator characters are stripped.
func slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case r == ' ', r == '-', r == '_', r == '/', r == '\\', r == '.':
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// extForMIME maps an image MIME type to a file extension. PNG, JPEG, WebP and
// GIF are accepted (matching the TUI's image attachment pipeline).
func extForMIME(mime string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "png", nil
	case "image/jpeg", "image/jpg":
		return "jpeg", nil
	case "image/webp":
		return "webp", nil
	case "image/gif":
		return "gif", nil
	}
	return "", fmt.Errorf("persona: unsupported avatar MIME type %q (use png/jpeg/webp/gif)", mime)
}
