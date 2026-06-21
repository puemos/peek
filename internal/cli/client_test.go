package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReader) Close() error {
	return nil
}

func TestDecodeRespUsesStructuredAPIError(t *testing.T) {
	err := decodeResp(errorResponse(http.StatusForbidden, `{"error":"not owner"}`), nil)
	if err == nil || err.Error() != "not owner" {
		t.Fatalf("error = %v, want not owner", err)
	}
}

func TestDecodeRespFallsBackToPlainErrorBody(t *testing.T) {
	err := decodeResp(errorResponse(http.StatusBadGateway, "upstream unavailable"), nil)
	if err == nil || err.Error() != "502 Bad Gateway: upstream unavailable" {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeRespFallsBackToStatusWhenErrorBodyEmpty(t *testing.T) {
	err := decodeResp(errorResponse(http.StatusUnauthorized, ""), nil)
	if err == nil || err.Error() != "401 Unauthorized" {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeRespReportsErrorBodyReadFailure(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       errReader{},
	}
	err := decodeResp(resp, nil)
	if err == nil || !strings.Contains(err.Error(), "read error response") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadConfigReturnsEmptyWhenConfigIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "" || cfg.Token != "" {
		t.Fatalf("config = %+v, want empty", cfg)
	}
}

func TestLoadConfigReportsConfigReadFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err = LoadConfig()
	if err == nil {
		t.Fatal("expected config read failure")
	}
}

func errorResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
