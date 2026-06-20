package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOAuthAvailableDiscovery(t *testing.T) {
	withProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/providers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"providers":[{"key":"google","name":"Google"}]}`))
	}))
	defer withProvider.Close()
	ok, err := oauthAvailable(withProvider.URL)
	if err != nil || !ok {
		t.Fatalf("expected provider discovery, ok=%v err=%v", ok, err)
	}

	withoutProvider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"providers":[]}`))
	}))
	defer withoutProvider.Close()
	ok, err = oauthAvailable(withoutProvider.URL)
	if err != nil || ok {
		t.Fatalf("expected no providers, ok=%v err=%v", ok, err)
	}

	oldServer := httptest.NewServer(http.NotFoundHandler())
	defer oldServer.Close()
	ok, err = oauthAvailable(oldServer.URL)
	if err != nil || ok {
		t.Fatalf("expected old server fallback, ok=%v err=%v", ok, err)
	}
}
