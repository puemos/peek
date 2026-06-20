package server

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func mustHexSecret() string {
	b := make([]byte, 32)
	return hex.EncodeToString(b)
}

func TestSignVerifyRoundtrip(t *testing.T) {
	secret := mustHexSecret()
	slug := "abc123"
	exp := time.Now().Add(time.Hour).Unix()
	tok := signToken(secret, slug, exp)
	if !verifyToken(secret, tok, slug) {
		t.Fatalf("expected token to verify")
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	secret := mustHexSecret()
	slug := "abc123"
	exp := time.Now().Add(-time.Hour).Unix()
	tok := signToken(secret, slug, exp)
	if verifyToken(secret, tok, slug) {
		t.Fatalf("expected expired token to be rejected")
	}
}

func TestVerifyTamperedSignature(t *testing.T) {
	secret := mustHexSecret()
	slug := "abc123"
	exp := time.Now().Add(time.Hour).Unix()
	tok := signToken(secret, slug, exp)
	tok += "x"
	if verifyToken(secret, tok, slug) {
		t.Fatalf("expected tampered token to be rejected")
	}
}

func TestVerifyWrongSlug(t *testing.T) {
	secret := mustHexSecret()
	exp := time.Now().Add(time.Hour).Unix()
	tok := signToken(secret, "abc123", exp)
	if verifyToken(secret, tok, "xyz789") {
		t.Fatalf("expected token for different slug to be rejected")
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	secret := mustHexSecret()
	plaintext := "my-super-secret-key"
	ciphertext, err := encryptSecret(secret, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := decryptSecret(secret, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if got != plaintext {
		t.Fatalf("decrypt mismatch: got %q want %q", got, plaintext)
	}
}

func TestDecryptSecretGCMFailureReturnsEmpty(t *testing.T) {
	secret1 := mustHexSecret()
	secret2 := strings.Repeat("f", 64)
	plaintext := "secret"
	ciphertext, err := encryptSecret(secret1, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := decryptSecret(secret2, ciphertext)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string on auth failure, got %q", got)
	}
}

func TestDecryptSecretEmpty(t *testing.T) {
	got, err := decryptSecret(mustHexSecret(), "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestDecryptSecretLegacyPlaintext(t *testing.T) {
	// Use a value that is not valid base64url so it falls through to the
	// legacy plaintext path.
	got, err := decryptSecret(mustHexSecret(), "plain text value")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "plain text value" {
		t.Fatalf("expected legacy plaintext to be returned as-is, got %q", got)
	}
}

func TestEncryptRequires32ByteKey(t *testing.T) {
	_, err := encryptSecret(strings.Repeat("a", 31), "x")
	if err == nil {
		t.Fatalf("expected error for non-hex key")
	}
	_, err = encryptSecret(hex.EncodeToString(make([]byte, 31)), "x")
	if err == nil {
		t.Fatalf("expected error for short key")
	}
}
