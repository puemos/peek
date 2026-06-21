package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"

	"github.com/puemos/peek/internal/server"
	"github.com/puemos/peek/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(runHealthcheck(os.Args[2:]))
		case "backup":
			os.Exit(runBackup(os.Args[2:]))
		}
	}

	addr := flag.String("addr", getenv("PEEK_ADDR", ":7700"), "listen address")
	dataDir := flag.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	baseURL := flag.String("base-url", getenv("PEEK_BASE_URL", "http://localhost:7700"), "public base URL")
	maxUpload := flag.Int64("max-upload", getenvInt("PEEK_MAX_UPLOAD", 2<<20), "max upload size in bytes (per file)")
	secret := flag.String("secret", getenv("PEEK_SECRET", ""), "server secret for HMAC signing and encryption (auto-generated if empty)")

	storageFlag := flag.String("storage", getenv("PEEK_STORAGE", "file"), "storage backend: file or s3")
	s3Endpoint := flag.String("s3-endpoint", getenv("PEEK_S3_ENDPOINT", ""), "S3-compatible endpoint URL")
	s3Bucket := flag.String("s3-bucket", getenv("PEEK_S3_BUCKET", ""), "S3 bucket name")
	s3Region := flag.String("s3-region", getenv("PEEK_S3_REGION", "us-east-1"), "S3 region")
	s3AccessKey := flag.String("s3-access-key", getenv("PEEK_S3_ACCESS_KEY", ""), "S3 access key")
	s3SecretKey := flag.String("s3-secret-key", getenv("PEEK_S3_SECRET_KEY", ""), "S3 secret key")
	s3AllowPrivateEndpoint := flag.Bool("s3-allow-private-endpoint", getenvBool("PEEK_S3_ALLOW_PRIVATE_ENDPOINT"), "allow private/link-local S3 endpoint addresses for explicit dev deployments")

	maxTotalSize := flag.Int64("max-total-size", getenvInt("PEEK_MAX_TOTAL_SIZE", 0), "max total storage bytes across all uploads (0 = unlimited)")
	retentionDays := flag.Int("retention-days", getenvIntAsInt("PEEK_RETENTION_DAYS", 0), "auto-delete uploads older than N days (0 = off)")
	trustedProxy := flag.Bool("trusted-proxy", getenvBool("PEEK_TRUSTED_PROXY"), "trust X-Forwarded-For header (set when behind a reverse proxy)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("peekd " + version.String())
		return
	}

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "data dir: %v\n", err)
		os.Exit(1)
	}

	// Structured JSON logging (parseable by log aggregators).
	logLevel := slog.LevelInfo
	if v := os.Getenv("PEEK_LOG_LEVEL"); v == "debug" {
		logLevel = slog.LevelDebug
	} else if v == "warn" {
		logLevel = slog.LevelWarn
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel})))

	srv, err := server.New(server.Config{
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

func runHealthcheck(args []string) int {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	addr := fs.String("addr", getenv("PEEK_HEALTHCHECK_ADDR", getenv("PEEK_ADDR", ":7700")), "healthcheck address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(healthcheckURL(*addr))
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func healthcheckURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + "/healthz"
	}
	if strings.HasPrefix(addr, ":") {
		addr = "localhost" + addr
	}
	return "http://" + addr + "/healthz"
}

func runBackup(args []string) int {
	dataDir, backupPath, err := backupArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "backup: %v\n", err)
		return 2
	}
	if err := backupDatabase(dataDir, backupPath); err != nil {
		fmt.Fprintf(os.Stderr, "backup failed: %v\n", err)
		return 1
	}
	fmt.Printf("backup written to %s\n", backupPath)
	return 0
}

func backupArgs(args []string) (dataDir, backupPath string, err error) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	data := fs.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() > 1 {
		return "", "", fmt.Errorf("usage: peekd backup [--data <dir>] [path/to/backup.db]")
	}
	abs, err := filepath.Abs(*data)
	if err != nil {
		return "", "", fmt.Errorf("data dir: %w", err)
	}
	dest := filepath.Join(abs, "peek-backup.db")
	if fs.NArg() == 1 {
		dest = fs.Arg(0)
	}
	return abs, dest, nil
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

// backupDatabase creates a consistent snapshot of the SQLite database using
// VACUUM INTO, which works even while the server is running. The backup file
// is a standalone SQLite database that can be restored by simply replacing
// the original peek.db.
func backupDatabase(dataDir, destPath string) error {
	dbPath := filepath.Join(dataDir, "peek.db")
	dsn := "file:" + dbPath + "?_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open source db: %w", err)
	}
	defer db.Close()

	// VACUUM INTO creates a compact, consistent snapshot.
	destURI := "file:" + destPath
	_, err = db.Exec("VACUUM INTO ?", destURI)
	if err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	return nil
}
