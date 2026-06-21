package server

import (
	"encoding/json"
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

func TestGetSettingsRowsAreSorted(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetSetting("s3_endpoint", "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("auth_token_login_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}
	rec := httptest.NewRecorder()

	s.handleGetSettings(rec, httptest.NewRequest(http.MethodGet, "/api/settings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var rows []settingsRow
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode rows: %v", err)
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, row.Key)
	}
	want := []string{"auth_token_login_enabled", "max_upload", "s3_endpoint"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("keys = %+v, want %+v", keys, want)
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
