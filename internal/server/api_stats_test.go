package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

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

func TestHandleViewsReturnsAlignedBuckets(t *testing.T) {
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
	upload, err := store.GetUpload(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}
	today := bucketStart(time.Now(), 86400).Unix()
	for i, ts := range []int64{today - 86400, today} {
		cookie := "visitor-" + string(rune('a'+i))
		if _, err := store.Exec(`INSERT INTO visits(upload_id,visitor_cookie,visitor_name,ip,user_agent,visited_at) VALUES(?,?,?,?,?,?)`,
			upload.ID, cookie, "", "", "", ts+3600); err != nil {
			t.Fatal(err)
		}
	}

	s := &Server{store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/uploads/page/views", nil)
	req.SetPathValue("slug", "page")
	rec := httptest.NewRecorder()

	s.handleViews(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Total   int `json:"total"`
		Unique  int `json:"unique"`
		Buckets []struct {
			T int64 `json:"t"`
			N int   `json:"n"`
		} `json:"buckets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Total != 2 || body.Unique != 2 {
		t.Fatalf("counts = total %d unique %d", body.Total, body.Unique)
	}
	if len(body.Buckets) != 7 {
		t.Fatalf("bucket count = %d", len(body.Buckets))
	}
	for _, bucket := range body.Buckets {
		if bucket.N > 0 {
			return
		}
	}
	t.Fatalf("expected at least one non-zero bucket: %+v", body.Buckets)
}
