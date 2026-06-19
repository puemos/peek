package server

import (
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

// randID returns a URL-safe random id of nBytes entropy.
func randID(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
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
func makeViewToken(secret, slug string) string {
	exp := time.Now().Add(viewTokenTTL).Unix()
	return signToken(secret, slug, exp)
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

// makeWebSession signs subject.exp for an authenticated dashboard session. The
// subject is the token's row id, so the cookie carries a revocable reference
// rather than the bearer token itself.
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

var errInvalid = errors.New("invalid")
