package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/uploadquota"
)

func TestAddCommentLogsVisitorUpsertFailure(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount("owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked("page", account.ID, 0, "page.html", 42, "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.store.Exec(`DROP TABLE visitors`); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/page/comments", strings.NewReader(`{"name":"Ada","body":"Looks good"}`))
	req.SetPathValue("slug", "page")
	rec := httptest.NewRecorder()

	s.handleAddComment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"author":"Ada"`) {
		t.Fatalf("comment response did not include saved comment: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "comment visitor upsert failed") {
		t.Fatalf("visitor upsert failure was not logged: %s", logs.String())
	}
}
