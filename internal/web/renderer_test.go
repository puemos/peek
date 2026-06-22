package web

import (
	"strings"
	"testing"
)

func TestRendererExecutesAllTemplates(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	cases := []struct {
		name string
		data any
		want string
	}{
		{
			name: TemplateSetup,
			data: SetupData{CSRF: "csrf", Code: "code"},
			want: "Create admin",
		},
		{
			name: TemplateLogin,
			data: LoginData{CSRF: "csrf", Providers: []AuthProvider{{Key: "github", Name: "GitHub"}}, PasswordLogin: true, OAuthEnabled: true},
			want: "Continue with GitHub",
		},
		{
			name: TemplateDashboard,
			data: DashboardData{
				CSRF:         "csrf",
				User:         "Admin",
				IsAdmin:      true,
				Uploads:      []DashboardUpload{{Slug: "abc123", Name: "page.html", SizeHuman: "1 KB", CreatedHuman: "2026-06-21 10:00"}},
				Invites:      []InviteDashboardRow{{ID: 1, Email: "person@example.com", Status: "pending", Expires: "2026-06-28 10:00", Link: "http://example.test/invite/token", CanRevoke: true}},
				Accounts:     []AccountDashboardRow{{ID: 1, Name: "Admin", Email: "admin@example.com", Admin: true, IsSelf: true}},
				Settings:     map[string]string{"max_upload": "2097152"},
				SettingsMeta: []SettingRow{{Key: "max_upload", Value: "2097152", Label: "Max upload size", Description: "bytes"}},
			},
			want: "Upload HTML",
		},
		{
			name: TemplateStats,
			data: StatsData{Slug: "abc123", Name: "page.html", TotalVisits: 2, UniqueVisitors: 1, Recent: []StatsVisit{{Name: "Ada", IP: "hash", UA: "test", WhenHuman: "2026-06-21 10:00"}}},
			want: "Recent visits",
		},
		{
			name: TemplatePage,
			data: PageData{Name: "page.html", Slug: "abc123", RawURL: "/raw/abc123?t=t&v=v"},
			want: "sandbox=",
		},
		{
			name: TemplateGate,
			data: GateData{Slug: "abc123", Error: true},
			want: "Incorrect password",
		},
		{
			name: TemplateIndex,
			data: IndexData{BaseURL: "http://localhost:7700"},
			want: "CLI quick start",
		},
		{
			name: TemplateError,
			data: ErrorData{Title: "Page not found", Message: "This shared page does not exist."},
			want: "Page not found",
		},
		{
			name: TemplateCLILogin,
			data: CLILoginData{Code: "ABCDEFGH", CSRF: "csrf", User: "Admin"},
			want: "Approve CLI login",
		},
		{
			name: TemplateCLILoginDone,
			data: CLILoginDoneData{Title: "CLI login approved", Message: "Return to your terminal to continue."},
			want: "CLI login approved",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := renderer.Execute(tc.name, tc.data)
			if err != nil {
				t.Fatalf("execute %s: %v", tc.name, err)
			}
			if !strings.Contains(string(body), tc.want) {
				t.Fatalf("rendered %s without %q: %s", tc.name, tc.want, body)
			}
		})
	}
}

