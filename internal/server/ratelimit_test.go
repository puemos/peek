package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestLimiterAllowsUpToMax(t *testing.T) {
	l := newLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.allow("k") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.allow("k") {
		t.Fatalf("request beyond max should be rejected")
	}
}

func TestLimiterResetsAfterWindow(t *testing.T) {
	l := newLimiter(2, 50*time.Millisecond)
	if !l.allow("k") {
		t.Fatalf("first request should be allowed")
	}
	if !l.allow("k") {
		t.Fatalf("second request should be allowed")
	}
	if l.allow("k") {
		t.Fatalf("third request should be rejected before window reset")
	}
	time.Sleep(60 * time.Millisecond)
	if !l.allow("k") {
		t.Fatalf("request should be allowed after window reset")
	}
}

func TestLimiterPerKeyIsolation(t *testing.T) {
	l := newLimiter(1, time.Minute)
	if !l.allow("a") {
		t.Fatalf("key a first request should be allowed")
	}
	if !l.allow("b") {
		t.Fatalf("key b first request should be allowed")
	}
	if l.allow("a") {
		t.Fatalf("key a second request should be rejected")
	}
}

func TestLimiterCapacityCleanup(t *testing.T) {
	l := newLimiter(1, 50*time.Millisecond)
	for i := 0; i < 10001; i++ {
		l.allow(fmt.Sprintf("k%d", i))
	}
	// After exceeding capacity, expired windows should be cleaned.
	time.Sleep(60 * time.Millisecond)
	if !l.allow("cleanup-key") {
		t.Fatalf("request should be allowed after cleanup of expired windows")
	}
}

func TestRateLimitWrapperReturns429(t *testing.T) {
	s := &Server{globalLimiter: newLimiter(1, time.Minute)}
	called := 0
	handler := s.rateLimit(s.globalLimiter, func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusOK || called != 1 {
		t.Fatalf("first request should pass")
	}
	rec = httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusTooManyRequests || called != 1 {
		t.Fatalf("second request should be 429 and not call handler")
	}
}

func TestRateLimitWrapperReturnsJSONForAPI(t *testing.T) {
	s := &Server{globalLimiter: newLimiter(1, time.Minute)}
	handler := s.rateLimit(s.globalLimiter, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/upload", nil)

	handler(httptest.NewRecorder(), req)
	rec := httptest.NewRecorder()
	handler(rec, req)

	assertJSONRateLimit(t, rec)
}

func TestGlobalRateLimitReturnsJSONForAPI(t *testing.T) {
	s := &Server{globalLimiter: newLimiter(1, time.Minute)}
	handler := s.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/uploads", nil)

	handler.ServeHTTP(httptest.NewRecorder(), req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assertJSONRateLimit(t, rec)
}

func TestLimiterConcurrentAccess(t *testing.T) {
	l := newLimiter(10, time.Minute)
	const goroutines = 50
	const calls = 20
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < calls; j++ {
				_ = l.allow("concurrent-key")
			}
		}()
	}
	wg.Wait()
	w := l.hits["concurrent-key"]
	if w == nil || w.count > 10 {
		t.Fatalf("expected count <= max after concurrency, got %d", w.count)
	}
}

func assertJSONRateLimit(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q", got)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "too many requests, try again shortly" {
		t.Fatalf("error = %q", body.Error)
	}
}
