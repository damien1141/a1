//go:build !linux && !darwin && !windows

package clipboard

func readClipboardImagePlatform() (Image, error) {
	return Image{}, ErrUnavailable
}
