package server_test

import (
	"bytes"
	"html"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestUploadVisibilityE2E(t *testing.T) {
	app := newTestApp(t)
	app.client = noRedirectClient()

	t.Run("public anonymous access", func(t *testing.T) {
		up := uploadRawHTML(t, app, "public-page", "public")

		page := app.request(t, http.MethodGet, "/p/"+up.Slug, nil)
		assertStatus(t, page, http.StatusOK)
		if !strings.Contains(string(page.Body), "/raw/"+up.Slug) {
			t.Fatalf("public page did not include raw iframe: %s", page.Body)
		}

		addComment(t, app, up.Slug, nil, http.StatusOK)
		comments := app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/comments", nil)
		assertStatus(t, comments, http.StatusOK)
		if !strings.Contains(string(comments.Body), `"body":"Looks good"`) {
			t.Fatalf("public comments missing saved comment: %s", comments.Body)
		}

		views := app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/views", nil)
		assertStatus(t, views, http.StatusOK)
	})

	t.Run("password gate protects page and APIs", func(t *testing.T) {
		up := uploadMultipartHTML(t, app, "password-page", "password", "secret")

		page := app.request(t, http.MethodGet, "/p/"+up.Slug, nil)
		assertStatus(t, page, http.StatusOK)
		if !strings.Contains(string(page.Body), "Enter the password") {
			t.Fatalf("password upload did not render gate: %s", page.Body)
		}

		addComment(t, app, up.Slug, nil, http.StatusUnauthorized)
		views := app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/views", nil)
		assertStatus(t, views, http.StatusUnauthorized)

		wrong := postPagePassword(t, app, up.Slug, "wrong")
		assertStatus(t, wrong, http.StatusUnauthorized)
		if c := findCookie(wrong.Cookies, "hn_auth_"+up.Slug); c != nil {
			t.Fatalf("wrong password set auth cookie: %+v", c)
		}

		unlocked := postPagePassword(t, app, up.Slug, "secret")
		assertStatus(t, unlocked, http.StatusSeeOther)
		authCookie := findCookie(unlocked.Cookies, "hn_auth_"+up.Slug)
		if authCookie == nil {
			t.Fatalf("password response did not set page auth cookie: %+v", unlocked.Cookies)
		}

		page = app.request(t, http.MethodGet, "/p/"+up.Slug, nil, withCookies(authCookie))
		assertStatus(t, page, http.StatusOK)
		addComment(t, app, up.Slug, []*http.Cookie{authCookie}, http.StatusOK)
		views = app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/views", nil, withCookies(authCookie))
		assertStatus(t, views, http.StatusOK)
	})

	t.Run("private requires active non-owner account", func(t *testing.T) {
		up := uploadRawHTML(t, app, "private-page", "private")
		viewerToken := createAPIToken(t, app, "private-viewer")

		blocked := app.request(t, http.MethodGet, "/p/"+up.Slug, nil)
		assertStatus(t, blocked, http.StatusSeeOther)
		if got := blocked.Header.Get("Location"); got != "/login" {
			t.Fatalf("private page redirect = %q", got)
		}
		nextCookie := findCookie(blocked.Cookies, "hn_next")
		if nextCookie == nil || nextCookie.Value != "/p/"+up.Slug {
			t.Fatalf("private page did not set next cookie: %+v", blocked.Cookies)
		}

		addComment(t, app, up.Slug, nil, http.StatusUnauthorized)
		views := app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/views", nil)
		assertStatus(t, views, http.StatusUnauthorized)

		sessionCookie := loginWithToken(t, app, viewerToken, nextCookie)
		page := app.request(t, http.MethodGet, "/p/"+up.Slug, nil, withCookies(sessionCookie))
		assertStatus(t, page, http.StatusOK)
		addComment(t, app, up.Slug, []*http.Cookie{sessionCookie}, http.StatusOK)
		views = app.request(t, http.MethodGet, "/api/uploads/"+up.Slug+"/views", nil, withCookies(sessionCookie))
		assertStatus(t, views, http.StatusOK)
	})
}

func TestRawHTMLE2ERequiresIssuedViewToken(t *testing.T) {
	app := newTestApp(t)
	app.client = noRedirectClient()
	up := uploadRawHTML(t, app, "raw-token-page", "public")

	raw := app.request(t, http.MethodGet, "/raw/"+up.Slug, nil)
	assertStatus(t, raw, http.StatusForbidden)

	page := app.request(t, http.MethodGet, "/p/"+up.Slug, nil)
	assertStatus(t, page, http.StatusOK)
	rawPath := rawPathFromPage(t, string(page.Body), up.Slug)
	raw = app.request(t, http.MethodGet, rawPath, nil, withCookies(page.Cookies...))
	assertStatus(t, raw, http.StatusOK)
	if !strings.Contains(string(raw.Body), "e2e body") {
		t.Fatalf("raw response did not include uploaded HTML: %s", raw.Body)
	}
}

type uploadResult struct {
	Slug       string `json:"slug"`
	URL        string `json:"url"`
	Visibility string `json:"visibility"`
}

func uploadRawHTML(t *testing.T, app testApp, name, visibility string) uploadResult {
	t.Helper()
	path := "/api/upload?filename=" + url.QueryEscape(name+".html") + "&visibility=" + url.QueryEscape(visibility)
	resp := app.requestString(t, http.MethodPost, path, "<!doctype html><html><body>e2e body</body></html>", withAuth(app.AdminToken), withContentType("text/html; charset=utf-8"))
	assertStatus(t, resp, http.StatusOK)
	out := decodeResponseJSON[uploadResult](t, resp)
	if out.Slug == "" || out.Visibility != visibility {
		t.Fatalf("unexpected upload response: %+v", out)
	}
	return out
}

func uploadMultipartHTML(t *testing.T, app testApp, name, visibility, password string) uploadResult {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("visibility", visibility); err != nil {
		t.Fatal(err)
	}
	if password != "" {
		if err := mw.WriteField("password", password); err != nil {
			t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("file", name+".html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "<!doctype html><html><body>e2e body</body></html>"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	resp := app.request(t, http.MethodPost, "/api/upload", &body, withAuth(app.AdminToken), withContentType(mw.FormDataContentType()))
	assertStatus(t, resp, http.StatusOK)
	out := decodeResponseJSON[uploadResult](t, resp)
	if out.Slug == "" || out.Visibility != visibility {
		t.Fatalf("unexpected upload response: %+v", out)
	}
	return out
}

func postPagePassword(t *testing.T, app testApp, slug, password string) testResponse {
	t.Helper()
	form := url.Values{"password": {password}}
	return app.requestString(t, http.MethodPost, "/p/"+slug, form.Encode(), withContentType("application/x-www-form-urlencoded"))
}

func addComment(t *testing.T, app testApp, slug string, cookies []*http.Cookie, want int) {
	t.Helper()
	opts := []requestOption{withContentType("application/json")}
	if len(cookies) > 0 {
		opts = append(opts, withCookies(cookies...))
	}
	resp := app.requestString(t, http.MethodPost, "/api/uploads/"+slug+"/comments", `{"name":"Ada","body":"Looks good"}`, opts...)
	assertStatus(t, resp, want)
}

func loginWithToken(t *testing.T, app testApp, token string, nextCookie *http.Cookie) *http.Cookie {
	t.Helper()
	loginPage := app.request(t, http.MethodGet, "/login", nil, withCookies(nextCookie))
	assertStatus(t, loginPage, http.StatusOK)
	csrf := hiddenValue(t, string(loginPage.Body), "csrf")
	csrfCookie := findCookie(loginPage.Cookies, "hn_csrf")
	if csrfCookie == nil {
		t.Fatalf("login did not set csrf cookie: %+v", loginPage.Cookies)
	}
	form := url.Values{"method": {"token"}, "token": {token}, "csrf": {csrf}}
	resp := app.requestString(t, http.MethodPost, "/login", form.Encode(), withContentType("application/x-www-form-urlencoded"), withCookies(csrfCookie, nextCookie))
	assertStatus(t, resp, http.StatusSeeOther)
	if got := resp.Header.Get("Location"); got == "" || !strings.HasPrefix(got, "/p/") {
		t.Fatalf("login redirect = %q", got)
	}
	sessionCookie := findCookie(resp.Cookies, "hn_session")
	if sessionCookie == nil {
		t.Fatalf("login did not set session cookie: %+v", resp.Cookies)
	}
	return sessionCookie
}

func rawPathFromPage(t *testing.T, body, slug string) string {
	t.Helper()
	re := regexp.MustCompile(`src="([^"]*/raw/` + regexp.QuoteMeta(slug) + `[^"]*)"`)
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("raw iframe src not found in page: %s", body)
	}
	rawPath := html.UnescapeString(m[1])
	u, err := url.Parse(rawPath)
	if err != nil {
		t.Fatalf("parse raw path %q: %v", rawPath, err)
	}
	return u.RequestURI()
}
