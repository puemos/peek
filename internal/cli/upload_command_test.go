package cli

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamMultipartUploadEncodesFileAndPassword(t *testing.T) {
	body, contentType := streamMultipartUpload(strings.NewReader("<html></html>"), "page.html", "secret")
	defer body.Close()

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse content type: %v", err)
	}
	if mediaType != "multipart/form-data" {
		t.Fatalf("media type = %q", mediaType)
	}
	form, err := multipart.NewReader(body, params["boundary"]).ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("read multipart form: %v", err)
	}
	defer form.RemoveAll()
	if got := form.Value["password"]; len(got) != 1 || got[0] != "secret" {
		t.Fatalf("password field = %+v", got)
	}
	files := form.File["file"]
	if len(files) != 1 {
		t.Fatalf("file fields = %+v", files)
	}
	if files[0].Filename != "page.html" {
		t.Fatalf("filename = %q", files[0].Filename)
	}
	file, err := files[0].Open()
	if err != nil {
		t.Fatalf("open file part: %v", err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read file part: %v", err)
	}
	if string(data) != "<html></html>" {
		t.Fatalf("file body = %q", data)
	}
}

func TestCmdUploadStreamsPasswordMultipart(t *testing.T) {
	type observedRequest struct {
		Auth          string
		ContentLength int64
		Filename      string
		Password      string
		Body          string
	}
	observed := make(chan observedRequest, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := observedRequest{
			Auth:          r.Header.Get("Authorization"),
			ContentLength: r.ContentLength,
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			observed <- got
			return
		}
		got.Password = r.FormValue("password")
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			observed <- got
			return
		}
		defer file.Close()
		got.Filename = header.Filename
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			observed <- got
			return
		}
		got.Body = string(data)
		observed <- got
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"slug": "page",
			"url":  "http://example.test/p/page",
		})
	}))
	defer ts.Close()

	t.Setenv("HOME", t.TempDir())
	t.Setenv("PEEK_HOST", ts.URL)
	t.Setenv("PEEK_TOKEN", "token")
	file := filepath.Join(t.TempDir(), "page.html")
	if err := os.WriteFile(file, []byte("<!doctype html><html></html>"), 0o600); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	if err := cmdUpload([]string{file, "--password", "secret"}); err != nil {
		t.Fatalf("cmdUpload: %v", err)
	}
	got := <-observed
	if got.Auth != "Bearer token" {
		t.Fatalf("authorization = %q", got.Auth)
	}
	if got.ContentLength != -1 {
		t.Fatalf("content length = %d, want streamed request", got.ContentLength)
	}
	if got.Filename != "page.html" {
		t.Fatalf("filename = %q", got.Filename)
	}
	if got.Password != "secret" {
		t.Fatalf("password = %q", got.Password)
	}
	if got.Body != "<!doctype html><html></html>" {
		t.Fatalf("body = %q", got.Body)
	}
}
