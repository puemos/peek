package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/puemos/peek/internal/db"
)

const (
	viewTokenTTL  = 1 * time.Hour
	sessionTTL    = 7 * 24 * time.Hour
	visitorCookie = "hn_vid"
	nameCookie    = "hn_name"
)

type Config struct {
	Addr       string
	DataDir    string
	BaseURL    string
	AdminToken string
	Secret     string
	MaxUpload  int64

	Storage     string
	S3Endpoint  string
	S3Bucket    string
	S3Region    string
	S3AccessKey string
	S3SecretKey string

	MaxTotalSize  int64
	RetentionDays int

	TrustedProxy bool
}

type Server struct {
	store    *db.Store
	secret   string
	baseURL  string
	storage  Storage
	secure   bool

	loginLimiter   *limiter
	commentLimiter *limiter

	trustedProxy bool
}

func New(cfg Config) (*Server, error) {
	if cfg.Storage == "s3" {
		if cfg.S3Bucket == "" || cfg.S3Endpoint == "" {
			return nil, errors.New("s3 storage requires --s3-bucket and --s3-endpoint (or set via dashboard after first run)")
		}
	} else {
		cfg.Storage = "file"
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
	s3Defaults := map[string]string{
		"storage":       cfg.Storage,
		"s3_endpoint":   cfg.S3Endpoint,
		"s3_bucket":     cfg.S3Bucket,
		"s3_region":     cfg.S3Region,
		"s3_access_key": cfg.S3AccessKey,
		"s3_secret_key": cfg.S3SecretKey,
	}
	if err := initDefaultSettings(store, secret, cfg.MaxUpload, cfg.MaxTotalSize, cfg.RetentionDays, s3Defaults); err != nil {
		return nil, err
	}

	storageBackend, _ := store.GetSetting("storage")
	if storageBackend == "" {
		storageBackend = cfg.Storage
	}

	var st Storage
	if storageBackend == "s3" {
		st = NewS3Storage(secret, func(key string) string {
			v, _ := store.GetSetting(key)
			return v
		})
		log.Printf("storage: s3 backend (config managed via settings API / dashboard)")
	} else {
		uploadsDir := filepath.Join(cfg.DataDir, "uploads")
		if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
			return nil, err
		}
		st = &FileStorage{Dir: uploadsDir}
		log.Printf("storage: file backend (dir=%s)", uploadsDir)
	}

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	secure := strings.HasPrefix(baseURL, "https://")
	if !secure && !isLocalBaseURL(baseURL) {
		log.Printf("WARNING: base URL %q is not https. Bearer tokens and session cookies will be sent in clear — put peek behind a TLS reverse proxy (e.g. Caddy/nginx) for any non-local deployment.", baseURL)
	}

	srv := &Server{
		store:          store,
		secret:         secret,
		baseURL:        baseURL,
		storage:        st,
		secure:         secure,
		loginLimiter:   newLimiter(10, time.Minute),
		commentLimiter: newLimiter(30, time.Minute),
		trustedProxy:   cfg.TrustedProxy,
	}

	if !cfg.TrustedProxy && !isLocalBaseURL(baseURL) {
		log.Printf("WARNING: --trusted-proxy is not set. X-Forwarded-For headers will be ignored for rate limiting. Set --trusted-proxy if peek runs behind a reverse proxy (Caddy/nginx).")
	}

	go srv.startRetentionCleanup()

	return srv, nil
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
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		b := make([]byte, 128)
		n, _ := f.Read(b)
		if n >= 32 {
			return string(b[:n])
		}
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
	mux.HandleFunc("GET /api/settings", s.authAdmin(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.authAdmin(s.handleUpdateSettings))

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
	mux.HandleFunc("POST /dashboard/settings", s.handleDashboardSettings)
	mux.HandleFunc("GET /dashboard/stats/{slug}", s.handleDashboardStats)

	return s.withMiddleware(mux)
}

func (s *Server) withMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
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

func initDefaultSettings(store *db.Store, secret string, maxUpload, maxTotalSize int64, retentionDays int, s3Defaults map[string]string) error {
	upsert := func(key, val string) error {
		_, err := store.GetSetting(key)
		if err == nil {
			return nil
		}
		if secretSettingKeys[key] && secret != "" && val != "" {
			enc, err := encryptSecret(secret, val)
			if err != nil {
				return err
			}
			val = enc
		}
		return store.SetSetting(key, val)
	}
	_ = upsert("max_upload", strconv.FormatInt(maxUpload, 10))
	_ = upsert("max_total_size", strconv.FormatInt(maxTotalSize, 10))
	_ = upsert("retention_days", strconv.Itoa(retentionDays))
	for k, v := range s3Defaults {
		if v != "" {
			_ = upsert(k, v)
		}
	}
	return nil
}

func (s *Server) settingInt64(key string, def int64) int64 {
	v, err := s.encryptedGetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) settingInt(key string, def int) int {
	v, err := s.encryptedGetSetting(key)
	if err != nil || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) encryptedGetSetting(key string) (string, error) {
	v, err := s.store.GetSetting(key)
	if err != nil {
		return "", err
	}
	if secretSettingKeys[key] {
		return decryptSecret(s.secret, v)
	}
	return v, nil
}

func (s *Server) encryptedSetSetting(key, val string) error {
	if secretSettingKeys[key] && val != "" {
		enc, err := encryptSecret(s.secret, val)
		if err != nil {
			return err
		}
		val = enc
	}
	return s.store.SetSetting(key, val)
}

func (s *Server) encryptedGetAllSettings() (map[string]string, error) {
	raw, err := s.store.GetAllSettings()
	if err != nil {
		return nil, err
	}
	for k, v := range raw {
		if secretSettingKeys[k] {
			dec, err := decryptSecret(s.secret, v)
			if err != nil {
				raw[k] = ""
			} else {
				raw[k] = dec
			}
		}
	}
	return raw, nil
}

func (s *Server) audit(format string, args ...any) {
	log.Printf("[audit] "+format, args...)
}

func (s *Server) startRetentionCleanup() {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		s.cleanupExpired()
	}
}

func (s *Server) cleanupExpired() {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	uploads, err := s.store.ListUploadsOlderThan(cutoff)
	if err != nil {
		log.Printf("retention cleanup: list: %v", err)
		return
	}
	for _, u := range uploads {
		if err := s.storage.Delete(context.Background(), u.Slug); err != nil {
			log.Printf("retention cleanup: storage delete %s: %v", u.Slug, err)
		}
		if err := s.store.DeleteUpload(u.ID); err != nil {
			log.Printf("retention cleanup: db delete %s: %v", u.Slug, err)
		}
	}
	if len(uploads) > 0 {
		log.Printf("retention cleanup: removed %d expired uploads (cutoff %s)", len(uploads), cutoff.Format(time.DateOnly))
	}
}
