//go:build !windows

package clipboard

import "errors"

func copyTextWindows(string) error {
	return errors.New("clipboard: Windows text backend is unavailable on this platform")
}