func TestStatsTemplateRendersSparkline(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplateStats, StatsData{
		Slug:           "abc123",
		Name:           "page.html",
		TotalVisits:    3,
		UniqueVisitors: 2,
		Sparkline: StatsSparkline{
			Summary:  "3 visits last 7 days",
			LinePath: "M 2.0 54.0 L 166.0 2.0",
			AreaPath: "M 2.0 54.0 L 166.0 2.0 L 166.0 54.0 L 2.0 54.0 Z",
			LastX:    "166.0",
			LastY:    "2.0",
		},
	})
	if err != nil {
		t.Fatalf("execute stats: %v", err)
	}
	html := string(body)
	for _, want := range []string{
		"3 visits last 7 days",
		`aria-label="Visit trend for the last 7 days"`,
		`d="M 2.0 54.0 L 166.0 2.0"`,
		`cx="166.0"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("stats template missing %q: %s", want, html)
		}
	}
}

func TestPageTemplateUsesDatasetForViewerConfig(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplatePage, PageData{Name: "page.html", Slug: "abc123", RawURL: "/raw/abc123?t=t&v=v", Visibility: "password"})
	if err != nil {
		t.Fatalf("execute page: %v", err)
	}
	html := string(body)
	if strings.Contains(html, `x-init="init(`) {
		t.Fatalf("page template should not call Alpine init with positional args: %s", html)
	}
	for _, want := range []string{
		`x-data="pageApp"`,
		`data-slug="abc123"`,
		`data-visibility="password"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("page template missing %q: %s", want, html)
		}
	}
}

func TestPageTemplateNameModalCanBeShownByAlpine(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplatePage, PageData{Name: "page.html", Slug: "abc123", RawURL: "/raw/abc123?t=t&v=v", Visibility: "public"})
	if err != nil {
		t.Fatalf("execute page: %v", err)
	}
	html := string(body)
	start := strings.Index(html, `<div id="hn-name-modal"`)
	if start < 0 {
		t.Fatalf("page template missing name modal: %s", html)
	}
	end := strings.Index(html[start:], ">")
	if end < 0 {
		t.Fatalf("name modal tag was not closed: %s", html[start:])
	}
	tag := html[start : start+end+1]
	if strings.Contains(tag, " hidden") {
		t.Fatalf("name modal must not use the hidden attribute because Alpine x-show cannot override it: %s", tag)
	}
	for _, want := range []string{`x-show="nameModalOpen"`, `x-cloak`} {
		if !strings.Contains(tag, want) {
			t.Fatalf("name modal tag missing %q: %s", want, tag)
		}
	}
}

func TestDashboardInviteLinkRendersAsCopyAction(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplateDashboard, DashboardData{
		CSRF:    "csrf",
		User:    "Admin",
		IsAdmin: true,
		Invites: []InviteDashboardRow{{
			ID:        1,
			Email:     "person@example.com",
			Status:    "pending",
			Expires:   "2026-06-28 10:00",
			Link:      "http://example.test/invite/token",
			CanRevoke: true,
		}},
	})
	if err != nil {
		t.Fatalf("execute dashboard: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, `title="Copy invite link"`) || !strings.Contains(html, `data-url="http://example.test/invite/token"`) {
		t.Fatalf("dashboard invite did not render a copy action: %s", html)
	}
	if !strings.Contains(html, `<span class="peek-link-copy-text">http://example.test/invite/token</span>`) {
		t.Fatalf("dashboard invite did not render the link value in the copy control: %s", html)
	}
	if strings.Contains(html, `>Copy link</span>`) {
		t.Fatalf("dashboard invite should show the link value, not generic copy text: %s", html)
	}
	if strings.Contains(html, `<code`) {
		t.Fatalf("dashboard invite should not render the link in an unconstrained code element: %s", html)
	}
	if strings.Contains(html, `window.location.origin + url`) {
		t.Fatalf("dashboard invite copy should not concatenate origin onto absolute URLs: %s", html)
	}
}

