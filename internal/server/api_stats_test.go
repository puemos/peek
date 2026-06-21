package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/uploadquota"
)

func TestExportUploadReportsVisitQueryFailure(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	token := "owner-token"
	if err := store.CreateToken(context.Background(), token, "owner", false, 0); err != nil {
		t.Fatal(err)
	}
	owner, err := store.GetToken(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUploadChecked(context.Background(), "page", owner.AccountID, owner.ID, "page.html", 42, "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Exec(`DROP TABLE visits`); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/uploads/page/export", nil)
	req.SetPathValue("slug", "page")
	req = withAPIToken(req, owner)
	rec := httptest.NewRecorder()

	s.handleExportUpload(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "db error" {
		t.Fatalf("body = %+v", body)
	}
}
