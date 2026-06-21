package server

import (
	"log/slog"
	"net/http"
)

const csrfCookie = "hn_csrf"

func (s *Server) newCSRF(w http.ResponseWriter) (string, error) {
	val, err := randHex(16)
	if err != nil {
		return "", err
	}
	s.setCookie(w, &http.Cookie{
		Name: csrfCookie, Value: val, Path: "/",
		MaxAge:   0,
		SameSite: http.SameSiteStrictMode, HttpOnly: false,
	})
	return val, nil
}

func (s *Server) csrfToken(w http.ResponseWriter) (string, bool) {
	val, err := s.newCSRF(w)
	if err != nil {
		s.renderCSRFError(w, err)
		return "", false
	}
	return val, true
}

func (s *Server) validateCSRF(r *http.Request, w http.ResponseWriter, val string) (bool, error) {
	if val == "" {
		return false, nil
	}
	c, err := r.Cookie(csrfCookie)
	if err != nil || c.Value != val {
		return false, nil
	}
	if _, err := s.newCSRF(w); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Server) renderCSRFError(w http.ResponseWriter, err error) {
	slog.Error("csrf token generation failed", "err", err)
	http.Error(w, "internal server error", http.StatusInternalServerError)
}