func TestDashboardUploadFormShowsSelectedFileAndDisablesEmptySubmit(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplateDashboard, DashboardData{
		CSRF: "csrf",
		User: "Admin",
	})
	if err != nil {
		t.Fatalf("execute dashboard: %v", err)
	}
	html := string(body)
	for _, want := range []string{
		`x-on:submit="guardUploadSubmit($event)"`,
		`x-on:change="setSelectedFile($event.target.files && $event.target.files[0])"`,
		`x-text="fileName || 'Click to choose an HTML file'"`,
		`x-text="fileSizeLabel ? fileSizeLabel + ' selected' : 'File selected'"`,
		`x-model="html"`,
		`:disabled="!canUpload()"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard upload form missing %q: %s", want, html)
		}
	}
}

func TestLoginRendersTabsWithMultipleMethods(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	body, err := renderer.Execute(TemplateLogin, LoginData{
		CSRF:          "csrf",
		PasswordLogin: true,
		TokenLogin:    true,
	})
	if err != nil {
		t.Fatalf("execute login: %v", err)
	}
	if got := strings.Count(string(body), `alt="Peek"`); got != 1 {
		t.Fatalf("login rendered %d logos, want 1: %s", got, body)
	}
	if !strings.Contains(string(body), `class="peek-login-tabs"`) {
		t.Fatalf("login did not render method tabs: %s", body)
	}
	if strings.Contains(string(body), `<span>token</span>`) {
		t.Fatalf("login rendered old stacked token separator: %s", body)
	}
}

func TestLoginRendersBrandedOAuthButtons(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}

	body, err := renderer.Execute(TemplateLogin, LoginData{
		CSRF: "csrf",
		Providers: []AuthProvider{
			{Key: "google", Name: "Google"},
			{Key: "github", Name: "GitHub"},
			{Key: "oidc", Name: "SSO"},
		},
		PasswordLogin: true,
		OAuthEnabled:  true,
	})
	if err != nil {
		t.Fatalf("execute login: %v", err)
	}

	html := string(body)
	for _, want := range []string{
		`href="/oauth/google/start"`,
		`class="peek-oauth-button peek-oauth-button-google"`,
		`Continue with Google`,
		`href="/oauth/github/start"`,
		`class="peek-oauth-button peek-oauth-button-github"`,
		`Continue with GitHub`,
		`href="/oauth/oidc/start"`,
		`class="peek-oauth-button peek-oauth-button-oidc"`,
		`Continue with SSO`,
		`class="peek-oauth-logo" viewBox="0 0 18 18" aria-hidden="true"`,
		`class="peek-oauth-logo" viewBox="0 0 98 96" aria-hidden="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("login oauth button missing %q: %s", want, html)
		}
	}
	if got := strings.Count(html, `alt="Peek"`); got != 1 {
		t.Fatalf("login rendered %d Peek logos, want 1: %s", got, html)
	}
	oidcStart := strings.Index(html, `href="/oauth/oidc/start"`)
	if oidcStart < 0 {
		t.Fatalf("OIDC button missing: %s", html)
	}
	oidcEnd := strings.Index(html[oidcStart:], `</a>`)
	if oidcEnd < 0 {
		t.Fatalf("OIDC button not closed: %s", html[oidcStart:])
	}
	oidcButton := html[oidcStart : oidcStart+oidcEnd]
	if strings.Contains(oidcButton, `peek-oauth-logo`) {
		t.Fatalf("OIDC button should not render a provider logo: %s", oidcButton)
	}
}

func TestLoginInviteCopyIsClear(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplateLogin, LoginData{
		CSRF:      "csrf",
		Invite:    true,
		Providers: []AuthProvider{{Key: "google", Name: "Google"}},
	})
	if err != nil {
		t.Fatalf("execute login: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, "Accept your invite by signing in.") {
		t.Fatalf("login invite copy missing: %s", html)
	}
	if strings.Contains(html, "Choose a provider to accept your invite.") {
		t.Fatalf("login rendered confusing invite copy: %s", html)
	}
}

