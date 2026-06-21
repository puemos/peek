package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCmdStatsPrintsSummaryAndRecentVisits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page/stats" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"slug":            "page",
			"filename":        "report.html",
			"total_visits":    3,
			"unique_visitors": 2,
			"recent": []map[string]any{
				{
					"name":       "Ada",
					"ip":         "abcdef1234567890abcdef",
					"user_agent": "Mozilla/5.0 internal browser",
					"visited_at": int64(1700000000),
				},
				{
					"ip":         "visitorhash",
					"user_agent": "curl/8.0",
					"visited_at": int64(1700000600),
				},
			},
		})
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdStats([]string{"page"})
	})
	if err != nil {
		t.Fatalf("cmdStats: %v", err)
	}
	for _, want := range []string{
		"slug:            page",
		"filename:        report.html",
		"total visits:    3",
		"unique visitors: 2",
		"recent visits:",
		"Ada",
		"(anonymous)",
		"Mozilla/5.0 internal browser",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCmdCommentsPrintsContextualComments(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/uploads/page/comments" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":           1,
				"selector":     "#hero",
				"element_text": "Important claim",
				"author":       "Grace",
				"body":         "Please attach benchmark evidence.",
				"created_at":   int64(1700000000),
			},
			{
				"id":           2,
				"selector":     "#footer",
				"element_text": "Footer copy",
				"anchor_kind":  "element",
				"body":         "Needs legal review.",
				"created_at":   int64(1700000600),
			},
		})
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdComments([]string{"page"})
	})
	if err != nil {
		t.Fatalf("cmdComments: %v", err)
	}
	for _, want := range []string{
		"Grace",
		"on: “Important claim”",
		"Please attach benchmark evidence.",
		"anonymous",
		"on: #footer",
		"Needs legal review.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCmdConfigShowPrintsSavedConfig(t *testing.T) {
	setTestConfigHome(t)
	t.Setenv("PEEK_HOST", "")
	t.Setenv("PEEK_TOKEN", "")
	if err := SaveConfig(&Config{Host: "https://peek.example.test", Token: "1234567890abcdef"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return cmdConfig([]string{"show"})
	})
	if err != nil {
		t.Fatalf("cmdConfig show: %v", err)
	}
	if !strings.Contains(out, "host:  https://peek.example.test") {
		t.Fatalf("stdout missing host:\n%s", out)
	}
	if !strings.Contains(out, "token: 1234…cdef") {
		t.Fatalf("stdout missing masked token:\n%s", out)
	}
}

func TestCmdTokenCreateSendsNameAndPrintsToken(t *testing.T) {
	type tokenRequest struct {
		Name string `json:"name"`
	}
	seen := make(chan tokenRequest, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tokens" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		var body tokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		seen <- body
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": body.Name, "token": "plain-token"})
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdToken([]string{"create", "--name", "deploy"})
	})
	if err != nil {
		t.Fatalf("cmdToken create: %v", err)
	}
	if got := <-seen; got.Name != "deploy" {
		t.Fatalf("request body = %+v", got)
	}
	if !strings.Contains(out, `created token for "deploy":`) || !strings.Contains(out, "plain-token") {
		t.Fatalf("stdout = %q", out)
	}
}

func TestCmdTokenListPrintsAdminColumn(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tokens" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": int64(1), "name": "admin", "admin": true},
			{"id": int64(2), "name": "automation", "admin": false},
		})
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdToken([]string{"list"})
	})
	if err != nil {
		t.Fatalf("cmdToken list: %v", err)
	}
	for _, want := range []string{"ID", "ADMIN", "admin", "yes", "automation", "no", "shown only once"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCmdTokenRevokeSendsDelete(t *testing.T) {
	seen := make(chan string, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tokens/42" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			t.Fatalf("method = %s", r.Method)
		}
		seen <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()
	configureTestClient(t, ts.URL)

	out, err := captureStdout(t, func() error {
		return cmdToken([]string{"revoke", "42"})
	})
	if err != nil {
		t.Fatalf("cmdToken revoke: %v", err)
	}
	if got := <-seen; got != "Bearer test-token" {
		t.Fatalf("authorization = %q", got)
	}
	if !strings.Contains(out, "revoked token 42") {
		t.Fatalf("stdout = %q", out)
	}
}
