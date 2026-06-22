package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"net/http"
)

const (
	TemplateSetup        = "setup"
	TemplateLogin        = "login"
	TemplateDashboard    = "dashboard"
	TemplateStats        = "stats"
	TemplatePage         = "page"
	TemplateGate         = "gate"
	TemplateIndex        = "index"
	TemplateError        = "error"
	TemplateCLILogin     = "cli-login"
	TemplateCLILoginDone = "cli-login-done"
)

// DashboardCSP is the Content-Security-Policy for the management UI.
// It restricts all resources to same-origin, with no inline scripts/styles.
// DashboardCSP is the Content-Security-Policy for the management UI.
// Alpine.js requires 'unsafe-eval' (expression evaluation) and 'unsafe-inline' for styles.
const DashboardCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-eval'; img-src 'self' data:; frame-ancestors 'none'"

// PageCSP is the Content-Security-Policy for the trusted parent page that
// hosts the uploaded HTML inside a sandboxed iframe.
const PageCSP = "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-eval'; frame-src 'self'"

//go:embed templates/*.gohtml
var templateFS embed.FS

type assetPath func(name string) string

type Renderer struct {
	tmpl *template.Template
}

func NewRenderer() (*Renderer, error) {
	return newRenderer(AssetURL)
}

func newRenderer(assetPath assetPath) (*Renderer, error) {
	if assetPath == nil {
		assetPath = func(name string) string { return "/" + name }
	}
	tmpl, err := template.New("peek").
		Funcs(template.FuncMap{"asset": assetPath}).
		ParseFS(templateFS, "templates/*.gohtml")
	if err != nil {
		return nil, err
	}
	return &Renderer{tmpl: tmpl}, nil
}

func (r *Renderer) Execute(name string, data any) ([]byte, error) {
	if r == nil || r.tmpl == nil {
		return nil, fmt.Errorf("renderer is not initialized")
	}
	var buf bytes.Buffer
	if err := r.tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("render %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func (r *Renderer) RenderHTML(w http.ResponseWriter, status int, name string, data any) error {
	body, err := r.Execute(name, data)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

type AuthProvider struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type SetupData struct {
	CSRF  string
	Code  string
	Error string
}

type LoginData struct {
	CSRF          string
	Error         string
	Invite        bool
	Providers     []AuthProvider
	PasswordLogin bool
	TokenLogin    bool
	OAuthEnabled  bool
}

type DashboardUpload struct {
	Slug         string
	Name         string
	SizeHuman    string
	Visibility   string
	CreatedHuman string
}

type InviteDashboardRow struct {
	ID        int64
	Email     string
	Status    string
	Expires   string
	Link      string
	CanRevoke bool
}

type AccountDashboardRow struct {
	ID       int64
	Name     string
	Email    string
	Admin    bool
	Disabled bool
	IsSelf   bool
}

type SettingRow struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description"`
	IsSecret    bool   `json:"is_secret"`
	IsStartup   bool   `json:"is_startup"`
	IsBool      bool   `json:"is_bool"`
}

type DashboardSettings struct {
	Auth    AuthSettings
	Storage StorageSettings
	Limits  []LimitSetting
}

type AuthSettings struct {
	Token     SettingRow
	Domain    SettingRow
	Google    OAuthProviderSettings
	GitHub    OAuthProviderSettings
	OIDC      OAuthProviderSettings
	Providers []OAuthProviderSettings
}

type OAuthProviderSettings struct {
	Key          string
	Name         string
	Enabled      SettingRow
	ClientID     SettingRow
	ClientSecret SettingRow
	Fields       []SettingRow
	EnabledValue bool
}

type StorageSettings struct {
	Backend    SettingRow
	Value      string
	S3Selected bool
	S3Settings []SettingRow
}

type LimitSetting struct {
	Key         string
	FormKey     string
	JSKey       string
	Label       string
	Description string
	Unit        string
	Value       int64
	Min         int64
	Max         int64
	Step        int64
}

type DashboardData struct {
	CSRF             string
	User             string
	IsAdmin          bool
	Settings         map[string]string
	SettingsMeta     []SettingRow
	SettingsPanel    DashboardSettings
	Invites          []InviteDashboardRow
	Accounts         []AccountDashboardRow
	Uploads          []DashboardUpload
	UploadError      string
	FlashSuccess     string
	UploadSuccessURL string
}

type StatsVisit struct {
	Name      string
	IP        string
	UA        string
	WhenHuman string
}

type StatsSparkline struct {
	Summary  string
	LinePath string
	AreaPath string
	LastX    string
	LastY    string
}

type StatsData struct {
	Slug           string
	Name           string
	TotalVisits    int
	UniqueVisitors int
	Sparkline      StatsSparkline
	Recent         []StatsVisit
	Error          string
}

type PageData struct {
	Name       string
	Slug       string
	RawURL     string
	Visibility string
}

type GateData struct {
	Slug  string
	Error bool
}

type IndexData struct {
	BaseURL string
}

type ErrorData struct {
	Title   string
	Message string
}

type CLILoginData struct {
	Code  string
	CSRF  string
	User  string
	Error string
}

type CLILoginDoneData struct {
	Title   string
	Message string
}
