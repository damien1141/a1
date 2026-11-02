package image

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToLLM(t *testing.T) {
	r := Result{Data: []byte("abc"), MimeType: "image/png"}
	got := ToLLM(r)
	assert.Equal(t, "image/png", got.MimeType)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("abc")), got.Data)
}

func TestAttachmentLabel(t *testing.T) {
	assert.Equal(t, "foo.png", AttachmentLabel("/tmp/foo.png"))
}

func TestAttachmentFromResult(t *testing.T) {
	r := Result{Data: []byte("x"), MimeType: "image/jpeg"}
	att := AttachmentFromResult("", r)
	assert.Equal(t, "image", att.Label)
	require.Equal(t, r, att.Result)
}
