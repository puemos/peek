package server

import (
	"strings"
	"testing"

	webui "github.com/puemos/peek/internal/web"
)

func TestDashboardTemplateHasNoInlineScriptExecution(t *testing.T) {
	renderer, err := webui.NewRenderer(webui.AssetURL)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	body, err := renderer.Execute(webui.TemplateDashboard, dashData{
		CSRF:             "csrf",
		User:             "admin",
		UploadSuccess:    true,
		UploadSuccessURL: `");alert(1);//`,
		Uploads: []dashUpload{{
			Slug:         "abc123",
			Filename:     `x" onmouseover="alert(1)`,
			SizeHuman:    "1 KB",
			CreatedHuman: "2026-06-21 10:00",
		}},
	})
	if err != nil {
		t.Fatalf("execute dashboard template: %v", err)
	}
	html := string(body)
	if strings.Contains(html, "onclick=") || strings.Contains(html, "onchange=") || strings.Contains(html, "onsubmit=") {
		t.Fatalf("dashboard template contains inline event handler: %s", html)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("dashboard template contains inline script block: %s", html)
	}
	if strings.Contains(html, `onmouseover="alert(1)"`) {
		t.Fatalf("dashboard template rendered executable injected attribute: %s", html)
	}
	if !strings.Contains(html, `<script src="/dashboard.js?v=`) {
		t.Fatalf("dashboard template did not include dashboard.js: %s", html)
	}
}

func TestDashboardCSPRejectsInlineScript(t *testing.T) {
	if strings.Contains(webui.DashboardCSP, "'unsafe-inline'") {
		t.Fatalf("dashboard CSP allows inline script/style: %s", webui.DashboardCSP)
	}
	if !strings.Contains(webui.DashboardCSP, "script-src 'self'") {
		t.Fatalf("dashboard CSP does not pin scripts to self: %s", webui.DashboardCSP)
	}
}
