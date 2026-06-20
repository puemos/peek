package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type settingsRow struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	IsSecret    bool   `json:"is_secret"`
	IsStartup   bool   `json:"is_startup"`
}

var settingsMeta = map[string]settingsRow{
	"max_upload":              {Label: "Max upload size (bytes)", Description: "Maximum size per individual HTML file upload"},
	"max_total_size":          {Label: "Max total storage (bytes)", Description: "Cumulative size limit across all uploads (0 = unlimited)"},
	"retention_days":          {Label: "Retention (days)", Description: "Auto-delete uploads older than this many days (0 = off)"},
	"max_uploads_per_token":   {Label: "Max uploads per token", Description: "Maximum number of uploads per token (0 = unlimited)"},
	"max_storage_per_token":   {Label: "Max storage per token (bytes)", Description: "Maximum total storage per token (0 = unlimited)"},
	"storage":        {Label: "Storage backend", Description: "file or s3 (requires restart to take effect)", IsStartup: true},
	"s3_endpoint":    {Label: "S3 endpoint URL", Description: "S3-compatible endpoint (e.g. https://<id>.r2.cloudflarestorage.com)"},
	"s3_bucket":      {Label: "S3 bucket", Description: "Bucket name for HTML file storage"},
	"s3_region":      {Label: "S3 region", Description: "AWS region (e.g. us-east-1, auto)"},
	"s3_access_key":  {Label: "S3 access key", Description: "Access key ID for S3-compatible storage"},
	"s3_secret_key":  {Label: "S3 secret key", Description: "Secret access key for S3-compatible storage", IsSecret: true},
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
			if err := validateS3Endpoint(v); err != nil {
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
	return keys
}

func (s *Server) handleDashboardSettings(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	owner, ok := s.webAuth(r)
	if !ok || !owner.IsAdmin {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	r.ParseForm()
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	for k, meta := range settingsMeta {
		v := r.FormValue(k)
		if v == "" {
			if meta.IsSecret {
				continue
			}
			_ = s.encryptedSetSetting(k, v)
		} else {
			if k == "s3_endpoint" {
				if err := validateS3Endpoint(v); err != nil {
					http.Redirect(w, r, "/dashboard?err=invalid+s3+endpoint:+ "+url.PathEscape(err.Error()), http.StatusSeeOther)
					return
				}
			}
			_ = s.encryptedSetSetting(k, v)
		}
	}
	s.auditRequest(r, owner.Name, "settings.update", "via dashboard")
	http.Redirect(w, r, "/dashboard?ok=settings+saved", http.StatusSeeOther)
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

func settingsToInt64(raw map[string]string, key string, def int64) int64 {
	v, ok := raw[key]
	if !ok || v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func settingsToInt(raw map[string]string, key string, def int) int {
	v, ok := raw[key]
	if !ok || v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) settingsSummary() map[string]any {
	raw := s.dashboardSettingsMap()
	return map[string]any{
		"MaxUpload":     humanSize(settingsToInt64(raw, "max_upload", 2<<20)),
		"MaxTotalSize":  humanSize(settingsToInt64(raw, "max_total_size", 0)),
		"RetentionDays": settingsToInt(raw, "retention_days", 0),
	}
}

func dashboardSettingsRows(raw map[string]string) []settingsRow {
	order := []string{
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
			meta.Value = "••••••••"
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
