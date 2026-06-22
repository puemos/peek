package peekd

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/puemos/peek/internal/server"
	"github.com/puemos/peek/internal/version"
)

type serveConfig struct {
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
	OIDCAllowPrivateIssuer bool

	MaxTotalSize  int64
	RetentionDays int
	TrustedProxy  bool
}

func Run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "healthcheck":
			return runHealthcheck(args[1:])
		case "backup":
			return runBackup(args[1:])
		}
	}

	cfg, showVersion, err := parseServeConfig(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "peekd: %v\n", err)
		return 2
	}
	if showVersion {
		fmt.Println("peekd " + version.String())
		return 0
	}
	return runServe(cfg)
}

func parseServeConfig(args []string) (serveConfig, bool, error) {
	fs := flag.NewFlagSet("peekd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	maxUploadDefault, err := getenvInt64("PEEK_MAX_UPLOAD", 2<<20)
	if err != nil {
		return serveConfig{}, false, err
	}
	s3AllowPrivateEndpointDefault, err := getenvBool("PEEK_S3_ALLOW_PRIVATE_ENDPOINT")
	if err != nil {
		return serveConfig{}, false, err
	}
	oidcAllowPrivateIssuerDefault, err := getenvBool("PEEK_OIDC_ALLOW_PRIVATE_ISSUER")
	if err != nil {
		return serveConfig{}, false, err
	}
	maxTotalSizeDefault, err := getenvInt64("PEEK_MAX_TOTAL_SIZE", 0)
	if err != nil {
		return serveConfig{}, false, err
	}
	retentionDaysDefault, err := getenvInt("PEEK_RETENTION_DAYS", 0)
	if err != nil {
		return serveConfig{}, false, err
	}
	trustedProxyDefault, err := getenvBool("PEEK_TRUSTED_PROXY")
	if err != nil {
		return serveConfig{}, false, err
	}

	addr := fs.String("addr", getenv("PEEK_ADDR", ":7700"), "listen address")
	dataDir := fs.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	baseURL := fs.String("base-url", getenv("PEEK_BASE_URL", "http://localhost:7700"), "public base URL")
	maxUpload := fs.Int64("max-upload", maxUploadDefault, "max upload size in bytes (per file)")
	secret := fs.String("secret", getenv("PEEK_SECRET", ""), "server secret for HMAC signing and encryption (auto-generated if empty)")

	storageFlag := fs.String("storage", getenv("PEEK_STORAGE", "file"), "storage backend: file or s3")
	s3Endpoint := fs.String("s3-endpoint", getenv("PEEK_S3_ENDPOINT", ""), "S3-compatible endpoint URL")
	s3Bucket := fs.String("s3-bucket", getenv("PEEK_S3_BUCKET", ""), "S3 bucket name")
	s3Region := fs.String("s3-region", getenv("PEEK_S3_REGION", "us-east-1"), "S3 region")
	s3AccessKey := fs.String("s3-access-key", getenv("PEEK_S3_ACCESS_KEY", ""), "S3 access key")
	s3SecretKey := fs.String("s3-secret-key", getenv("PEEK_S3_SECRET_KEY", ""), "S3 secret key")
	s3AllowPrivateEndpoint := fs.Bool("s3-allow-private-endpoint", s3AllowPrivateEndpointDefault, "allow private/link-local S3 endpoint addresses for explicit dev deployments")
	oidcAllowPrivateIssuer := fs.Bool("oidc-allow-private-issuer", oidcAllowPrivateIssuerDefault, "allow http/private/link-local OIDC issuer URLs for explicit dev deployments")

	maxTotalSize := fs.Int64("max-total-size", maxTotalSizeDefault, "max total storage bytes across all uploads (0 = unlimited)")
	retentionDays := fs.Int("retention-days", retentionDaysDefault, "auto-delete uploads older than N days (0 = off)")
	trustedProxy := fs.Bool("trusted-proxy", trustedProxyDefault, "trust X-Forwarded-For header (set when behind a reverse proxy)")
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return serveConfig{}, false, err
	}
	if *showVersion {
		return serveConfig{}, true, nil
	}
	if err := validateServeConfig(*baseURL, *maxUpload, *storageFlag, *maxTotalSize, *retentionDays); err != nil {
		return serveConfig{}, false, err
	}

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		return serveConfig{}, false, fmt.Errorf("data dir: %w", err)
	}
	return serveConfig{
		Addr:      *addr,
		DataDir:   abs,
		BaseURL:   *baseURL,
		Secret:    *secret,
		MaxUpload: *maxUpload,

		Storage:                *storageFlag,
		S3Endpoint:             *s3Endpoint,
		S3Bucket:               *s3Bucket,
		S3Region:               *s3Region,
		S3AccessKey:            *s3AccessKey,
		S3SecretKey:            *s3SecretKey,
		S3AllowPrivateEndpoint: *s3AllowPrivateEndpoint,
		OIDCAllowPrivateIssuer: *oidcAllowPrivateIssuer,

		MaxTotalSize:  *maxTotalSize,
		RetentionDays: *retentionDays,
		TrustedProxy:  *trustedProxy,
	}, false, nil
}

