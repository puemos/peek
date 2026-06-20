package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/puemos/peek/internal/server"
	"github.com/puemos/peek/internal/version"
)

func main() {
	addr := flag.String("addr", getenv("PEEK_ADDR", ":7700"), "listen address")
	dataDir := flag.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	baseURL := flag.String("base-url", getenv("PEEK_BASE_URL", "http://localhost:7700"), "public base URL")
	adminToken := flag.String("admin-token", getenv("PEEK_ADMIN_TOKEN", ""), "initial admin token (only used on first run)")
	maxUpload := flag.Int64("max-upload", getenvInt("PEEK_MAX_UPLOAD", 2<<20), "max upload size in bytes (per file)")
	secret := flag.String("secret", getenv("PEEK_SECRET", ""), "server secret for HMAC signing and encryption (auto-generated if empty)")

	storageFlag := flag.String("storage", getenv("PEEK_STORAGE", "file"), "storage backend: file or s3")
	s3Endpoint := flag.String("s3-endpoint", getenv("PEEK_S3_ENDPOINT", ""), "S3-compatible endpoint URL")
	s3Bucket := flag.String("s3-bucket", getenv("PEEK_S3_BUCKET", ""), "S3 bucket name")
	s3Region := flag.String("s3-region", getenv("PEEK_S3_REGION", "us-east-1"), "S3 region")
	s3AccessKey := flag.String("s3-access-key", getenv("PEEK_S3_ACCESS_KEY", ""), "S3 access key")
	s3SecretKey := flag.String("s3-secret-key", getenv("PEEK_S3_SECRET_KEY", ""), "S3 secret key")

	maxTotalSize := flag.Int64("max-total-size", getenvInt("PEEK_MAX_TOTAL_SIZE", 0), "max total storage bytes across all uploads (0 = unlimited)")
	retentionDays := flag.Int("retention-days", getenvIntAsInt("PEEK_RETENTION_DAYS", 0), "auto-delete uploads older than N days (0 = off)")
	trustedProxy := flag.Bool("trusted-proxy", getenvBool("PEEK_TRUSTED_PROXY"), "trust X-Forwarded-For header (set when behind a reverse proxy)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("peekd " + version.String())
		return
	}

	// Structured JSON logging (parseable by log aggregators).
	logLevel := slog.LevelInfo
	if v := os.Getenv("PEEK_LOG_LEVEL"); v == "debug" {
		logLevel = slog.LevelDebug
	} else if v == "warn" {
		logLevel = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		slog.Error("data dir", "err", err)
		os.Exit(1)
	}

	srv, err := server.New(server.Config{
		Addr:       *addr,
		DataDir:    abs,
		BaseURL:    *baseURL,
		AdminToken: *adminToken,
		Secret:     *secret,
		MaxUpload:  *maxUpload,

		Storage:     *storageFlag,
		S3Endpoint:  *s3Endpoint,
		S3Bucket:    *s3Bucket,
		S3Region:    *s3Region,
		S3AccessKey: *s3AccessKey,
		S3SecretKey: *s3SecretKey,

		MaxTotalSize:  *maxTotalSize,
		RetentionDays: *retentionDays,
		TrustedProxy:  *trustedProxy,
	})
	if err != nil {
		slog.Error("init", "err", err)
		os.Exit(1)
	}

	slog.Info("peek starting", "addr", *addr, "data", abs, "base_url", *baseURL)
	hs := &http.Server{
		Addr:         *addr,
		Handler:      srv.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	go func() {
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	sig := <-stop
	slog.Info("shutting down", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := hs.Shutdown(ctx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
	if err := srv.Close(); err != nil {
		slog.Error("store close", "err", err)
	}
	slog.Info("bye")
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getenvInt(k string, d int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return d
}

func getenvIntAsInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return d
}

func getenvBool(k string) bool {
	v := os.Getenv(k)
	return v == "1" || v == "true" || v == "yes"
}
