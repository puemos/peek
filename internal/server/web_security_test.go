package server

import (
	"bytes"
	"strings"
	"testing"
)

func TestDashboardTemplateHasNoInlineScriptExecution(t *testing.T) {
	var body bytes.Buffer
	err := dashboardTmpl.Execute(&body, dashData{
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
	html := body.String()
	if strings.Contains(html, "onclick=") || strings.Contains(html, "onchange=") || strings.Contains(html, "onsubmit=") {
		t.Fatalf("dashboard template contains inline event handler: %s", html)
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("dashboard template contains inline script block: %s", html)
	}
	if strings.Contains(html, `onmouseover="alert(1)"`) {
		t.Fatalf("dashboard template rendered executable injected attribute: %s", html)
	}
	if !strings.Contains(html, `<script src="/dashboard.js"></script>`) {
		t.Fatalf("dashboard template did not include dashboard.js: %s", html)
	}
}

func TestDashboardCSPRejectsInlineScript(t *testing.T) {
	if strings.Contains(dashboardCSP, "'unsafe-inline'") {
		t.Fatalf("dashboard CSP allows inline script/style: %s", dashboardCSP)
	}
	if !strings.Contains(dashboardCSP, "script-src 'self'") {
		t.Fatalf("dashboard CSP does not pin scripts to self: %s", dashboardCSP)
	}
}
