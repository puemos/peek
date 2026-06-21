package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/uploadquota"
)

func TestAddCommentLogsVisitorUpsertFailure(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked(context.Background(), "page", account.ID, 0, "page.html", 42, "public", "", uploadquota.Limits{}); err != nil {
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
	if !strings.Contains(rec.Body.String(), `"anchor_kind":"page"`) {
		t.Fatalf("comment response did not infer page anchor kind: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "comment visitor upsert failed") {
		t.Fatalf("visitor upsert failure was not logged: %s", logs.String())
	}
}

func TestAddCommentPersistsExplicitAnchorKind(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked(context.Background(), "page", account.ID, 0, "page.html", 42, "public", "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/page/comments", strings.NewReader(`{"name":"Ada","body":"Looks good","selector":"#hero","element_text":"Hero copy","anchor_kind":"element"}`))
	req.SetPathValue("slug", "page")
	rec := httptest.NewRecorder()

	s.handleAddComment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"anchor_kind":"element"`) {
		t.Fatalf("comment response did not include explicit anchor kind: %s", rec.Body.String())
	}
}

func TestAddCommentRejectsInvalidAnchorKind(t *testing.T) {
	s := newTestServer(t)
	account, err := s.store.CreateAccount(context.Background(), "owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked(context.Background(), "page", account.ID, 0, "page.html", 42, "public", "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/page/comments", strings.NewReader(`{"body":"Looks good","selector":"#hero","element_text":"Hero copy","anchor_kind":"sideways"}`))
	req.SetPathValue("slug", "page")
	rec := httptest.NewRecorder()

	s.handleAddComment(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestPrivateUploadCommentsRequireActiveAccount(t *testing.T) {
	s := newTestServer(t)
	owner, err := s.store.CreateAccount(context.Background(), "owner@example.test", "Owner", false)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.store.CreateAccount(context.Background(), "viewer@example.test", "Viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateUploadChecked(context.Background(), "private", owner.ID, 0, "private.html", 42, "private", "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/private/comments", strings.NewReader(`{"name":"Ada","body":"Looks good"}`))
	req.SetPathValue("slug", "private")
	rec := httptest.NewRecorder()
	s.handleAddComment(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "login required") {
		t.Fatalf("anonymous private error = %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/uploads/private/comments", strings.NewReader(`{"name":"Ada","body":"Looks good"}`))
	req.SetPathValue("slug", "private")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(viewer.ID, 10), sessionTTL)})
	rec = httptest.NewRecorder()
	s.handleAddComment(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("active viewer status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
