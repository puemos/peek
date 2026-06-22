package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/puemos/peek/internal/objectstore"
	webui "github.com/puemos/peek/internal/web"
)

type settingsRow = webui.SettingRow

const dashboardMegabyte int64 = 1024 * 1024
const maxDashboardMegabytes = int64(1<<63-1) / dashboardMegabyte

var settingsMeta = map[string]settingsRow{
	"auth_token_login_enabled":    {Label: "Access token login", Description: "Allow signing in to the web dashboard with an access token", IsBool: true},
	authAllowedEmailDomainSetting: {Label: "Allowed email domain", Description: "Restrict human sign-in, CLI approval, and invites to this email domain (blank = any domain)"},
	"max_upload":                  {Label: "Max upload size (bytes)", Description: "Maximum size per individual HTML file upload"},
	"max_total_size":              {Label: "Max total storage (bytes)", Description: "Cumulative size limit across all uploads (0 = unlimited)"},
	"retention_days":              {Label: "Retention (days)", Description: "Auto-delete uploads older than this many days (0 = off)"},
	"max_uploads_per_token":       {Label: "Max uploads per owner", Description: "Maximum number of uploads per account/token owner (0 = unlimited)"},
	"max_storage_per_token":       {Label: "Max storage per owner (bytes)", Description: "Maximum total storage per account/token owner (0 = unlimited)"},
	"storage":                     {Label: "Storage backend", Description: "file or s3 (requires restart to take effect)", IsStartup: true},
	"s3_endpoint":                 {Label: "S3 endpoint URL", Description: "S3-compatible endpoint (e.g. https://<id>.r2.cloudflarestorage.com)"},
	"s3_bucket":                   {Label: "S3 bucket", Description: "Bucket name for HTML file storage"},
	"s3_region":                   {Label: "S3 region", Description: "AWS region (e.g. us-east-1, auto)"},
	"s3_access_key":               {Label: "S3 access key", Description: "Access key ID for S3-compatible storage"},
	"s3_secret_key":               {Label: "S3 secret key", Description: "Secret access key for S3-compatible storage", IsSecret: true},
	"oauth_google_enabled":        {Label: "Google login", Description: "Enable Google OAuth login", IsBool: true},
	"oauth_google_client_id":      {Label: "Google client ID", Description: "OAuth web client ID"},
	"oauth_google_client_secret":  {Label: "Google client secret", Description: "OAuth web client secret", IsSecret: true},
	"oauth_github_enabled":        {Label: "GitHub login", Description: "Enable GitHub OAuth login", IsBool: true},
	"oauth_github_client_id":      {Label: "GitHub client ID", Description: "OAuth app client ID"},
	"oauth_github_client_secret":  {Label: "GitHub client secret", Description: "OAuth app client secret", IsSecret: true},
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	raw, err := s.encryptedGetAllSettings(r.Context())
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
	sort.Slice(out, func(i, j int) bool {
		return out[i].Key < out[j].Key
	})
	jsonOK(w, out)
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	var body map[string]string
	if err := decodeJSON(w, r, &body, defaultJSONBodyLimit); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	updates := make(map[string]string, len(body))
	for k, v := range body {
		normalized, err := s.normalizeSettingValue(k, v)
		if err != nil {
			jsonError(w, http.StatusBadRequest, err.Error())
			return
		}
		updates[k] = normalized
	}
	if err := s.validateAllowedEmailDomainUpdate(r.Context(), updates); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	for _, k := range s.settingKeys(updates) {
		v := updates[k]
		if err := s.encryptedSetSetting(r.Context(), k, v); err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	s.auditRequest(r, actorName(actor), "settings.update", strings.Join(s.settingKeys(updates), ","))
	jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) normalizeSettingValue(key, value string) (string, error) {
	meta, ok := settingsMeta[key]
	if !ok {
		return "", fmt.Errorf("unknown setting: %s", key)
	}
	value = strings.TrimSpace(value)
	if meta.IsBool {
		return normalizeBoolSetting(key, value)
	}
	switch key {
	case "max_upload":
		return normalizePositiveInt64Setting(key, value)
	case "max_total_size", "max_storage_per_token":
		return normalizeNonNegativeInt64Setting(key, value)
	case "max_uploads_per_token", "retention_days":
		return normalizeNonNegativeIntSetting(key, value)
	case authAllowedEmailDomainSetting:
		return normalizeAllowedEmailDomain(value)
	case "storage":
		switch strings.ToLower(value) {
		case "file", "s3":
			return strings.ToLower(value), nil
		default:
			return "", fmt.Errorf("%s must be file or s3", key)
		}
	case "s3_endpoint":
		if value != "" {
			if err := objectstore.ValidateS3Endpoint(value, s.s3AllowPrivateEndpoint); err != nil {
				return "", fmt.Errorf("invalid s3 endpoint: %w", err)
			}
		}
	}
	return value, nil
}

func normalizeBoolSetting(key, value string) (string, error) {
	switch strings.ToLower(value) {
	case "", "0", "false", "no", "off":
		return "", nil
	case "1", "true", "yes", "on":
		return "true", nil
	default:
		return "", fmt.Errorf("%s must be a boolean", key)
	}
}

func normalizePositiveInt64Setting(key, value string) (string, error) {
	n, err := parseInt64Setting(key, value)
	if err != nil {
		return "", err
	}
	if n <= 0 {
		return "", fmt.Errorf("%s must be a positive integer", key)
	}
	return strconv.FormatInt(n, 10), nil
}

func normalizeNonNegativeInt64Setting(key, value string) (string, error) {
	n, err := parseInt64Setting(key, value)
	if err != nil {
		return "", err
	}
	if n < 0 {
		return "", fmt.Errorf("%s must be a non-negative integer", key)
	}
	return strconv.FormatInt(n, 10), nil
}

func normalizeNonNegativeIntSetting(key, value string) (string, error) {
	n, err := strconv.Atoi(value)
	if err != nil || value == "" {
		return "", fmt.Errorf("%s must be a non-negative integer", key)
	}
	if n < 0 {
		return "", fmt.Errorf("%s must be a non-negative integer", key)
	}
	return strconv.Itoa(n), nil
}

func parseInt64Setting(key, value string) (int64, error) {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value == "" {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return n, nil
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
	updates := map[string]string{}
	for k, meta := range settingsMeta {
		if meta.IsBool {
			normalized, err := s.normalizeSettingValue(k, r.FormValue(k))
			if err != nil {
				dashboardError(w, r, err.Error())
				return
			}
			updates[k] = normalized
			continue
		}
		v, submitted, err := dashboardSubmittedSettingValue(k, r.PostForm)
		if err != nil {
			dashboardError(w, r, err.Error())
			return
		}
		if !submitted {
			continue
		}
		if v == "" && meta.IsSecret {
			continue
		}
		normalized, err := s.normalizeSettingValue(k, v)
		if err != nil {
			dashboardError(w, r, err.Error())
			return
		}
		updates[k] = normalized
	}
	if err := s.validateAllowedEmailDomainUpdate(r.Context(), updates); err != nil {
		dashboardError(w, r, err.Error())
		return
	}
	for _, k := range s.settingKeys(updates) {
		if err := s.encryptedSetSetting(r.Context(), k, updates[k]); err != nil {
			dashboardError(w, r, "settings update failed")
			return
		}
	}
	s.auditRequest(r, owner.Name, "settings.update", "via dashboard")
	dashboardOK(w, r, "settings saved")
}

func dashboardSubmittedSettingValue(key string, form map[string][]string) (string, bool, error) {
	if dashboardSettingUsesMegabytes(key) {
		if values, submitted := form[key+"_mb"]; submitted {
			v := firstPostValue(values)
			bytes, err := dashboardMegabytesToBytes(key, v)
			return bytes, true, err
		}
	}
	values, submitted := form[key]
	if !submitted {
		return "", false, nil
	}
	return firstPostValue(values), true, nil
}

func firstPostValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func dashboardMegabytesToBytes(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	mb, err := strconv.ParseInt(value, 10, 64)
	if err != nil || value == "" {
		return "", fmt.Errorf("%s must be an integer", key)
	}
	if mb < 0 {
		return strconv.FormatInt(mb, 10), nil
	}
	if mb > maxDashboardMegabytes {
		return "", fmt.Errorf("%s is too large", key)
	}
	return strconv.FormatInt(mb*dashboardMegabyte, 10), nil
}

func dashboardSettingUsesMegabytes(key string) bool {
	switch key {
	case "max_upload", "max_total_size", "max_storage_per_token":
		return true
	default:
		return false
	}
}

func (s *Server) dashboardSettingsMap(ctx context.Context) map[string]string {
	raw, _ := s.encryptedGetAllSettings(ctx)
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

func dashboardSettingsPanel(raw map[string]string) webui.DashboardSettings {
	return webui.DashboardSettings{
		Auth: webui.AuthSettings{
			Token:  dashboardSettingRow(raw, "auth_token_login_enabled"),
			Domain: dashboardSettingRow(raw, authAllowedEmailDomainSetting),
			Google: dashboardOAuthProviderSettings(raw, "google", "Google"),
			GitHub: dashboardOAuthProviderSettings(raw, "github", "GitHub"),
		},
		Storage: dashboardStorageSettings(raw),
		Limits: []webui.LimitSetting{
			dashboardMegabyteLimit(raw, "max_upload", "maxUpload", "Max upload size", "Maximum size per HTML file upload", 1, maxInt64(1024, dashboardSettingMegabytes(raw, "max_upload")), 1),
			dashboardMegabyteLimit(raw, "max_total_size", "maxTotalSize", "Total storage limit", "Cumulative storage across all uploads (0 = unlimited)", 0, maxInt64(102400, dashboardSettingMegabytes(raw, "max_total_size")), 1),
			dashboardCountLimit(raw, "max_uploads_per_token", "maxUploadsPerOwner", "Max uploads per owner", "Maximum uploads per account/token owner (0 = unlimited)", "uploads", 0, maxInt64(1000, dashboardSettingInt64(raw, "max_uploads_per_token")), 1),
			dashboardMegabyteLimit(raw, "max_storage_per_token", "maxStoragePerOwner", "Storage per owner", "Maximum storage per account/token owner (0 = unlimited)", 0, maxInt64(10240, dashboardSettingMegabytes(raw, "max_storage_per_token")), 1),
			dashboardCountLimit(raw, "retention_days", "retentionDays", "Retention", "Auto-delete uploads older than this many days (0 = off)", "days", 0, maxInt64(365, dashboardSettingInt64(raw, "retention_days")), 1),
		},
	}
}

func dashboardOAuthProviderSettings(raw map[string]string, key, name string) webui.OAuthProviderSettings {
	enabled := dashboardSettingRow(raw, "oauth_"+key+"_enabled")
	return webui.OAuthProviderSettings{
		Key:          key,
		Name:         name,
		Enabled:      enabled,
		ClientID:     dashboardSettingRow(raw, "oauth_"+key+"_client_id"),
		ClientSecret: dashboardSettingRow(raw, "oauth_"+key+"_client_secret"),
		EnabledValue: settingValueBool(enabled.Value),
	}
}

func dashboardStorageSettings(raw map[string]string) webui.StorageSettings {
	value := strings.ToLower(strings.TrimSpace(raw["storage"]))
	if value != "s3" {
		value = "file"
	}
	return webui.StorageSettings{
		Backend:    dashboardSettingRow(raw, "storage"),
		Value:      value,
		S3Selected: value == "s3",
		S3Settings: []webui.SettingRow{
			dashboardSettingRow(raw, "s3_endpoint"),
			dashboardSettingRow(raw, "s3_bucket"),
			dashboardSettingRow(raw, "s3_region"),
			dashboardSettingRow(raw, "s3_access_key"),
			dashboardSettingRow(raw, "s3_secret_key"),
		},
	}
}

func dashboardSettingRow(raw map[string]string, key string) settingsRow {
	meta := settingsMeta[key]
	meta.Key = key
	meta.Value = raw[key]
	if meta.IsSecret && meta.Value != "" {
		meta.Value = ""
		meta.Description = meta.Description + " (leave blank to keep current value)"
	}
	return meta
}

func dashboardMegabyteLimit(raw map[string]string, key, jsKey, label, description string, min, max, step int64) webui.LimitSetting {
	return webui.LimitSetting{
		Key:         key,
		FormKey:     key + "_mb",
		JSKey:       jsKey,
		Label:       label,
		Description: description,
		Unit:        "MB",
		Value:       dashboardSettingMegabytes(raw, key),
		Min:         min,
		Max:         max,
		Step:        step,
	}
}

func dashboardCountLimit(raw map[string]string, key, jsKey, label, description, unit string, min, max, step int64) webui.LimitSetting {
	return webui.LimitSetting{
		Key:         key,
		FormKey:     key,
		JSKey:       jsKey,
		Label:       label,
		Description: description,
		Unit:        unit,
		Value:       dashboardSettingInt64(raw, key),
		Min:         min,
		Max:         max,
		Step:        step,
	}
}

func dashboardSettingMegabytes(raw map[string]string, key string) int64 {
	bytes := dashboardSettingInt64(raw, key)
	if bytes <= 0 {
		return 0
	}
	mb := bytes / dashboardMegabyte
	if bytes%dashboardMegabyte != 0 {
		mb++
	}
	return mb
}

func dashboardSettingInt64(raw map[string]string, key string) int64 {
	value := strings.TrimSpace(raw[key])
	if value == "" {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func settingValueBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func dashboardSettingsRows(raw map[string]string) []settingsRow {
	order := []string{
		"auth_token_login_enabled", authAllowedEmailDomainSetting,
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

func (s *Server) settingString(ctx context.Context, key string) string {
	v, err := s.encryptedGetSetting(ctx, key)
	if err != nil {
		return ""
	}
	return v
}

func (s *Server) settingBool(ctx context.Context, key string) bool {
	switch s.settingString(ctx, key) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
