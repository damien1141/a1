package clipboard

import (
	"fmt"
	"net/http"

	imgutil "github.com/damien1141/a1/internal/util/image"
)

// Image is raw image data from the clipboard plus a MIME type.
type Image struct {
	Data     []byte
	MimeType string
}

// ReadImage returns image bytes from the clipboard when available.
func ReadImage() (Image, error) {
	img, err := readClipboardImagePlatform()
	if err != nil {
		return Image{}, err
	}
	if len(img.Data) == 0 {
		return Image{}, ErrUnavailable
	}
	mime := baseMimeType(img.MimeType)
	if mime == "" {
		mime = http.DetectContentType(img.Data)
	}
	if !isSupportedImageMimeType(mime) {
		return Image{}, fmt.Errorf("clipboard: unsupported image type %q", mime)
	}
	if len(img.Data) > imgutil.MaxBytes {
		return Image{}, fmt.Errorf("clipboard: image too large: %d bytes (max %d)", len(img.Data), imgutil.MaxBytes)
	}
	return Image{Data: img.Data, MimeType: mime}, nil
}

// ReadImageResult validates clipboard image bytes the same way as image.Load.
func ReadImageResult() (imgutil.Result, error) {
	img, err := ReadImage()
	if err != nil {
		return imgutil.Result{}, err
	}
	mime := http.DetectContentType(img.Data)
	if !isSupportedImageMimeType(mime) {
		return imgutil.Result{}, fmt.Errorf("clipboard: unsupported image type %q (want png, jpeg, gif, or webp)", mime)
	}
	return imgutil.Result{Data: img.Data, MimeType: mime}, nil
}
