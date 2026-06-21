package server

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/puemos/peek/internal/db"
)

func TestDecryptedStoreSettingDecryptsSecretValues(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	secret := "0000000000000000000000000000000000000000000000000000000000000000"
	encrypted, err := encryptSecret(secret, "access-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "s3_secret_key", encrypted); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "s3_endpoint", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	if got := decryptedStoreSetting(context.Background(), store, secret, "s3_secret_key"); got != "access-secret" {
		t.Fatalf("secret setting = %q", got)
	}
	if got := decryptedStoreSetting(context.Background(), store, secret, "s3_endpoint"); got != "https://example.com" {
		t.Fatalf("plain setting = %q", got)
	}
}
