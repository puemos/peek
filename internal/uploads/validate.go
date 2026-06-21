package uploads

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

func looksLikeHTML(b []byte) bool {
	if len(b) == 0 {
		return false
	}
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
	return strings.Contains(head, "<") && strings.Contains(head, ">")
}

const bcryptMaxLen = 72

func ValidatePasswordLength(pw string) bool {
	return len(pw) <= bcryptMaxLen
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
