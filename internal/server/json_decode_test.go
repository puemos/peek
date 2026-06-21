package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"token","extra":true}`))
	rec := httptest.NewRecorder()

	err := decodeJSON(rec, req, &body, smallJSONBodyLimit)

	if err == nil {
		t.Fatal("expected unknown field error")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field", err)
	}
}

func TestDecodeJSONRejectsTrailingValues(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"token"} {"name":"other"}`))
	rec := httptest.NewRecorder()

	err := decodeJSON(rec, req, &body, smallJSONBodyLimit)

	if err == nil || !strings.Contains(err.Error(), "single JSON value") {
		t.Fatalf("error = %v, want trailing value rejection", err)
	}
}

func TestDecodeJSONEnforcesBodyLimit(t *testing.T) {
	var body struct {
		Name string `json:"name"`
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"too-long"}`))
	rec := httptest.NewRecorder()

	err := decodeJSON(rec, req, &body, 4)

	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want body limit error", err)
	}
}
