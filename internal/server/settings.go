package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/puemos/peek/internal/objectstore"
	webui "github.com/puemos/peek/internal/web"
)

type settingsRow = webui.SettingRow

var settingsMeta = map[string]settingsRow{
	"auth_token_login_enabled":   {Label: "Access token login", Description: "Allow signing in to the web dashboard with an access token", IsBool: true},
	"max_upload":                 {Label: "Max upload size (bytes)", Description: "Maximum size per individual HTML file upload"},
	"max_total_size":             {Label: "Max total storage (bytes)", Description: "Cumulative size limit across all uploads (0 = unlimited)"},
	"retention_days":             {Label: "Retention (days)", Description: "Auto-delete uploads older than this many days (0 = off)"},
	"max_uploads_per_token":      {Label: "Max uploads per owner", Description: "Maximum number of uploads per account/token owner (0 = unlimited)"},
	"max_storage_per_token":      {Label: "Max storage per owner (bytes)", Description: "Maximum total storage per account/token owner (0 = unlimited)"},
	"storage":                    {Label: "Storage backend", Description: "file or s3 (requires restart to take effect)", IsStartup: true},
	"s3_endpoint":                {Label: "S3 endpoint URL", Description: "S3-compatible endpoint (e.g. https://<id>.r2.cloudflarestorage.com)"},
	"s3_bucket":                  {Label: "S3 bucket", Description: "Bucket name for HTML file storage"},
	"s3_region":                  {Label: "S3 region", Description: "AWS region (e.g. us-east-1, auto)"},
	"s3_access_key":              {Label: "S3 access key", Description: "Access key ID for S3-compatible storage"},
	"s3_secret_key":              {Label: "S3 secret key", Description: "Secret access key for S3-compatible storage", IsSecret: true},
	"oauth_google_enabled":       {Label: "Google login", Description: "Enable Google OAuth login", IsBool: true},
	"oauth_google_client_id":     {Label: "Google client ID", Description: "OAuth web client ID"},
	"oauth_google_client_secret": {Label: "Google client secret", Description: "OAuth web client secret", IsSecret: true},
	"oauth_github_enabled":       {Label: "GitHub login", Description: "Enable GitHub OAuth login", IsBool: true},
	"oauth_github_client_id":     {Label: "GitHub client ID", Description: "OAuth app client ID"},
	"oauth_github_client_secret": {Label: "GitHub client secret", Description: "OAuth app client secret", IsSecret: true},
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	raw, err := s.encryptedGetAllSettings()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	out := make([]settingsRow, 0, len(raw))
	for k, v := range raw {
		meta, ok := settingsMeta[k]
		if !ok {
			meta.Label = k
		}
		meta.Key = k
		if meta.IsSecret && v != "" {
			meta.Value = "••••"
		} else {
			meta.Value = v
		}
		out = append(out, meta)
	}
	jsonOK(w, out)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	for k, v := range body {
		if _, ok := settingsMeta[k]; !ok {
			continue
		}
		if k == "s3_endpoint" && v != "" {
			if err := objectstore.ValidateS3Endpoint(v, s.s3AllowPrivateEndpoint); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid S3 endpoint: "+err.Error())
				return
			}
		}
		if err := s.encryptedSetSetting(k, v); err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	actor, _ := s.store.GetToken(bearerToken(r))
	s.auditRequest(r, actorName(actor), "settings.update", strings.Join(s.settingKeys(body), ","))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) settingKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *Server) handleDashboardSettings(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.parseDashboardForm(w, r) {
		return
	}
	for k, meta := range settingsMeta {
		v := r.FormValue(k)
		if meta.IsBool {
			if v != "" {
				if err := s.encryptedSetSetting(k, "true"); err != nil {
					dashboardError(w, r, "settings update failed")
					return
				}
			} else {
				if err := s.encryptedSetSetting(k, ""); err != nil {
					dashboardError(w, r, "settings update failed")
					return
				}
			}
			continue
		}
		if v == "" {
			if meta.IsSecret {
				continue
			}
			if err := s.encryptedSetSetting(k, v); err != nil {
				dashboardError(w, r, "settings update failed")
				return
			}
		} else {
			if k == "s3_endpoint" {
				if err := objectstore.ValidateS3Endpoint(v, s.s3AllowPrivateEndpoint); err != nil {
					dashboardError(w, r, "invalid s3 endpoint: "+err.Error())
					return
				}
			}
			if err := s.encryptedSetSetting(k, v); err != nil {
				dashboardError(w, r, "settings update failed")
				return
			}
		}
	}
	s.auditRequest(r, owner.Name, "settings.update", "via dashboard")
	dashboardOK(w, r, "settings saved")
}

func (s *Server) dashboardSettingsMap() map[string]string {
	raw, _ := s.encryptedGetAllSettings()
	if raw == nil {
		raw = map[string]string{}
	}
	for k := range settingsMeta {
		if _, ok := raw[k]; !ok {
			raw[k] = ""
		}
	}
	return raw
}

func dashboardSettingsRows(raw map[string]string) []settingsRow {
	order := []string{
		"auth_token_login_enabled",
		"oauth_google_enabled", "oauth_google_client_id", "oauth_google_client_secret",
		"oauth_github_enabled", "oauth_github_client_id", "oauth_github_client_secret",
		"storage", "s3_endpoint", "s3_bucket", "s3_region", "s3_access_key", "s3_secret_key",
		"max_upload", "max_total_size", "max_uploads_per_token", "max_storage_per_token", "retention_days",
	}
	seen := map[string]bool{}
	out := make([]settingsRow, 0, len(order))
	for _, k := range order {
		meta := settingsMeta[k]
		meta.Key = k
		meta.Value = raw[k]
		if meta.IsSecret && meta.Value != "" {
			meta.Value = ""
			meta.Description = meta.Description + " (leave blank to keep current value)"
		}
		out = append(out, meta)
		seen[k] = true
	}
	for k, v := range raw {
		if seen[k] {
			continue
		}
		out = append(out, settingsRow{Key: k, Value: v, Label: k})
	}
	return out
}

func (s *Server) settingString(key string) string {
	v, err := s.encryptedGetSetting(key)
	if err != nil {
		return ""
	}
	return v
}

func (s *Server) settingBool(key string) bool {
	switch s.settingString(key) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
