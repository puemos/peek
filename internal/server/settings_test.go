package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/puemos/peek/internal/db"
)

func TestSettingKeysAreSorted(t *testing.T) {
	s := &Server{}
	got := s.settingKeys(map[string]string{
		"retention_days": "30",
		"max_upload":     "1024",
		"s3_endpoint":    "https://example.test",
	})
	want := []string{"max_upload", "retention_days", "s3_endpoint"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settingKeys() = %+v, want %+v", got, want)
	}
}

func TestDashboardSettingsInvalidS3EndpointRedirectEscapesQuery(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount("admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	form := url.Values{
		"csrf":        {"csrf-token"},
		"s3_endpoint": {"http://8.8.8.8"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardSettings(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect location %q: %v", loc, err)
	}
	got := u.Query().Get("err")
	if !strings.HasPrefix(got, "invalid s3 endpoint: ") {
		t.Fatalf("err query = %q", got)
	}
	if strings.Contains(loc, " ") {
		t.Fatalf("redirect location contains raw space: %q", loc)
	}
}

func TestDashboardSettingsRejectsMalformedForm(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount("admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", strings.NewReader("csrf=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardSettings(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse redirect location %q: %v", loc, err)
	}
	if got := u.Query().Get("err"); got != "invalid session" {
		t.Fatalf("err query = %q, location = %q", got, loc)
	}
}
