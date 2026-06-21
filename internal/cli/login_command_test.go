package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdLoginTokenFileSavesConfig(t *testing.T) {
	setTestConfigHome(t)
	tokenFile := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(tokenFile, []byte("tok-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"providers":[]}`))
	}))
	defer authServer.Close()

	if err := cmdLogin([]string{"--host", authServer.URL, "--token-file", tokenFile}); err != nil {
		t.Fatalf("cmdLogin: %v", err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != authServer.URL || cfg.Token != "tok-file" {
		t.Fatalf("saved config = %+v", cfg)
	}
}

func TestCmdLoginRejectsTokenWhenOAuthRequired(t *testing.T) {
	setTestConfigHome(t)
	tokenFile := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(tokenFile, []byte("tok-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"providers":[{"key":"github","name":"GitHub"}]}`))
	}))
	defer authServer.Close()

	err := cmdLogin([]string{"--host", authServer.URL, "--token-file", tokenFile})
	if err == nil || !strings.Contains(err.Error(), "requires OAuth browser login") {
		t.Fatalf("expected OAuth-required error, got %v", err)
	}
}

func TestCmdLoginRejectsConflictingTokenInputs(t *testing.T) {
	setTestConfigHome(t)
	tokenFile := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(tokenFile, []byte("tok-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := [][]string{
		{"--host", "http://example.test", "--token", "tok", "--token-file", tokenFile},
		{"--host", "http://example.test", "--token", "tok", "--token-stdin"},
		{"--host", "http://example.test", "--token-file", tokenFile, "--token-stdin"},
	}
	for _, args := range tests {
		err := cmdLogin(args)
		if err == nil || err.Error() != "use only one of --token, --token-file, or --token-stdin" {
			t.Fatalf("cmdLogin(%v) error = %v", args, err)
		}
	}
}
