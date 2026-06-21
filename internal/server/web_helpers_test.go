package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDashboardFlashRedirectEscapesQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/dashboard/upload", nil)
	rec := httptest.NewRecorder()

	dashboardUploaded(rec, req, "http://example.test/p/page?x=a b&y=1")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse location %q: %v", loc, err)
	}
	if got := u.Query().Get("uploaded"); got != "http://example.test/p/page?x=a b&y=1" {
		t.Fatalf("uploaded query = %q", got)
	}
	if strings.Contains(loc, " ") {
		t.Fatalf("redirect location contains raw space: %q", loc)
	}
}
