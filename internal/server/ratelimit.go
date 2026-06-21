package server

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a small fixed-window per-key rate limiter. It is intended for
// abuse mitigation (login brute force, comment spam) on a single-node,
// self-hosted deployment — not distributed quota enforcement.
type limiter struct {
	mu     sync.Mutex
	hits   map[string]*window
	max    int
	window time.Duration
	now    func() time.Time
}

type window struct {
	count int
	reset time.Time
}

func newLimiter(max int, win time.Duration) *limiter {
	return &limiter{hits: make(map[string]*window), max: max, window: win, now: time.Now}
}

func (l *limiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.hits) > 10000 {
		for k, w := range l.hits {
			if now.After(w.reset) {
				delete(l.hits, k)
			}
		}
	}
	w := l.hits[key]
	if w == nil || now.After(w.reset) {
		l.hits[key] = &window{count: 1, reset: now.Add(l.window)}
		return true
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	return true
}

// rateLimit wraps a handler with a per-client-IP limit (HTML-style 429).
func (s *Server) rateLimit(l *limiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(s.clientIP(r)) {
			rateLimitError(w, r)
			return
		}
		next(w, r)
	}
}

func rateLimitError(w http.ResponseWriter, r *http.Request) {
	if wantsJSONError(r) {
		jsonError(w, http.StatusTooManyRequests, "too many requests, try again shortly")
		return
	}
	http.Error(w, "Too many requests. Try again shortly.", http.StatusTooManyRequests)
}

func wantsJSONError(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json")
}
