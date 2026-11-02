package clipboard

import (
	"slices"
	"strings"
)

var preferredImageMimes = []string{
	"image/png",
	"image/jpeg",
	"image/webp",
	"image/gif",
}

func baseMimeType(mimeType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
}

func selectPreferredImageMimeType(types []string) string {
	normalized := make([]struct{ raw, base string }, 0, len(types))
	for _, t := range types {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		normalized = append(normalized, struct{ raw, base string }{raw: t, base: baseMimeType(t)})
	}
	for _, preferred := range preferredImageMimes {
		for _, t := range normalized {
			if t.base == preferred {
				return t.raw
			}
		}
	}
	for _, t := range normalized {
		if strings.HasPrefix(t.base, "image/") {
			return t.raw
		}
	}
	return ""
}

func isSupportedImageMimeType(mimeType string) bool {
	return slices.Contains(preferredImageMimes, baseMimeType(mimeType))
}

// ExtensionForImageMimeType maps a MIME type to a common file extension.
func ExtensionForImageMimeType(mimeType string) string {
	switch baseMimeType(mimeType) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	default:
		return ""
	}
}
