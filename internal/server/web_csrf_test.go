package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCSRFTokenSetsCookie(t *testing.T) {
	stubSecureRandomRead(t, func(b []byte) (int, error) {
		for i := range b {
			b[i] = byte(i)
		}
		return len(b), nil
	})
	s := &Server{}
	rec := httptest.NewRecorder()

	token, ok := s.csrfToken(rec)
	if !ok {
		t.Fatal("csrfToken failed")
	}
	if token != "000102030405060708090a0b0c0d0e0f" {
		t.Fatalf("token = %q", token)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != csrfCookie || cookies[0].Value != token {
		t.Fatalf("csrf cookie = %+v", cookies)
	}
}

func TestCSRFTokenReportsEntropyFailure(t *testing.T) {
	errEntropy := errors.New("entropy unavailable")
	stubSecureRandomRead(t, func([]byte) (int, error) {
		return 0, errEntropy
	})
	s := &Server{}
	rec := httptest.NewRecorder()

	token, ok := s.csrfToken(rec)
	if ok {
		t.Fatal("csrfToken succeeded")
	}
	if token != "" {
		t.Fatalf("token = %q", token)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("unexpected cookies: %+v", got)
	}
}

func TestValidateCSRFReportsRotationEntropyFailure(t *testing.T) {
	errEntropy := errors.New("entropy unavailable")
	stubSecureRandomRead(t, func([]byte) (int, error) {
		return 0, errEntropy
	})
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/upload", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "token"})

	valid, err := s.validateCSRF(req, httptest.NewRecorder(), "token")
	if valid {
		t.Fatal("csrf validated despite rotation failure")
	}
	if !errors.Is(err, errEntropy) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginReturns500WhenCSRFGenerationFails(t *testing.T) {
	errEntropy := errors.New("entropy unavailable")
	stubSecureRandomRead(t, func([]byte) (int, error) {
		return 0, errEntropy
	})
	s, _, _ := newWebLoginTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)

	s.handleLogin(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<form") {
		t.Fatalf("login form rendered after csrf failure: %s", rec.Body.String())
	}
}

func stubSecureRandomRead(t *testing.T, f func([]byte) (int, error)) {
	t.Helper()
	old := secureRandomRead
	secureRandomRead = f
	t.Cleanup(func() {
		secureRandomRead = old
	})
}
