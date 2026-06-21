package server

import (
	"context"
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
	"github.com/puemos/peek/internal/models"
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
	if err := store.SetSetting(context.Background(), "s3_endpoint", "https://example.test"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "auth_token_login_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
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

func TestUpdateSettingsRejectsUnknownKeysBeforeWriting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"max_upload":"2048","not_a_setting":"x"}`))
	req = withAPIToken(req, &models.Token{Name: "admin", IsAdmin: true})
	rec := httptest.NewRecorder()

	s.handleUpdateSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown setting: not_a_setting") {
		t.Fatalf("response did not identify unknown setting: %s", rec.Body.String())
	}
	got, err := store.GetSetting(context.Background(), "max_upload")
	if err != nil {
		t.Fatal(err)
	}
	if got != "1024" {
		t.Fatalf("max_upload was partially updated to %q", got)
	}
}

func TestUpdateSettingsRejectsInvalidValuesBeforeWriting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "retention_days", "7"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"max_upload":"0","retention_days":"30"}`))
	req = withAPIToken(req, &models.Token{Name: "admin", IsAdmin: true})
	rec := httptest.NewRecorder()

	s.handleUpdateSettings(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "max_upload must be a positive integer") {
		t.Fatalf("response did not identify invalid setting: %s", rec.Body.String())
	}
	if got := mustGetSetting(t, store, "max_upload"); got != "1024" {
		t.Fatalf("max_upload was partially updated to %q", got)
	}
	if got := mustGetSetting(t, store, "retention_days"); got != "7" {
		t.Fatalf("retention_days was partially updated to %q", got)
	}
}

func TestNormalizeSettingsValues(t *testing.T) {
	s := &Server{}
	tests := []struct {
		key   string
		value string
		want  string
	}{
		{key: "auth_token_login_enabled", value: "yes", want: "true"},
		{key: "oauth_google_enabled", value: "off", want: ""},
		{key: "max_upload", value: "2048", want: "2048"},
		{key: "max_total_size", value: "0", want: "0"},
		{key: "max_uploads_per_token", value: "12", want: "12"},
		{key: "retention_days", value: "30", want: "30"},
		{key: "storage", value: "S3", want: "s3"},
	}
	for _, tt := range tests {
		got, err := s.normalizeSettingValue(tt.key, tt.value)
		if err != nil {
			t.Fatalf("normalizeSettingValue(%q, %q): %v", tt.key, tt.value, err)
		}
		if got != tt.want {
			t.Fatalf("normalizeSettingValue(%q, %q) = %q, want %q", tt.key, tt.value, got, tt.want)
		}
	}
}

func TestDashboardSettingsInvalidS3EndpointRedirectEscapesQuery(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
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

func TestDashboardSettingsRejectsInvalidValuesBeforeWriting(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "auth_token_login_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "max_upload", "1024"); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	form := url.Values{
		"csrf":       {"csrf-token"},
		"max_upload": {"0"},
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
	if got := u.Query().Get("err"); got != "max_upload must be a positive integer" {
		t.Fatalf("err query = %q, location = %q", got, loc)
	}
	if got := mustGetSetting(t, store, "auth_token_login_enabled"); got != "true" {
		t.Fatalf("auth_token_login_enabled was partially updated to %q", got)
	}
	if got := mustGetSetting(t, store, "max_upload"); got != "1024" {
		t.Fatalf("max_upload was partially updated to %q", got)
	}
}

func TestDashboardSettingsPreservesHiddenOAuthAndS3Settings(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}
	if err := store.SetSetting(context.Background(), "oauth_google_enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "oauth_google_client_id", "google-client"); err != nil {
		t.Fatal(err)
	}
	if err := s.encryptedSetSetting(context.Background(), "oauth_google_client_secret", "google-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "storage", "s3"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(context.Background(), "s3_endpoint", "https://storage.example.test"); err != nil {
		t.Fatal(err)
	}
	if err := s.encryptedSetSetting(context.Background(), "s3_secret_key", "s3-secret"); err != nil {
		t.Fatal(err)
	}

	form := url.Values{
		"csrf":    {"csrf-token"},
		"storage": {"file"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardSettings(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := mustGetSetting(t, store, "oauth_google_enabled"); got != "" {
		t.Fatalf("oauth_google_enabled = %q, want disabled", got)
	}
	if got := mustGetSetting(t, store, "oauth_google_client_id"); got != "google-client" {
		t.Fatalf("oauth_google_client_id = %q", got)
	}
	if got, err := s.encryptedGetSetting(context.Background(), "oauth_google_client_secret"); err != nil || got != "google-secret" {
		t.Fatalf("oauth_google_client_secret = %q, err = %v", got, err)
	}
	if got := mustGetSetting(t, store, "storage"); got != "file" {
		t.Fatalf("storage = %q, want file", got)
	}
	if got := mustGetSetting(t, store, "s3_endpoint"); got != "https://storage.example.test" {
		t.Fatalf("s3_endpoint = %q", got)
	}
	if got, err := s.encryptedGetSetting(context.Background(), "s3_secret_key"); err != nil || got != "s3-secret" {
		t.Fatalf("s3_secret_key = %q, err = %v", got, err)
	}
}

func TestDashboardSettingsConvertsMegabyteLimits(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{store: store, secret: strings.Repeat("0", 64)}

	form := url.Values{
		"csrf":                     {"csrf-token"},
		"max_upload_mb":            {"3"},
		"max_total_size_mb":        {"512"},
		"max_storage_per_token_mb": {"128"},
		"max_uploads_per_token":    {"12"},
		"retention_days":           {"30"},
	}
	req := httptest.NewRequest(http.MethodPost, "/dashboard/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: makeWebSession(s.secret, strconv.FormatInt(account.ID, 10), sessionTTL)})
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "csrf-token"})
	rec := httptest.NewRecorder()

	s.handleDashboardSettings(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := mustGetSetting(t, store, "max_upload"); got != "3145728" {
		t.Fatalf("max_upload = %q", got)
	}
	if got := mustGetSetting(t, store, "max_total_size"); got != "536870912" {
		t.Fatalf("max_total_size = %q", got)
	}
	if got := mustGetSetting(t, store, "max_storage_per_token"); got != "134217728" {
		t.Fatalf("max_storage_per_token = %q", got)
	}
	if got := mustGetSetting(t, store, "max_uploads_per_token"); got != "12" {
		t.Fatalf("max_uploads_per_token = %q", got)
	}
	if got := mustGetSetting(t, store, "retention_days"); got != "30" {
		t.Fatalf("retention_days = %q", got)
	}
}

func TestDashboardSettingsRejectsMalformedForm(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "peek.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	account, err := store.CreateAccount(context.Background(), "admin@example.test", "Admin", true)
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

func mustGetSetting(t *testing.T, store *db.Store, key string) string {
	t.Helper()
	got, err := store.GetSetting(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
