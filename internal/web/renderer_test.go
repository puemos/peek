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
				Uploads:      []DashboardUpload{{Slug: "abc123", Filename: "page.html", SizeHuman: "1 KB", CreatedHuman: "2026-06-21 10:00"}},
				Invites:      []InviteDashboardRow{{ID: 1, Email: "person@example.com", Status: "pending", Expires: "2026-06-28 10:00", Link: "http://example.test/invite/token", CanRevoke: true}},
				Accounts:     []AccountDashboardRow{{ID: 1, Name: "Admin", Email: "admin@example.com", Admin: true, IsSelf: true}},
				Settings:     map[string]string{"max_upload": "2097152"},
				SettingsMeta: []SettingRow{{Key: "max_upload", Value: "2097152", Label: "Max upload size", Description: "bytes"}},
			},
			want: "Upload HTML",
		},
		{
			name: TemplateStats,
			data: StatsData{Slug: "abc123", Filename: "page.html", TotalVisits: 2, UniqueVisitors: 1, Recent: []StatsVisit{{Name: "Ada", IP: "hash", UA: "test", WhenHuman: "2026-06-21 10:00"}}},
			want: "Recent visits",
		},
		{
			name: TemplatePage,
			data: PageData{Filename: "page.html", Slug: "abc123", RawURL: "/raw/abc123?t=t&v=v"},
			want: "shared page",
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
	if strings.Contains(html, `data-url="settings saved"`) {
		t.Fatalf("generic success rendered as copyable upload URL: %s", html)
	}
}
