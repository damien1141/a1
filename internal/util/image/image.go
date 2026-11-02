// Package image loads image files from disk: raw bytes plus a content-sniffed
// MIME type. Callers (e.g. the TUI attach path) base64-encode the bytes into
// their own message shape.
package image

import (
	"fmt"
	"net/http"
	"os"
)

// MaxBytes caps how large an attached image may be. Base64 inflates the
// payload by ~33%, so an oversized file would dominate the context window.
const MaxBytes = 10 << 20 // 10 MiB

// Result is a loaded image file.
type Result struct {
	Data     []byte // raw file bytes
	MimeType string // sniffed from content, e.g. "image/png"
}

// supportedMimeTypes is the set of raster formats vision APIs accept.
var supportedMimeTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// Load reads an image file, sniffs its MIME type from the content (not the
// extension), and returns the raw bytes plus that type.
func Load(path string) (Result, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	if len(data) > MaxBytes {
		return Result{}, fmt.Errorf("image too large: %d bytes (max %d)", len(data), MaxBytes)
	}
	mime := http.DetectContentType(data)
	if !supportedMimeTypes[mime] {
		return Result{}, fmt.Errorf("unsupported image type %q (want png, jpeg, gif, or webp)", mime)
	}
	return Result{Data: data, MimeType: mime}, nil
}
