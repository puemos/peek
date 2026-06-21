package server

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/puemos/peek/internal/models"
	"github.com/puemos/peek/internal/uploads"
	webui "github.com/puemos/peek/internal/web"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	if s.setupRequired() {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	var list []models.Upload
	if owner.IsAdmin {
		list, _ = s.store.ListAllUploads()
	} else {
		list, _ = s.store.ListUploadsByOwner(owner.ID)
	}
	uploads := make([]dashUpload, 0, len(list))
	for _, u := range list {
		uploads = append(uploads, dashUpload{
			Slug: u.Slug, Filename: u.Filename,
			SizeHuman: humanSize(u.Size), Protected: u.PasswordHash != "",
			CreatedHuman: u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	csrf := s.newCSRF(w)
	allSettings := s.dashboardSettingsMap()
	sortedMeta := dashboardSettingsRows(allSettings)
	dashData_ := dashData{
		CSRF:         csrf,
		User:         owner.Name,
		IsAdmin:      owner.IsAdmin,
		Settings:     allSettings,
		SettingsMeta: sortedMeta,
		Uploads:      uploads,
	}
	if owner.IsAdmin {
		dashData_.Invites = s.dashboardInviteRows()
		dashData_.Accounts = s.dashboardAccountRows(owner.ID)
	}
	// carry over flash messages from query params
	if e := r.URL.Query().Get("err"); e != "" {
		dashData_.UploadError = e
	}
	if url := r.URL.Query().Get("ok"); url != "" {
		dashData_.UploadSuccess = true
		dashData_.UploadSuccessURL = url
	}
	s.renderHTML(w, http.StatusOK, webui.TemplateDashboard, dashData_)
}

func (s *Server) handleDashboardUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	maxUpload := s.settingInt64("max_upload", 2<<20)

	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Redirect(w, r, "/dashboard?err=file+too+large+or+invalid+form", http.StatusSeeOther)
		return
	}
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}

	mode := r.FormValue("mode")
	password := strings.TrimSpace(r.FormValue("password"))
	filename := strings.TrimSpace(r.FormValue("filename"))

	var data []byte
	if mode == "paste" {
		html := strings.TrimSpace(r.FormValue("html"))
		if html == "" {
			http.Redirect(w, r, "/dashboard?err=no+html+pasted", http.StatusSeeOther)
			return
		}
		data = []byte(html)
		if filename == "" {
			filename = "pasted.html"
		}
	} else {
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Redirect(w, r, "/dashboard?err=no+file+selected", http.StatusSeeOther)
			return
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil || int64(len(data)) > maxUpload {
			http.Redirect(w, r, "/dashboard?err=file+too+large", http.StatusSeeOther)
			return
		}
		if filename == "" {
			filename = header.Filename
		}
	}

	up, err := s.uploadService().Create(r.Context(), uploads.CreateInput{
		OwnerAccountID: owner.ID,
		Filename:       filename,
		Password:       password,
		Data:           data,
		Limits:         s.uploadLimits(),
	})
	if err != nil {
		if ue, ok := err.(*uploads.Error); ok {
			dashboardError(w, r, ue.Message)
		} else {
			dashboardError(w, r, "upload failed")
		}
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+up.Slug+" file="+up.Filename+" size="+strconv.Itoa(up.Size))
	shareURL := up.URL
	http.Redirect(w, r, "/dashboard?ok="+url.QueryEscape(shareURL), http.StatusSeeOther)
}

func (s *Server) handleDashboardDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := r.PathValue("slug")
	r.ParseForm()
	if !s.validateCSRF(r, w, r.FormValue("csrf")) {
		http.Redirect(w, r, "/dashboard?err=invalid+session", http.StatusSeeOther)
		return
	}
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.Redirect(w, r, "/dashboard?err=not+found", http.StatusSeeOther)
		return
	}
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
		http.Redirect(w, r, "/dashboard?err=not+owner", http.StatusSeeOther)
		return
	}
	_ = s.store.DeleteUpload(u.ID)
	_ = s.storage.Delete(r.Context(), slug)
	s.auditRequest(r, owner.Name, "upload.delete", "slug="+slug+" file="+u.Filename)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	w.Header().Set("Content-Security-Policy", webui.DashboardCSP)
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
		http.NotFound(w, r)
		return
	}
	total, unique, _ := s.store.CountVisits(u.ID)
	recent, _ := s.store.RecentVisits(u.ID, 100)
	visits := make([]statsVisit, 0, len(recent))
	for _, v := range recent {
		name := v.VisitorName
		if name == "" {
			name = ""
		}
		visits = append(visits, statsVisit{
			Name: name, IP: v.IP, UA: v.UserAgent,
			WhenHuman: v.VisitedAt.Format("2006-01-02 15:04"),
		})
	}
	s.renderHTML(w, http.StatusOK, webui.TemplateStats, statsData{
		Slug: slug, Filename: u.Filename,
		TotalVisits: total, UniqueVisitors: unique, Recent: visits,
	})
}
