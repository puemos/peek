package cli

import (
	"fmt"
	"os"
)

// --- formatting helpers ---

func humanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return t
	}
	return t[:4] + "…" + t[len(t)-4:]
}

func envNote() string {
	host := os.Getenv("PEEK_HOST")
	tok := os.Getenv("PEEK_TOKEN")
	if host != "" || tok != "" {
		return "  (env override active)"
	}
	return ""
}
