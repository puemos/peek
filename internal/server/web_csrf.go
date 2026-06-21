package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

const csrfCookie = "hn_csrf"

func (s *Server) newCSRF(w http.ResponseWriter) string {
	b := make([]byte, 16)
	rand.Read(b)
	val := hex.EncodeToString(b)
	s.setCookie(w, &http.Cookie{
		Name: csrfCookie, Value: val, Path: "/",
		MaxAge:   0,
		SameSite: http.SameSiteStrictMode, HttpOnly: false,
	})
	return val
}

func (s *Server) validateCSRF(r *http.Request, w http.ResponseWriter, val string) bool {
	if val == "" {
		return false
	}
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value != val {
		return false
	}
	s.newCSRF(w)
	return true
}
