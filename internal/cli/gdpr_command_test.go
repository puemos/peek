package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCmdExportPrintsSuccessfulExport(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page/export" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"slug":"page"}`))
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdExport([]string{"page"})
	})
	if err != nil {
		t.Fatalf("cmdExport: %v", err)
	}
	if out != "{\"slug\":\"page\"}\n" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestCmdExportReturnsAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page/export" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"export failed"}`))
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdExport([]string{"page"})
	})
	if err == nil || !strings.Contains(err.Error(), "export failed") {
		t.Fatalf("error = %v", err)
	}
	if out != "" {
		t.Fatalf("stdout = %q", out)
	}
}