func TestDashboardRendersSettingsTabsAndControls(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	google := OAuthProviderSettings{
		Key:          "google",
		Name:         "Google",
		Enabled:      SettingRow{Key: "oauth_google_enabled", Value: "true", Label: "Google login", Description: "Enable Google OAuth login"},
		ClientID:     SettingRow{Key: "oauth_google_client_id", Label: "Google client ID", Description: "OAuth web client ID"},
		ClientSecret: SettingRow{Key: "oauth_google_client_secret", Label: "Google client secret", Description: "OAuth web client secret", IsSecret: true},
		EnabledValue: true,
	}
	google.Fields = []SettingRow{google.ClientID, google.ClientSecret}
	github := OAuthProviderSettings{
		Key:          "github",
		Name:         "GitHub",
		Enabled:      SettingRow{Key: "oauth_github_enabled", Label: "GitHub login", Description: "Enable GitHub OAuth login"},
		ClientID:     SettingRow{Key: "oauth_github_client_id", Label: "GitHub client ID", Description: "OAuth app client ID"},
		ClientSecret: SettingRow{Key: "oauth_github_client_secret", Label: "GitHub client secret", Description: "OAuth app client secret", IsSecret: true},
	}
	github.Fields = []SettingRow{github.ClientID, github.ClientSecret}
	oidc := OAuthProviderSettings{
		Key:          "oidc",
		Name:         "SSO",
		Enabled:      SettingRow{Key: "oauth_oidc_enabled", Label: "SSO login", Description: "Enable generic OpenID Connect login"},
		ClientID:     SettingRow{Key: "oauth_oidc_client_id", Label: "SSO client ID", Description: "OpenID Connect client ID"},
		ClientSecret: SettingRow{Key: "oauth_oidc_client_secret", Label: "SSO client secret", Description: "OpenID Connect client secret", IsSecret: true},
	}
	oidc.Fields = []SettingRow{
		{Key: "oauth_oidc_issuer_url", Label: "SSO issuer URL", Description: "OpenID Connect issuer URL"},
		oidc.ClientID,
		oidc.ClientSecret,
	}
	body, err := renderer.Execute(TemplateDashboard, DashboardData{
		CSRF:    "csrf",
		User:    "Admin",
		IsAdmin: true,
		SettingsPanel: DashboardSettings{
			Auth: AuthSettings{
				Token:     SettingRow{Key: "auth_token_login_enabled", Value: "true", Label: "Access token login", Description: "Allow token login"},
				Domain:    SettingRow{Key: "auth_allowed_email_domain", Value: "example.com", Label: "Allowed email domain", Description: "Restrict login by domain"},
				Google:    google,
				GitHub:    github,
				OIDC:      oidc,
				Providers: []OAuthProviderSettings{google, github, oidc},
			},
			Storage: StorageSettings{
				Backend:    SettingRow{Key: "storage", Value: "s3", Label: "Storage backend", Description: "file or s3", IsStartup: true},
				Value:      "s3",
				S3Selected: true,
				S3Settings: []SettingRow{{Key: "s3_endpoint", Label: "S3 endpoint URL", Description: "S3-compatible endpoint"}},
			},
			Limits: []LimitSetting{{
				Key: "max_upload", FormKey: "max_upload_mb", JSKey: "maxUpload",
				Label: "Max upload size", Description: "Maximum size per HTML file upload",
				Unit: "MB", Value: 2, Min: 1, Max: 1024, Step: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("execute dashboard: %v", err)
	}
	html := string(body)
	for _, want := range []string{
		`settingsTab === 'auth'`,
		`settingsTab === 'storage'`,
		`settingsTab === 'limits'`,
		`name="auth_allowed_email_domain"`,
		`value="example.com"`,
		`name="oauth_google_enabled"`,
		`name="oauth_github_enabled"`,
		`name="oauth_oidc_enabled"`,
		`name="oauth_oidc_issuer_url"`,
		`x-show="oauth.google"`,
		`:disabled="!oauth.github"`,
		`:disabled="!oauth.oidc"`,
		`name="storage" value="file"`,
		`name="storage" value="s3"`,
		`type="number" name="max_upload_mb"`,
		`>MB</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard settings controls missing %q: %s", want, html)
		}
	}
	if strings.Contains(html, `type="range"`) || strings.Contains(html, `peek-range-input`) {
		t.Fatalf("dashboard limits should render compact number inputs, not sliders: %s", html)
	}
	if !strings.Contains(html, `name="storage" value="s3" x-model="storageBackend" checked`) {
		t.Fatalf("s3 storage radio was not checked: %s", html)
	}
}

func TestDashboardRendersGenericAndUploadFlashesSeparately(t *testing.T) {
	renderer, err := NewRenderer()
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplateDashboard, DashboardData{
		CSRF:             "csrf",
		User:             "Admin",
		FlashSuccess:     "settings saved",
		UploadSuccessURL: "http://example.test/p/page",
	})
	if err != nil {
		t.Fatalf("execute dashboard: %v", err)
	}
	html := string(body)
	if !strings.Contains(html, "settings saved") {
		t.Fatalf("dashboard did not render generic success: %s", html)
	}
	if !strings.Contains(html, "Uploaded! Share link:") || !strings.Contains(html, "http://example.test/p/page") {
		t.Fatalf("dashboard did not render upload success: %s", html)
	}
	for _, want := range []string{
		`data-peek-toast data-toast-type="success" data-toast-message="settings saved"`,
		`data-peek-toast data-toast-type="success" data-toast-message="Uploaded! Share link:" data-toast-description="http://example.test/p/page"`,
		`x-data="peekToasts"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard flash did not render toast marker %q: %s", want, html)
		}
	}
	if strings.Contains(html, `data-url="settings saved"`) {
		t.Fatalf("generic success rendered as copyable upload URL: %s", html)
	}
	if strings.Contains(html, `Uploaded! Share link:</span>`) {
		t.Fatalf("dashboard rendered old inline upload flash instead of toast marker: %s", html)
	}
}
