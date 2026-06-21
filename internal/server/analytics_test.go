package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/puemos/peek/internal/db"
	"github.com/puemos/peek/internal/uploadquota"
)

func TestRecordVisitPersistsPrivacySafeAnalytics(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateUploadChecked(context.Background(), "page", account.ID, 0, "page.html", 42, "public", "", uploadquota.Limits{}); err != nil {
		t.Fatal(err)
	}
	upload, err := store.GetUpload(context.Background(), "page")
	if err != nil {
		t.Fatal(err)
	}

	secret := strings.Repeat("a", 64)
	s := &Server{
		store:        store,
		secret:       secret,
		trustedProxy: true,
		visitQueue:   make(chan visitEvent, 4),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.startVisitWorker(ctx)
	}()

	req := httptest.NewRequest(http.MethodGet, "/p/page", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.10, 10.0.0.1")
	req.Header.Set("User-Agent", strings.Repeat("u", 350))
	req.AddCookie(&http.Cookie{Name: nameCookie, Value: "Ada"})

	s.recordVisit(req, upload, "visitor-1")
	flushCtx, flushCancel := context.WithTimeout(context.Background(), time.Second)
	defer flushCancel()
	if err := s.FlushVisits(flushCtx); err != nil {
		t.Fatalf("flush visits: %v", err)
	}

	total, unique, err := store.CountVisits(context.Background(), upload.ID)
	if err != nil {
		t.Fatalf("count visits: %v", err)
	}
	if total != 1 || unique != 1 {
		t.Fatalf("visits total=%d unique=%d", total, unique)
	}
	recent, err := store.RecentVisits(context.Background(), upload.ID, 1)
	if err != nil {
		t.Fatalf("recent visits: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("recent visits = %+v", recent)
	}
	wantHash := sha256.Sum256([]byte(secret + "|203.0.113.10"))
	if recent[0].IP != hex.EncodeToString(wantHash[:])[:16] {
		t.Fatalf("ip hash = %q", recent[0].IP)
	}
	if strings.Contains(recent[0].IP, "203.0.113.10") {
		t.Fatalf("visit stored raw IP: %+v", recent[0])
	}
	if recent[0].UserAgent != strings.Repeat("u", 300) {
		t.Fatalf("user agent length = %d", len(recent[0].UserAgent))
	}
	visitor, err := store.GetVisitor(context.Background(), "visitor-1")
	if err != nil {
		t.Fatalf("get visitor: %v", err)
	}
	if visitor.Name != "Ada" {
		t.Fatalf("visitor name = %q", visitor.Name)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("visit worker did not stop")
	}
}
