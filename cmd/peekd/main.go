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
	maxUpload := flag.Int64("max-upload", getenvInt("PEEK_MAX_UPLOAD", 2<<20), "max upload size in bytes")
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
