package objectstore

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStorageSaveOpenDeleteValidSlug(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fs := &FileStorage{Dir: dir}
	data := []byte("<!doctype html><html></html>")

	if err := fs.Save(ctx, "abc-DEF_123", data); err != nil {
		t.Fatalf("save: %v", err)
	}
	rc, err := fs.Open(ctx, "abc-DEF_123")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("data = %q, want %q", got, data)
	}
	if err := fs.Delete(ctx, "abc-DEF_123"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "abc-DEF_123.html")); !os.IsNotExist(err) {
		t.Fatalf("expected object to be deleted, stat err=%v", err)
	}
}

func TestStorageRejectsUnsafeSlugs(t *testing.T) {
	ctx := context.Background()
	fs := &FileStorage{Dir: t.TempDir()}
	s3 := NewS3Storage(true, func(key string) string {
		switch key {
		case "s3_endpoint":
			return "http://127.0.0.1:9000"
		case "s3_bucket":
			return "bucket"
		default:
			return ""
		}
	})
	for _, slug := range []string{"", ".", "..", "../escape", "nested/path", `nested\path`, "name.html", "name space"} {
		t.Run(slug, func(t *testing.T) {
			if err := fs.Save(ctx, slug, []byte("x")); !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("file save error = %v, want ErrInvalidSlug", err)
			}
			if _, err := fs.Open(ctx, slug); !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("file open error = %v, want ErrInvalidSlug", err)
			}
			if err := fs.Delete(ctx, slug); !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("file delete error = %v, want ErrInvalidSlug", err)
			}
			if _, err := s3.objectKey(slug); !errors.Is(err, ErrInvalidSlug) {
				t.Fatalf("s3 key error = %v, want ErrInvalidSlug", err)
			}
		})
	}
}

func TestValidateS3EndpointRejectsUnsafeDefaults(t *testing.T) {
	tests := []string{
		"http://8.8.8.8",
		"https://127.0.0.1:9000",
		"https://10.0.0.1",
		"https://169.254.169.254",
		"ftp://example.com",
	}
	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			if err := ValidateS3Endpoint(endpoint, false); err == nil {
				t.Fatalf("expected %s to be rejected", endpoint)
			}
		})
	}
}

func TestValidateS3EndpointAllowsExplicitPrivateOptIn(t *testing.T) {
	if err := ValidateS3Endpoint("http://127.0.0.1:9000", true); err != nil {
		t.Fatalf("expected private dev endpoint with opt-in to pass: %v", err)
	}
}

func TestValidateS3EndpointAllowsPublicHTTPS(t *testing.T) {
	if err := ValidateS3Endpoint("https://8.8.8.8", false); err != nil {
		t.Fatalf("expected public https endpoint to pass: %v", err)
	}
}

func TestS3HTTPClientRejectsPrivateDialByDefault(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := newS3HTTPClient(false).Get(ts.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected private test server dial to be rejected")
	}
	if !strings.Contains(err.Error(), "private or link-local") {
		t.Fatalf("expected private endpoint error, got %v", err)
	}
}

func TestS3HTTPClientAllowsPrivateDialWithOptIn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := newS3HTTPClient(true).Get(ts.URL)
	if err != nil {
		t.Fatalf("expected private test server with opt-in to work: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
