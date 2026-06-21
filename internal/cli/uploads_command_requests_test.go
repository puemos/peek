package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCmdDeleteSendsAuthenticatedDelete(t *testing.T) {
	seen := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		seen <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	if _, err := captureStdout(t, func() error {
		return cmdDelete([]string{"page"})
	}); err != nil {
		t.Fatalf("cmdDelete: %v", err)
	}
	if got := <-seen; got != "Bearer test-token" {
		t.Fatalf("authorization = %q", got)
	}
}

func TestCmdVisibilitySendsSetVisibilityRequest(t *testing.T) {
	type requestBody struct {
		Visibility string `json:"visibility"`
		Password   string `json:"password"`
	}
	seen := make(chan requestBody, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page/visibility" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
		var body requestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"visibility": "password"})
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	if _, err := captureStdout(t, func() error {
		return cmdVisibility([]string{"page", "password", "--password", "secret"})
	}); err != nil {
		t.Fatalf("cmdVisibility: %v", err)
	}
	if got := <-seen; got.Visibility != "password" || got.Password != "secret" {
		t.Fatalf("request body = %+v", got)
	}
}

func TestCmdVisibilityRejectsConflictingOptions(t *testing.T) {
	err := cmdVisibility([]string{"page", "password", "--password", "secret", "--password-stdin"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
}
