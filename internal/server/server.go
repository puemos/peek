package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	Addr      string
	DataDir   string
	BaseURL   string
	Secret    string
	MaxUpload int64

	Storage                string
	S3Endpoint             string
	S3Bucket               string
	S3Region               string
	S3AccessKey            string
	S3SecretKey            string
	S3AllowPrivateEndpoint bool

	MaxTotalSize  int64
	RetentionDays int

	TrustedProxy bool
}

type Server struct {
	store   *db.Store
	secret  string
	baseURL string
	dataDir string
	storage Storage
	secure  bool

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	loginLimiter    *limiter
	commentLimiter  *limiter
	uploadLimiter   *limiter
	passwordLimiter *limiter
	globalLimiter   *limiter
	cliLoginLimiter *limiter

	trustedProxy           bool
	s3AllowPrivateEndpoint bool
	visitQueue             chan visitEvent
}

func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
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
	closeStoreOnError := true
	defer func() {
		if closeStoreOnError {
			_ = store.Close()
		}
	}()

	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if _, err := bootstrapSetup(store, cfg.DataDir, baseURL); err != nil {
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
		if endpoint, err := store.GetSetting("s3_endpoint"); err == nil && endpoint != "" {
			if err := validateS3Endpoint(endpoint, cfg.S3AllowPrivateEndpoint); err != nil {
				return nil, err
			}
		}
		st = NewS3Storage(secret, cfg.S3AllowPrivateEndpoint, func(key string) string {
			v, _ := store.GetSetting(key)
			return v
		})
		slog.Info("storage backend: s3 (config managed via settings API / dashboard)")
	} else {
		uploadsDir := filepath.Join(cfg.DataDir, "uploads")
		if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
			return nil, err
		}
		st = &FileStorage{Dir: uploadsDir}
		slog.Info("storage backend: file", "dir", uploadsDir)
	}

	secure := strings.HasPrefix(baseURL, "https://")
	if !secure && !isLocalBaseURL(baseURL) {
		slog.Warn("base URL is not https — tokens and cookies sent in clear. Use a TLS reverse proxy.", "base_url", baseURL)
	}

	ctx, cancel := context.WithCancel(context.Background())
	srv := &Server{
		store:                  store,
		secret:                 secret,
		baseURL:                baseURL,
		dataDir:                cfg.DataDir,
		storage:                st,
		secure:                 secure,
		ctx:                    ctx,
		cancel:                 cancel,
		loginLimiter:           newLimiter(10, time.Minute),
		commentLimiter:         newLimiter(30, time.Minute),
		uploadLimiter:          newLimiter(20, time.Minute),
		passwordLimiter:        newLimiter(10, time.Minute),
		globalLimiter:          newLimiter(300, time.Minute),
		cliLoginLimiter:        newLimiter(120, time.Minute),
		trustedProxy:           cfg.TrustedProxy,
		s3AllowPrivateEndpoint: cfg.S3AllowPrivateEndpoint,
		visitQueue:             make(chan visitEvent, 256),
	}

	if !cfg.TrustedProxy && !isLocalBaseURL(baseURL) {
		slog.Warn("trusted-proxy not set — X-Forwarded-For will be ignored. Enable if behind a reverse proxy.")
	}

	srv.wg.Add(2)
	go func() {
		defer srv.wg.Done()
		srv.startRetentionCleanup(ctx)
	}()
	go func() {
		defer srv.wg.Done()
		srv.startVisitWorker(ctx)
	}()

	closeStoreOnError = false
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
		panic("generating secret: " + err.Error())
	}
	s := hex.EncodeToString(b)
	_ = os.WriteFile(path, []byte(s), 0o600)
	return s
}

const setupCodeFile = "setup.key"

func setupCodePath(dataDir string) string {
	return filepath.Join(dataDir, setupCodeFile)
}

// bootstrapSetup prepares a one-time setup URL when the database has no
// accounts. The first admin is created through /setup, not through a token.
func bootstrapSetup(store *db.Store, dataDir, baseURL string) (string, error) {
	n, err := store.CountAccounts()
	if err != nil {
		return "", err
	}
	if n > 0 {
		_ = os.Remove(setupCodePath(dataDir))
		return "", nil
	}

	path := setupCodePath(dataDir)
	if b, err := os.ReadFile(path); err == nil {
		code := strings.TrimSpace(string(b))
		if code != "" {
			printSetupURL(baseURL, code)
			return code, nil
		}
	}
	code, err := randID(24)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(code+"\n"), 0o600); err != nil {
		return "", err
	}
	printSetupURL(baseURL, code)
	return code, nil
}

