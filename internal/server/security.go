package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

var secureRandomRead = rand.Read

// randID returns a URL-safe random id of nBytes entropy.
func randID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := secureRandomRead(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randHex(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := secureRandomRead(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// hmacSHA256 returns hex digest of msg keyed by secret.
func hmacSHA256(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// signToken produces slug.exp.sig using an HMAC over slug.exp.
func signToken(secret, slug string, exp int64) string {
	msg := slug + "." + strconv.FormatInt(exp, 10)
	sig := hmacSHA256(secret, msg)
	return msg + "." + sig
}

// verifyToken validates a slug.exp.sig token for the given slug.
func verifyToken(secret, tok, slug string) bool {
	parts := strings.SplitN(tok, ".", 3)
	if len(parts) != 3 {
		return false
	}
	tSlug, expStr, sig := parts[0], parts[1], parts[2]
	if tSlug != slug {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	want := hmacSHA256(secret, tSlug+"."+expStr)
	return hmac.Equal([]byte(sig), []byte(want))
}

// makeViewToken returns a short-lived token authorizing /raw access for slug.
// The vid (visitor id) is bound into the token so a raw URL shared with another
// viewer cannot be used — the visitor cookie must match.
func makeViewToken(secret, slug, vid string) string {
	exp := time.Now().Add(viewTokenTTL).Unix()
	msg := slug + "." + vid + "." + strconv.FormatInt(exp, 10)
	sig := hmacSHA256(secret, msg)
	return msg + "." + sig
}

// verifyViewToken validates a view token for the given slug and visitor id.
func verifyViewToken(secret, tok, slug, vid string) bool {
	parts := strings.SplitN(tok, ".", 4)
	if len(parts) != 4 {
		return false
	}
	tSlug, tVid, expStr, sig := parts[0], parts[1], parts[2], parts[3]
	if tSlug != slug || tVid != vid {
		return false
	}
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	want := hmacSHA256(secret, tSlug+"."+tVid+"."+expStr)
	return hmac.Equal([]byte(sig), []byte(want))
}

// makeSessionCookieValue signs slug.exp for password-gate sessions.
func makeSessionCookieValue(secret, slug string, ttl time.Duration) string {
	exp := time.Now().Add(ttl).Unix()
	return signToken(secret, slug, exp)
}

// verifySessionCookie validates a password-gate session value for slug.
func verifySessionCookie(secret, val, slug string) bool {
	return verifyToken(secret, val, slug)
}

// makeWebSession signs subject.exp for an authenticated dashboard session.
// Dashboard sessions use account ids, so disabling the account revokes web
// access without storing a bearer token in the cookie.
func makeWebSession(secret, subject string, ttl time.Duration) string {
	return signToken(secret, subject, time.Now().Add(ttl).Unix())
}

// parseSignedSubject verifies a subject.exp.sig value and returns the subject.
func parseSignedSubject(secret, val string) (string, bool) {
	parts := strings.SplitN(val, ".", 3)
	if len(parts) != 3 {
		return "", false
	}
	sub, expStr, sig := parts[0], parts[1], parts[2]
	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return "", false
	}
	want := hmacSHA256(secret, sub+"."+expStr)
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return "", false
	}
	return sub, true
}

// encryptSecret encrypts plaintext using AES-256-GCM with a key derived from
// the server secret. Returns base64-encoded nonce+ciphertext.
func encryptSecret(secretHex, plaintext string) (string, error) {
	key, err := hex.DecodeString(secretHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("invalid secret key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := secureRandomRead(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// decryptSecret decrypts ciphertext produced by encryptSecret. Returns the
// plaintext or an error. If the ciphertext is empty, returns empty. If the
// value is not valid base64 (legacy plaintext migration), it is returned
// as-is only if it does not look like encrypted data.
func decryptSecret(secretHex, ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	key, err := hex.DecodeString(secretHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("invalid secret key")
	}
	raw, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		// Not base64 — treat as legacy plaintext setting.
		return ciphertext, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		// Too short to be ciphertext — treat as legacy plaintext.
		return ciphertext, nil
	}
	nonce, ct := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// GCM authentication failed — do NOT return the ciphertext as
		// plaintext. Return empty so callers fall back to defaults rather
		// than leaking encrypted data.
		return "", nil
	}
	return string(plaintext), nil
}

// secretSettingKeys lists settings keys whose values are encrypted at rest.
var secretSettingKeys = map[string]bool{
	"s3_access_key":              true,
	"s3_secret_key":              true,
	"oauth_google_client_secret": true,
	"oauth_github_client_secret": true,
	"oauth_oidc_client_secret":   true,
}