func validateServeConfig(baseURL string, maxUpload int64, storage string, maxTotalSize int64, retentionDays int) error {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("base-url must be an absolute http or https URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base-url must use http or https")
	}
	if maxUpload <= 0 {
		return fmt.Errorf("max-upload must be greater than zero")
	}
	if maxTotalSize < 0 {
		return fmt.Errorf("max-total-size must be zero or greater")
	}
	if retentionDays < 0 {
		return fmt.Errorf("retention-days must be zero or greater")
	}
	if storage != "file" && storage != "s3" {
		return fmt.Errorf("storage must be file or s3")
	}
	return nil
}

func runServe(cfg serveConfig) int {
	configureLogging()

	srv, err := server.New(server.Config{
		Addr:      cfg.Addr,
		DataDir:   cfg.DataDir,
		BaseURL:   cfg.BaseURL,
		Secret:    cfg.Secret,
		MaxUpload: cfg.MaxUpload,

		Storage:                cfg.Storage,
		S3Endpoint:             cfg.S3Endpoint,
		S3Bucket:               cfg.S3Bucket,
		S3Region:               cfg.S3Region,
		S3AccessKey:            cfg.S3AccessKey,
		S3SecretKey:            cfg.S3SecretKey,
		S3AllowPrivateEndpoint: cfg.S3AllowPrivateEndpoint,
		OIDCAllowPrivateIssuer: cfg.OIDCAllowPrivateIssuer,

		MaxTotalSize:  cfg.MaxTotalSize,
		RetentionDays: cfg.RetentionDays,
		TrustedProxy:  cfg.TrustedProxy,
	})
	if err != nil {
		slog.Error("init", "err", err)
		return 1
	}

	slog.Info("peek starting", "addr", cfg.Addr, "data", cfg.DataDir, "base_url", cfg.BaseURL)
	hs := &http.Server{
		Addr:         cfg.Addr,
		Handler:      srv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	select {
	case err := <-serverErr:
		if err != nil {
			slog.Error("server", "err", err)
			closeServer(srv)
			return 1
		}
		closeServer(srv)
		return 0
	case sig := <-stop:
		slog.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	select {
	case err := <-serverErr:
		if err != nil {
			slog.Error("server", "err", err)
		}
	case <-ctx.Done():
	}
	closeServer(srv)
	slog.Info("bye")
	return 0
}

func closeServer(srv *server.Server) {
	if err := srv.Close(); err != nil {
		slog.Error("store close", "err", err)
	}
}

func configureLogging() {
	logLevel := slog.LevelInfo
	if v := os.Getenv("PEEK_LOG_LEVEL"); v == "debug" {
		logLevel = slog.LevelDebug
	} else if v == "warn" {
		logLevel = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))
}
