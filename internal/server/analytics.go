package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/puemos/peek/internal/models"
)

// visitorID returns the stable visitor id from the hn_vid cookie, setting a
// fresh long-lived one if absent.
func (s *Server) visitorID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(visitorCookie); err == nil && c.Value != "" {
		return c.Value
	}
	id, err := randID(18)
	if err != nil {
		return "anon"
	}
	s.setCookie(w, &http.Cookie{
		Name:     visitorCookie,
		Value:    id,
		Path:     "/",
		MaxAge:   int((10 * 365 * 24 * time.Hour).Seconds()),
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
	})
	return id
}

// recordVisit logs a page view, hashing the IP for privacy. vid is the visitor
// id already resolved (and cookie set) by the page handler.
func (s *Server) recordVisit(r *http.Request, u *models.Upload, vid string) {
	name := ""
	if c, err := r.Cookie(nameCookie); err == nil {
		name = strings.TrimSpace(c.Value)
	}
	ip := s.clientIP(r)
	h := sha256.Sum256([]byte(s.secret + "|" + ip))
	ipHash := hex.EncodeToString(h[:])[:16]
	if ip == "" {
		ipHash = ""
	}
	ua := r.Header.Get("User-Agent")
	if len(ua) > 300 {
		ua = ua[:300]
	}
	_ = s.store.RecordVisit(u.ID, vid, name, ipHash, ua)
	if name != "" {
		_ = s.store.UpsertVisitor(vid, name)
	}
}

func (s *Server) clientIP(r *http.Request) string {
	if s.trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		addr = addr[:i]
	}
	return strings.Trim(addr, "[]")
}