func printSetupURL(baseURL, code string) {
	fmt.Println("==========================================================")
	fmt.Println(" Peek first-run setup")
	fmt.Println(" Open this URL to create the first admin account:")
	fmt.Printf("   %s/setup?code=%s\n", baseURL, url.QueryEscape(code))
	fmt.Println("==========================================================")
}

func (s *Server) setupRequired() bool {
	n, err := s.store.CountAccounts()
	return err == nil && n == 0
}

func (s *Server) readSetupCode() string {
	b, err := os.ReadFile(setupCodePath(s.dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (s *Server) validSetupCode(code string) bool {
	if !s.setupRequired() {
		return false
	}
	want := s.readSetupCode()
	if want == "" || strings.TrimSpace(code) != want {
		return false
	}
	return true
}

func (s *Server) clearSetupCode() {
	_ = os.Remove(setupCodePath(s.dataDir))
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Trusted, same-origin JSON API (token-gated where noted).
	mux.HandleFunc("POST /api/upload", s.rateLimit(s.uploadLimiter, s.authToken(s.handleUpload)))
	mux.HandleFunc("GET /api/uploads", s.authToken(s.handleListUploads))
	mux.HandleFunc("DELETE /api/uploads/{slug}", s.authToken(s.handleDeleteUpload))
	mux.HandleFunc("POST /api/uploads/{slug}/password", s.authToken(s.handleSetPassword))
	mux.HandleFunc("GET /api/uploads/{slug}/stats", s.authToken(s.handleStats))
	mux.HandleFunc("POST /api/tokens", s.authAdmin(s.handleCreateToken))
	mux.HandleFunc("GET /api/tokens", s.authAdmin(s.handleListTokens))
	mux.HandleFunc("DELETE /api/tokens/{id}", s.authAdmin(s.handleDeleteToken))
	mux.HandleFunc("GET /api/settings", s.authAdmin(s.handleGetSettings))
	mux.HandleFunc("PUT /api/settings", s.authAdmin(s.handleUpdateSettings))
	mux.HandleFunc("GET /api/audit", s.authAdmin(s.handleAuditLog))
	mux.HandleFunc("GET /api/uploads/{slug}/export", s.authToken(s.handleExportUpload))
	mux.HandleFunc("DELETE /api/uploads-by-owner", s.authToken(s.handleDeleteAllByOwner))
	mux.HandleFunc("GET /api/auth/providers", s.handleAuthProviders)
	mux.HandleFunc("POST /api/cli/login/start", s.rateLimit(s.cliLoginLimiter, s.handleCLILoginStart))
	mux.HandleFunc("POST /api/cli/login/poll", s.rateLimit(s.cliLoginLimiter, s.handleCLILoginPoll))

	// Page-side API (callable by the trusted parent page JS).
	mux.HandleFunc("GET /api/uploads/{slug}/comments", s.handleListComments)
	mux.HandleFunc("POST /api/uploads/{slug}/comments", s.handleAddComment)

	// Pages & assets.
	mux.HandleFunc("GET /p/{slug}", s.handlePage)
	mux.HandleFunc("POST /p/{slug}", s.rateLimit(s.passwordLimiter, s.handlePagePassword))
	mux.HandleFunc("GET /raw/{slug}", s.handleRaw)
	mux.HandleFunc("GET /bridge.js", s.handleBridge)
	mux.HandleFunc("GET /app.js", s.handleApp)
	mux.HandleFunc("GET /dashboard.js", s.handleDashboardJS)
	mux.HandleFunc("GET /style.css", s.handleStyle)
	mux.HandleFunc("GET /dashboard.css", s.handleDashboardCSS)
	mux.HandleFunc("GET /", s.handleIndex)

	// Health checks (unauthenticated, for load balancers / orchestrators).
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Prometheus metrics (unauthenticated, for monitoring).
	mux.HandleFunc("GET /metrics", s.handleMetrics)

	// Web GUI (browser-based management).
	mux.HandleFunc("GET /setup", s.handleSetup)
	mux.HandleFunc("POST /setup", s.rateLimit(s.loginLimiter, s.handleSetup))
	mux.HandleFunc("GET /login", s.handleLogin)
	mux.HandleFunc("POST /login", s.rateLimit(s.loginLimiter, s.handleLogin))
	mux.HandleFunc("GET /oauth/{provider}/start", s.rateLimit(s.loginLimiter, s.handleOAuthStart))
	mux.HandleFunc("GET /oauth/{provider}/callback", s.rateLimit(s.loginLimiter, s.handleOAuthCallback))
	mux.HandleFunc("GET /invite/{token}", s.handleInviteLink)
	mux.HandleFunc("GET /cli-login/{code}", s.handleCLILoginPage)
	mux.HandleFunc("POST /cli-login/{code}", s.handleCLILoginApprove)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /dashboard", s.handleDashboard)
	mux.HandleFunc("POST /dashboard/upload", s.handleDashboardUpload)
	mux.HandleFunc("POST /dashboard/delete/{slug}", s.handleDashboardDelete)
	mux.HandleFunc("POST /dashboard/settings", s.handleDashboardSettings)
	mux.HandleFunc("POST /dashboard/invites", s.handleDashboardCreateInvite)
	mux.HandleFunc("POST /dashboard/invites/revoke/{id}", s.handleDashboardRevokeInvite)
	mux.HandleFunc("POST /dashboard/users/{id}/admin", s.handleDashboardUserAdmin)
	mux.HandleFunc("POST /dashboard/users/{id}/disabled", s.handleDashboardUserDisabled)
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
		// Global rate limit to protect against request floods.
		if !s.globalLimiter.allow(s.clientIP(r)) {
			http.Error(w, "Too many requests. Try again shortly.", http.StatusTooManyRequests)
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

// handleHealthz is a liveness probe — returns 200 if the process is running.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// handleReadyz is a readiness probe — returns 200 if the database is reachable.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(); err != nil {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready: " + err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// authToken gates an endpoint behind a bearer token (any valid user token).
func (s *Server) authToken(next http.HandlerFunc) http.HandlerFunc {
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
		if t.Disabled {
			jsonError(w, http.StatusForbidden, "account disabled")
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
		if t.Disabled {
			jsonError(w, http.StatusForbidden, "account disabled")
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
	defaults := map[string]string{
		"auth_token_login_enabled": "true",
		"max_upload":               strconv.FormatInt(maxUpload, 10),
		"max_total_size":           strconv.FormatInt(maxTotalSize, 10),
		"retention_days":           strconv.Itoa(retentionDays),
	}
	for k, v := range defaults {
		if err := upsert(k, v); err != nil {
			return err
		}
	}
	for k, v := range s3Defaults {
		if v != "" {
			if err := upsert(k, v); err != nil {
				return err
			}
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

// Close releases server resources (database connections, etc.).
func (s *Server) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

func (s *Server) audit(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Info("audit", "action", "system", "detail", msg)
	_ = s.store.AddAuditLog("", "system", msg, "")
}

// auditRequest logs an audit event with the actor's IP from the request.
func (s *Server) auditRequest(r *http.Request, actor, action, detail string) {
	ip := s.clientIP(r)
	slog.Info("audit", "actor", actor, "action", action, "detail", detail, "ip", ip)
	_ = s.store.AddAuditLog(actor, action, detail, ip)
}

func (s *Server) startRetentionCleanup(ctx context.Context) {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired(ctx)
		}
	}
}

func (s *Server) cleanupExpired(ctx context.Context) {
	retentionDays := s.settingInt("retention_days", 0)
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	uploads, err := s.store.ListUploadsOlderThan(cutoff)
	if err != nil {
		slog.Error("retention cleanup: list", "err", err)
		return
	}
	for _, u := range uploads {
		if err := s.storage.Delete(ctx, u.Slug); err != nil {
			slog.Error("retention cleanup: storage delete", "slug", u.Slug, "err", err)
		}
		if err := s.store.DeleteUpload(u.ID); err != nil {
			slog.Error("retention cleanup: db delete", "slug", u.Slug, "err", err)
		}
	}
	if len(uploads) > 0 {
		slog.Info("retention cleanup: removed expired uploads", "count", len(uploads), "cutoff", cutoff.Format(time.DateOnly))
	}
}
