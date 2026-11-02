package clipboard

import "errors"

var (
	// ErrEmpty means the clipboard holds no usable content for the requested kind.
	ErrEmpty = errors.New("clipboard: empty")
	// ErrUnavailable means no image is present or required platform tools are missing.
	ErrUnavailable = errors.New("clipboard: unavailable")
)
