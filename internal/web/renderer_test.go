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

func TestPageTemplateUsesDatasetForViewerConfig(t *testing.T) {
	renderer, err := newRenderer(func(name string) string {
		return "/" + name + "?v=test"
	})
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(TemplatePage, PageData{Name: "page.html", Slug: "abc123", RawURL: "/raw/abc123?t=t&v=v", Protected: true})
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
		`data-protected="true"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("page template missing %q: %s", want, html)
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
	body, err := renderer.Execute(TemplateDashboard, DashboardData{
		CSRF:    "csrf",
		User:    "Admin",
		IsAdmin: true,
		SettingsPanel: DashboardSettings{
			Auth: AuthSettings{
				Token: SettingRow{Key: "auth_token_login_enabled", Value: "true", Label: "Access token login", Description: "Allow token login"},
				Google: OAuthProviderSettings{
					Key:          "google",
					Name:         "Google",
					Enabled:      SettingRow{Key: "oauth_google_enabled", Value: "true", Label: "Google login", Description: "Enable Google OAuth login"},
					ClientID:     SettingRow{Key: "oauth_google_client_id", Label: "Google client ID", Description: "OAuth web client ID"},
					ClientSecret: SettingRow{Key: "oauth_google_client_secret", Label: "Google client secret", Description: "OAuth web client secret"},
					EnabledValue: true,
				},
				GitHub: OAuthProviderSettings{
					Key:          "github",
					Name:         "GitHub",
					Enabled:      SettingRow{Key: "oauth_github_enabled", Label: "GitHub login", Description: "Enable GitHub OAuth login"},
					ClientID:     SettingRow{Key: "oauth_github_client_id", Label: "GitHub client ID", Description: "OAuth app client ID"},
					ClientSecret: SettingRow{Key: "oauth_github_client_secret", Label: "GitHub client secret", Description: "OAuth app client secret"},
				},
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
		`name="oauth_google_enabled"`,
		`name="oauth_github_enabled"`,
		`x-show="oauth.google"`,
		`:disabled="!oauth.github"`,
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
