package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/puemos/peek/internal/db"
)

const (
	viewTokenTTL  = 1 * time.Hour
	sessionTTL    = 7 * 24 * time.Hour
	visitorCookie = "hn_vid"
	nameCookie    = "hn_name"
	defaultMax    = 2 << 20 // 2 MiB
)

type Config struct {
	Addr       string
	DataDir    string
	BaseURL    string
	AdminToken string
	Secret     string
	MaxUpload  int64
}

type Server struct {
	store      *db.Store
	secret     string
	baseURL    string
	uploadsDir string
	maxUpload  int64
	secure     bool // serve cookies with the Secure flag (https base URL)

	loginLimiter   *limiter
	commentLimiter *limiter
}

func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(filepath.Join(cfg.DataDir, "uploads"), 0o755); err != nil {
		return nil, err
	}
	secret := cfg.Secret
	if secret == "" {
		secret = loadOrCreateSecret(filepath.Join(cfg.DataDir, "secret.key"))
	}
	store, err := db.Open(filepath.Join(cfg.DataDir, "peek.db"))
	if err != nil {
		return nil, err
	}
	if err := bootstrapTokens(store, cfg.AdminToken); err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	max := cfg.MaxUpload
	if max <= 0 {
		max = defaultMax
	}
	secure := strings.HasPrefix(baseURL, "https://")
	if !secure && !isLocalBaseURL(baseURL) {
		log.Printf("WARNING: base URL %q is not https. Bearer tokens and session cookies will be sent in clear — put peek behind a TLS reverse proxy (e.g. Caddy/nginx) for any non-local deployment.", baseURL)
	}
	return &Server{
		store:          store,
		secret:         secret,
		baseURL:        baseURL,
		uploadsDir:     filepath.Join(cfg.DataDir, "uploads"),
		maxUpload:      max,
		secure:         secure,
		loginLimiter:   newLimiter(10, time.Minute),
		commentLimiter: newLimiter(30, time.Minute),
	}, nil
}

func isLocalBaseURL(u string) bool {
	return strings.Contains(u, "localhost") || strings.Contains(u, "127.0.0.1") || strings.Contains(u, "[::1]")
}

// setCookie applies the deployment-wide Secure flag, then writes the cookie.
func (s *Server) setCookie(w http.ResponseWriter, c *http.Cookie) {
	c.Secure = s.secure
	http.SetCookie(w, c)
}

func loadOrCreateSecret(path string) string {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return string(b)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("generating secret: %v", err)
	}
	s := hex.EncodeToString(b)
	_ = os.WriteFile(path, []byte(s), 0o600)
	return s
}

// bootstrapTokens ensures at least one admin token exists.
func bootstrapTokens(store *db.Store, adminToken string) error {
	n, err := store.CountTokens()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if adminToken == "" {
		t, err := randID(24)
		if err != nil {
			return err
		}
		adminToken = t
	}
	if err := store.CreateToken(adminToken, "admin", true); err != nil {
		return err
	}
	log.Printf("==========================================================")
	log.Printf(" Created admin token (save it now, it is stored in the DB):")
	log.Printf("   %s", adminToken)
	log.Printf(" Use it with the CLI:  peek login --host <url>   (paste this token when prompted)")
	log.Printf("==========================================================")
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Trusted, same-origin JSON API (token-gated where noted).
	mux.HandleFunc("POST /api/upload", s.authToken(s.handleUpload))
	mux.HandleFunc("GET /api/uploads", s.authToken(s.handleListUploads))
	mux.HandleFunc("DELETE /api/uploads/{slug}", s.authToken(s.handleDeleteUpload))
	mux.HandleFunc("POST /api/uploads/{slug}/password", s.authToken(s.handleSetPassword))
	mux.HandleFunc("GET /api/uploads/{slug}/stats", s.authToken(s.handleStats))
	mux.HandleFunc("POST /api/tokens", s.authAdmin(s.handleCreateToken))
	mux.HandleFunc("GET /api/tokens", s.authAdmin(s.handleListTokens))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.authAdmin(s.handleDeleteToken))

	// Page-side API (callable by the trusted parent page JS).
	mux.HandleFunc("GET /api/uploads/{slug}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/uploads/{slug}/comments", s.handleAddComment)

	// Pages & assets.
	mux.HandleFunc("GET /p/{slug}", s.handlePage)
	mux.HandleFunc("POST /p/{slug}", s.rateLimit(s.loginLimiter, s.handlePagePassword))
	mux.HandleFunc("GET /raw/{slug}", s.handleRaw)
	mux.HandleFunc("GET /bridge.js", s.handleBridge)
	mux.HandleFunc("GET /app.js", s.handleApp)
	mux.HandleFunc("GET /style.css", s.handleStyle)
	mux.HandleFunc("GET /dashboard.css", s.handleDashboardCSS)
	mux.HandleFunc("GET /", s.handleIndex)

	// Web GUI (browser-based management).
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("POST /login", s.rateLimit(s.loginLimiter, s.handleLogin))
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /dashboard", s.handleDashboard)
	mux.HandleFunc("POST /dashboard/upload", s.handleDashboardUpload)
	mux.HandleFunc("POST /dashboard/delete/{slug}", s.handleDashboardDelete)
	mux.HandleFunc("GET /dashboard/stats/{slug}", s.handleDashboardStats)

	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if s.secure {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		h.ServeHTTP(w, r)
	})
}

// authToken gates an endpoint behind a bearer token (any valid user token).
func (s *Server) authToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			jsonError(w, http.StatusUnauthorized, "missing token")
			return
		}
		if _, err := s.store.GetToken(tok); err != nil {
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		next(w, r)
	}
}

func (s *Server) authAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			jsonError(w, http.StatusUnauthorized, "missing token")
			return
		}
		t, err := s.store.GetToken(tok)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		if !t.IsAdmin {
			jsonError(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	}
}

func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	return ""
}

// uploadPath returns the on-disk path for a slug.
func (s *Server) uploadPath(slug string) string {
	return filepath.Join(s.uploadsDir, slug+".html")
}

var errMissing = errors.New("not found")

// readUploadFile reads & sanity-checks size of an uploaded file.
func (s *Server) readUploadFile(slug string) ([]byte, error) {
	p := s.uploadPath(slug)
	f, err := os.Open(p)
	if err != nil {
		return nil, errMissing
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return b, nil
}
