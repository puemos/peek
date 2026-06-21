package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalBaseURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "localhost", url: "http://localhost:7700", want: true},
		{name: "localhost trailing dot", url: "http://localhost.:7700", want: true},
		{name: "ipv4 loopback", url: "http://127.0.0.1:7700", want: true},
		{name: "ipv4 loopback range", url: "http://127.12.34.56:7700", want: true},
		{name: "ipv6 loopback", url: "http://[::1]:7700", want: true},
		{name: "https public", url: "https://peek.example.com", want: false},
		{name: "localhost suffix", url: "http://localhost.evil.test", want: false},
		{name: "loopback suffix", url: "http://127.0.0.1.example.test", want: false},
		{name: "private lan", url: "http://10.0.0.5:7700", want: false},
		{name: "invalid", url: "://bad", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalBaseURL(tt.url); got != tt.want {
				t.Fatalf("isLocalBaseURL(%q) = %t, want %t", tt.url, got, tt.want)
			}
		})
	}
}

func TestLoadOrCreateSecretReadsExistingSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	want := "0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(path, []byte(want), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := loadOrCreateSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("secret = %q, want %q", got, want)
	}
}

func TestLoadOrCreateSecretPersistsGeneratedSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	got, err := loadOrCreateSecret(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64 {
		t.Fatalf("generated secret length = %d, want 64", len(got))
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != got {
		t.Fatalf("stored secret = %q, want %q", string(b), got)
	}
}

func TestLoadOrCreateSecretReportsInvalidPath(t *testing.T) {
	dir := t.TempDir()

	if _, err := loadOrCreateSecret(dir); err == nil {
		t.Fatal("expected error when secret path is a directory")
	}
}
