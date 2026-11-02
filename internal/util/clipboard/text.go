package clipboard

import (
	"errors"
	"runtime"
	"strings"
)

// CopyText writes plain text to the system clipboard.
func CopyText(text string) error {
	if strings.TrimSpace(text) == "" {
		return ErrEmpty
	}
	switch runtime.GOOS {
	case "darwin":
		return pipeToCommand("pbcopy", []byte(text))
	case "windows":
		return copyTextWindows(text)
	default:
		if lookPath("wl-copy") {
			if err := pipeToCommand("wl-copy", []byte(text)); err == nil {
				return nil
			}
		}
		if lookPath("xclip") {
			return pipeToCommand("xclip", []byte(text), "-selection", "clipboard")
		}
		return errors.New("clipboard: copy text: install wl-clipboard or xclip")
	}
}
