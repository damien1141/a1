package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(dir)
}

func TestStore_LoadCreatesDefault(t *testing.T) {
	s := tempStore(t)
	ps, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("expected default persona only, got %d", len(ps))
	}
	if ps[0].ID != DefaultPersonaID {
		t.Fatalf("expected default ID, got %q", ps[0].ID)
	}
	if ps[0].Name != "a1" {
		t.Fatalf("default persona name should be 'a1', got %q", ps[0].Name)
	}
}

func TestStore_ActiveDefaultsToDefault(t *testing.T) {
	s := tempStore(t)
	id, err := s.Active()
	if err != nil {
		t.Fatalf("Active: %v", err)
	}
	if id != DefaultPersonaID {
		t.Fatalf("expected default active, got %q", id)
	}
}

func TestStore_CreateSetsActive(t *testing.T) {
	s := tempStore(t)
	p, err := s.Create("Sherlock", "A brilliant detective persona.", "You reason step by step.")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID != "sherlock" {
		t.Fatalf("expected slug 'sherlock', got %q", p.ID)
	}
	active, _ := s.Active()
	if active != "sherlock" {
		t.Fatalf("expected active to be sherlock, got %q", active)
	}
	// Persistence round-trip
	s2 := NewStore(s.root)
	ps, _ := s2.List()
	if len(ps) != 2 {
		t.Fatalf("expected 2 personas after reload, got %d", len(ps))
	}
	got, err := s2.Get("sherlock")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Description != "A brilliant detective persona." {
		t.Fatalf("description did not round-trip: %q", got.Description)
	}
	if got.SystemPrompt != "You reason step by step." {
		t.Fatalf("system prompt did not round-trip: %q", got.SystemPrompt)
	}
}

func TestStore_CreateUniqueID(t *testing.T) {
	s := tempStore(t)
	p1, _ := s.Create("Sage", "", "")
	p2, _ := s.Create("Sage", "", "")
	if p1.ID == p2.ID {
		t.Fatalf("expected unique IDs %q == %q", p1.ID, p2.ID)
	}
}

func TestStore_DefaultNameLocked(t *testing.T) {
	s := tempStore(t)
	p, err := s.Update(DefaultPersonaID, "Renamed", "x", "y")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if p.Name != "a1" {
		t.Fatalf("default persona name should be locked to 'a1', got %q", p.Name)
	}
}

func TestStore_DeleteDefaultRejected(t *testing.T) {
	s := tempStore(t)
	if err := s.Delete(DefaultPersonaID); err == nil {
		t.Fatal("expected error deleting default persona")
	}
}

func TestStore_DeleteActiveRevertsToDefault(t *testing.T) {
	s := tempStore(t)
	p, _ := s.Create("Temp", "", "")
	if err := s.Delete(p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	active, _ := s.Active()
	if active != DefaultPersonaID {
		t.Fatalf("expected active reverted to default, got %q", active)
	}
}

func TestStore_SetAvatar(t *testing.T) {
	s := tempStore(t)
	p, _ := s.Create("Visionary", "", "")
	body := []byte("fake-png-bytes")
	p, err := s.SetAvatar(p.ID, "image/png", body)
	if err != nil {
		t.Fatalf("SetAvatar: %v", err)
	}
	if !p.HasAvatar() {
		t.Fatal("expected HasAvatar=true")
	}
	path := s.AvatarPath(p)
	if path == "" {
		t.Fatal("AvatarPath is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read avatar: %v", err)
	}
	if string(data) != string(body) {
		t.Fatalf("avatar bytes did not round-trip")
	}
	// Reload and verify persistence
	s2 := NewStore(s.root)
	got, _ := s2.Get(p.ID)
	if !got.HasAvatar() {
		t.Fatal("avatar ext did not persist")
	}
}

func TestStore_SetAvatarUnsupportedMIME(t *testing.T) {
	s := tempStore(t)
	p, _ := s.Create("Visionary", "", "")
	_, err := s.SetAvatar(p.ID, "image/bmp", []byte("x"))
	if err == nil {
		t.Fatal("expected error for unsupported MIME")
	}
}

func TestStore_SetAvatarClearsExisting(t *testing.T) {
	s := tempStore(t)
	p, _ := s.Create("Visionary", "", "")
	_, _ = s.SetAvatar(p.ID, "image/png", []byte("png"))
	p, _ = s.SetAvatar(p.ID, "image/jpeg", []byte("jpeg"))
	// PNG should be gone, JPEG should be present
	if _, err := os.Stat(filepath.Join(s.root, p.ID, "avatar.png")); err == nil {
		t.Fatal("old avatar.png should have been removed")
	}
	path := s.AvatarPath(p)
	data, _ := os.ReadFile(path)
	if string(data) != "jpeg" {
		t.Fatalf("expected jpeg bytes, got %q", data)
	}
}

func TestStore_SetAvatarEmptyClears(t *testing.T) {
	s := tempStore(t)
	p, _ := s.Create("Visionary", "", "")
	_, _ = s.SetAvatar(p.ID, "image/png", []byte("png"))
	_, err := s.SetAvatar(p.ID, "", nil)
	if err != nil {
		t.Fatalf("SetAvatar clear: %v", err)
	}
	got, _ := s.Get(p.ID)
	if got.HasAvatar() {
		t.Fatal("expected HasAvatar=false after clear")
	}
}

func TestStore_Slugify(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Sherlock Holmes", "sherlock-holmes"},
		{"  Spaced  Out  ", "spaced-out"},
		{"a/b/c", "a-b-c"},
		{"Already-Slug", "already-slug"},
		{"日本語", ""}, // non-ASCII letters dropped; ID falls back to "persona"
	}
	for _, c := range cases {
		got := slugify(c.in)
		if got != c.want {
			t.Fatalf("slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
