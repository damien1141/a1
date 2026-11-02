package image

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// png1x1Base64 is a 1x1 transparent PNG.
const png1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestLoadPNG(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(png1x1Base64)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "pixel.png")
	require.NoError(t, os.WriteFile(path, png, 0o644))

	res, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "image/png", res.MimeType)
	assert.Equal(t, png, res.Data)
}

func TestLoadRejectsNonImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	_, err := Load(path)
	assert.ErrorContains(t, err, "unsupported image type")
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.png"))
	assert.Error(t, err)
}
