package server

import (
	"os"
	"path/filepath"
	"testing"
)

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
