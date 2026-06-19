package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/puemos/peek/internal/server"
)

func main() {
	addr := flag.String("addr", getenv("PEEK_ADDR", ":7700"), "listen address")
	dataDir := flag.String("data", getenv("PEEK_DATA", "./data"), "data directory")
	baseURL := flag.String("base-url", getenv("PEEK_BASE_URL", "http://localhost:7700"), "public base URL")
	adminToken := flag.String("admin-token", getenv("PEEK_ADMIN_TOKEN", ""), "initial admin token (only used on first run)")
	maxUpload := flag.Int64("max-upload", getenvInt("PEEK_MAX_UPLOAD", 2<<20), "max upload size in bytes (per file)")

	storageFlag := flag.String("storage", getenv("PEEK_STORAGE", "file"), "storage backend: file or s3")
	s3Endpoint := flag.String("s3-endpoint", getenv("PEEK_S3_ENDPOINT", ""), "S3-compatible endpoint URL")
	s3Bucket := flag.String("s3-bucket", getenv("PEEK_S3_BUCKET", ""), "S3 bucket name")
	s3Region := flag.String("s3-region", getenv("PEEK_S3_REGION", "us-east-1"), "S3 region")
	s3AccessKey := flag.String("s3-access-key", getenv("PEEK_S3_ACCESS_KEY", ""), "S3 access key")
	s3SecretKey := flag.String("s3-secret-key", getenv("PEEK_S3_SECRET_KEY", ""), "S3 secret key")

	maxTotalSize := flag.Int64("max-total-size", getenvInt("PEEK_MAX_TOTAL_SIZE", 0), "max total storage bytes across all uploads (0 = unlimited)")
	retentionDays := flag.Int("retention-days", getenvIntAsInt("PEEK_RETENTION_DAYS", 0), "auto-delete uploads older than N days (0 = off)")
	flag.Parse()

	abs, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatalf("data dir: %v", err)
	}

	srv, err := server.New(server.Config{
		Addr:       *addr,
		DataDir:    abs,
		BaseURL:    *baseURL,
		AdminToken: *adminToken,
		MaxUpload:  *maxUpload,

		Storage:     *storageFlag,
		S3Endpoint:  *s3Endpoint,
		S3Bucket:    *s3Bucket,
		S3Region:    *s3Region,
		S3AccessKey: *s3AccessKey,
		S3SecretKey: *s3SecretKey,

		MaxTotalSize:  *maxTotalSize,
		RetentionDays: *retentionDays,
	})
	if err != nil {
		log.Fatalf("init: %v", err)
	}

	log.Printf("peek listening on %s (data: %s, base: %s)", *addr, abs, *baseURL)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatalf("server: %v", err)
	}
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
