package server

import (
	"bytes"
	"strings"
	"unicode/utf8"
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
