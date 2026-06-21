package db

import "strings"

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
