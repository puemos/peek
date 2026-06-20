package server

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/puemos/peek/internal/models"
)

// looksLikeHTML sanity-checks that bytes are textual and HTML-ish, rejecting
// binary content and obvious non-HTML uploads.
func looksLikeHTML(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// Reject binary: null bytes or invalid UTF-8.
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return false
	}
	head := strings.ToLower(string(b[:min(len(b), 1024)]))
	if strings.Contains(head, "<!doctype html") ||
		strings.Contains(head, "<html") ||
		strings.Contains(head, "<body") ||
		strings.Contains(head, "<head") ||
		strings.Contains(head, "<div") ||
		strings.Contains(head, "<p>") ||
		strings.Contains(head, "<p ") ||
		strings.Contains(head, "<span") {
		return true
	}
	// Fallback: must contain at least one tag-like sequence.
	return strings.Contains(head, "<") && strings.Contains(head, ">")
}

const bcryptMaxLen = 72

// validatePasswordLength rejects passwords that exceed bcrypt's 72-byte limit,
// which would be silently truncated otherwise.
func validatePasswordLength(pw string) bool {
	return len(pw) <= bcryptMaxLen
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const slugRetries = 5

// generateSlug calls randID until a slug that is not already in the store is
// produced. Each attempt generates a fresh 10-byte (≈13 char) random id.
func generateSlug(store slugChecker) (string, error) {
	for range slugRetries {
		s, err := randID(10)
		if err != nil {
			return "", err
		}
		if _, err := store.GetUpload(s); err != nil {
			return s, nil
		}
	}
	return "", errors.New("slug collision after retries")
}

type slugChecker interface {
	GetUpload(slug string) (*models.Upload, error)
}
