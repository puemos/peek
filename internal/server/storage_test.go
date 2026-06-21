package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateS3EndpointRejectsUnsafeDefaults(t *testing.T) {
	tests := []string{
		"http://8.8.8.8",
		"https://127.0.0.1:9000",
		"https://10.0.0.1",
		"https://169.254.169.254",
		"ftp://example.com",
	}
	for _, endpoint := range tests {
		t.Run(endpoint, func(t *testing.T) {
			if err := validateS3Endpoint(endpoint, false); err == nil {
				t.Fatalf("expected %s to be rejected", endpoint)
			}
		})
	}
}

func TestValidateS3EndpointAllowsExplicitPrivateOptIn(t *testing.T) {
	if err := validateS3Endpoint("http://127.0.0.1:9000", true); err != nil {
		t.Fatalf("expected private dev endpoint with opt-in to pass: %v", err)
	}
}

func TestValidateS3EndpointAllowsPublicHTTPS(t *testing.T) {
	if err := validateS3Endpoint("https://8.8.8.8", false); err != nil {
		t.Fatalf("expected public https endpoint to pass: %v", err)
	}
}

func TestS3HTTPClientRejectsPrivateDialByDefault(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := newS3HTTPClient(false).Get(ts.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected private test server dial to be rejected")
	}
	if !strings.Contains(err.Error(), "private or link-local") {
		t.Fatalf("expected private endpoint error, got %v", err)
	}
}

func TestS3HTTPClientAllowsPrivateDialWithOptIn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	resp, err := newS3HTTPClient(true).Get(ts.URL)
	if err != nil {
		t.Fatalf("expected private test server with opt-in to work: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
