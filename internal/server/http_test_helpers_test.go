package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/server"
)

type testApp struct {
	URL        string
	AdminToken string
	DataDir    string

	server *server.Server
	client *http.Client
}

func newTestApp(t *testing.T) testApp {
	t.Helper()

	srv, adminToken, dir := newTestServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return testApp{
		URL:        ts.URL,
		AdminToken: adminToken,
		DataDir:    dir,
		server:     srv,
		client:     ts.Client(),
	}
}

type testResponse struct {
	StatusCode int
	Header     http.Header
	Cookies    []*http.Cookie
	Body       []byte
}

func (a testApp) request(t *testing.T, method, path string, body io.Reader, opts ...requestOption) testResponse {
	t.Helper()

	req, err := http.NewRequest(method, a.URL+path, body)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	for _, opt := range opts {
		opt(req)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s response: %v", method, path, err)
	}
	return testResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Cookies:    resp.Cookies(),
		Body:       data,
	}
}

func (a testApp) requestString(t *testing.T, method, path, body string, opts ...requestOption) testResponse {
	t.Helper()
	return a.request(t, method, path, strings.NewReader(body), opts...)
}

func (a testApp) requestJSON(t *testing.T, method, path string, in any, opts ...requestOption) testResponse {
	t.Helper()

	var body strings.Builder
	if err := json.NewEncoder(&body).Encode(in); err != nil {
		t.Fatalf("encode %s %s json: %v", method, path, err)
	}
	opts = append([]requestOption{withContentType("application/json")}, opts...)
	return a.request(t, method, path, strings.NewReader(body.String()), opts...)
}

func (a testApp) flushVisits(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.server.FlushVisits(ctx); err != nil {
		t.Fatalf("flush visits: %v", err)
	}
}

type requestOption func(*http.Request)

func withAuth(token string) requestOption {
	return func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func withContentType(contentType string) requestOption {
	return func(req *http.Request) {
		req.Header.Set("Content-Type", contentType)
	}
}

func withCookies(cookies ...*http.Cookie) requestOption {
	return func(req *http.Request) {
		for _, c := range cookies {
			req.AddCookie(c)
		}
	}
}

func assertStatus(t *testing.T, resp testResponse, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d, body = %s", resp.StatusCode, want, resp.Body)
	}
}

func decodeResponseJSON[T any](t *testing.T, resp testResponse) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		t.Fatalf("decode response json: %v; body = %s", err, resp.Body)
	}
	return out
}
