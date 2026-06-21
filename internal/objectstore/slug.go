package objectstore

import (
	"errors"
	"fmt"
)

var ErrInvalidSlug = errors.New("invalid object slug")

func objectName(slug string) (string, error) {
	if !validObjectSlug(slug) {
		return "", fmt.Errorf("%w: %q", ErrInvalidSlug, slug)
	}
	return slug + ".html", nil
}

func validObjectSlug(slug string) bool {
	if slug == "" {
		return false
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}
