package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/puemos/peek/internal/models"
)

type contextKey string

const apiTokenContextKey contextKey = "api-token"

func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		if s.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		// Global rate limit to protect against request floods.
		if !s.globalLimiter.allow(s.clientIP(r)) {
			rateLimitError(w, r)
			return
		}
		reqTotal.Add(1)
		rw := &statusRecorder{ResponseWriter: w, status: 200}
		h.ServeHTTP(rw, r)
		if rw.status >= 400 {
			reqErrors.Add(1)
		}
	})
}

// statusRecorder wraps http.ResponseWriter to capture the status code for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// authToken gates an endpoint behind a bearer token (any valid user token).
func (s *Server) authToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			jsonError(w, http.StatusUnauthorized, "missing token")
			return
		}
		t, err := s.store.GetToken(r.Context(), tok)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if t.Disabled {
			jsonError(w, http.StatusForbidden, "account disabled")
			return
		}
		next(w, withAPIToken(r, t))
	}
}

func (s *Server) authAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			jsonError(w, http.StatusUnauthorized, "missing token")
			return
		}
		t, err := s.store.GetToken(r.Context(), tok)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if t.Disabled {
			jsonError(w, http.StatusForbidden, "account disabled")
			return
		}
		if !t.IsAdmin {
			jsonError(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, withAPIToken(r, t))
	}
}

func withAPIToken(r *http.Request, t *models.Token) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), apiTokenContextKey, t))
}

func apiToken(r *http.Request) (*models.Token, bool) {
	t, ok := r.Context().Value(apiTokenContextKey).(*models.Token)
	return t, ok && t != nil
}

func requireAPIToken(w http.ResponseWriter, r *http.Request) (*models.Token, bool) {
	t, ok := apiToken(r)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "auth context missing")
		return nil, false
	}
	return t, true
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}
