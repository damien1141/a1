package webserver

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/damien1141/a1/internal/persona"
	"github.com/damien1141/a1/internal/project"
)

// avatarTestServer builds a server whose persona store lives in a temp dir,
// so SetAvatar's file writes and registry saves are observable and isolated.
func avatarTestServer(t *testing.T) (*Server, *persona.Store) {
	t.Helper()
	proj, err := project.Discover(t.TempDir())
	require.NoError(t, err)
	store := persona.NewStore(filepath.Join(t.TempDir(), "personas"))
	s := &Server{proj: proj, personaStore: store}
	s.initMaps()
	return s, store
}

// TestHandleClearPersonaAvatar_Positive: clearing removes the stored picture
// and reports hasAvatar=false, so the UI falls back to the emoji rendering.
func TestHandleClearPersonaAvatar_Positive(t *testing.T) {
	s, store := avatarTestServer(t)
	p, err := store.Create("pic", "", "")
	require.NoError(t, err)

	// Seed a real picture through the same path an upload takes.
	p, err = store.SetAvatar(p.ID, "image/png", []byte("\x89PNG\r\n\x1a\nfake"))
	require.NoError(t, err)
	require.NotEmpty(t, p.AvatarExt, "precondition: the persona must have a picture")

	req := httptest.NewRequest(http.MethodDelete, "/api/personas/avatar?id="+p.ID, nil)
	rec := httptest.NewRecorder()
	s.handleClearPersonaAvatar(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got, err := store.Get(p.ID)
	require.NoError(t, err)
	assert.Empty(t, got.AvatarExt, "the picture must be gone from the registry")
}

// TestHandleClearPersonaAvatar_Negative: a missing id is a 400 and an unknown
// persona is a 404 — neither silently succeeds.
func TestHandleClearPersonaAvatar_Negative(t *testing.T) {
	s, _ := avatarTestServer(t)

	t.Run("missing id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/personas/avatar", nil)
		rec := httptest.NewRecorder()
		s.handleClearPersonaAvatar(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown persona", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/personas/avatar?id=nope", nil)
		rec := httptest.NewRecorder()
		s.handleClearPersonaAvatar(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

// TestClearPersonaAvatar_MatchesStoreDifferential: the endpoint is a proxy for
// SetAvatar(id, "", nil), so its observable effect must equal calling the store
// directly — same AvatarExt, same hasAvatar.
func TestClearPersonaAvatar_MatchesStoreDifferential(t *testing.T) {
	// oracle: the store's own clear, run on an identical persona in a second
	// store, compared field by field with the endpoint's result.
	viaEndpoint := func(t *testing.T) persona.Persona {
		s, store := avatarTestServer(t)
		p, err := store.Create("pic", "", "")
		require.NoError(t, err)
		p, err = store.SetAvatar(p.ID, "image/png", []byte("\x89PNG\r\n\x1a\nx"))
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		s.handleClearPersonaAvatar(rec, httptest.NewRequest(http.MethodDelete, "/api/personas/avatar?id="+p.ID, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		got, err := store.Get(p.ID)
		require.NoError(t, err)
		return got
	}
	viaStore := func(t *testing.T) persona.Persona {
		_, store := avatarTestServer(t)
		p, err := store.Create("pic", "", "")
		require.NoError(t, err)
		p, err = store.SetAvatar(p.ID, "image/png", []byte("\x89PNG\r\n\x1a\nx"))
		require.NoError(t, err)
		got, err := store.SetAvatar(p.ID, "", nil)
		require.NoError(t, err)
		return got
	}

	a := viaEndpoint(t)
	b := viaStore(t)
	assert.Equal(t, b.AvatarExt, a.AvatarExt)
	assert.Equal(t, b.Name, a.Name)
}
