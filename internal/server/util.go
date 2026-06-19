package server

import (
	"bytes"
	"os"
	"path/filepath"
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

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func removeFile(path string) error {
	return os.Remove(path)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func safeBase(s string) string {
	return filepath.Base(s)
}
