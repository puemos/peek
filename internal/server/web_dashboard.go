package server

import (
	"io"
	"log/slog"
	"net/http"
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
	var listErr error
	if owner.IsAdmin {
		list, listErr = s.store.ListAllUploads()
	} else {
		list, listErr = s.store.ListUploadsByOwner(owner.ID)
	}
	if listErr != nil {
		slog.Error("dashboard upload list failed", "account_id", owner.ID, "admin", owner.IsAdmin, "err", listErr)
	}
	uploads := make([]dashUpload, 0, len(list))
	for _, u := range list {
		uploads = append(uploads, dashUpload{
			Slug: u.Slug, Filename: u.Filename,
			SizeHuman: humanSize(u.Size), Protected: u.PasswordHash != "",
			CreatedHuman: u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	csrf, ok := s.csrfToken(w)
	if !ok {
		return
	}
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
	if listErr != nil {
		dashData_.UploadError = "uploads could not be loaded"
	}
	if owner.IsAdmin {
		dashData_.Invites = s.dashboardInviteRows()
		dashData_.Accounts = s.dashboardAccountRows(owner.ID)
	}
	// carry over flash messages from query params
	if e := r.URL.Query().Get("err"); e != "" {
		dashData_.UploadError = e
	}
	if msg := r.URL.Query().Get("ok"); msg != "" {
		dashData_.FlashSuccess = msg
	}
	if url := r.URL.Query().Get("uploaded"); url != "" {
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
		dashboardError(w, r, "file too large or invalid form")
		return
	}
	validCSRF, err := s.validateCSRF(r, w, r.FormValue("csrf"))
	if err != nil {
		s.renderCSRFError(w, err)
		return
	}
	if !validCSRF {
		dashboardError(w, r, "invalid session")
		return
	}

	mode := r.FormValue("mode")
	password := strings.TrimSpace(r.FormValue("password"))
	filename := strings.TrimSpace(r.FormValue("filename"))

	var data []byte
	if mode == "paste" {
		html := strings.TrimSpace(r.FormValue("html"))
		if html == "" {
			dashboardError(w, r, "no html pasted")
			return
		}
		data = []byte(html)
		if filename == "" {
			filename = "pasted.html"
		}
	} else {
		file, header, err := r.FormFile("file")
		if err != nil {
			dashboardError(w, r, "no file selected")
			return
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil || int64(len(data)) > maxUpload {
			dashboardError(w, r, "file too large")
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
		logUploadError(err)
		dashboardError(w, r, uploadErrorMessage(err))
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+up.Slug+" file="+up.Filename+" size="+strconv.Itoa(up.Size))
	shareURL := up.URL
	dashboardUploaded(w, r, shareURL)
}

func (s *Server) handleDashboardDelete(w http.ResponseWriter, r *http.Request) {
	owner, ok := s.webAuth(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	slug := r.PathValue("slug")
	if !s.parseDashboardForm(w, r) {
		return
	}
	u, err := s.store.GetUpload(slug)
	if err != nil {
		dashboardError(w, r, "not found")
		return
	}
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
		dashboardError(w, r, "not owner")
		return
	}
	if err := s.deleteUpload(r.Context(), *u); err != nil {
		slog.Error("dashboard upload delete failed", "slug", slug, "err", err)
		dashboardError(w, r, "delete failed")
		return
	}
	s.auditRequest(r, owner.Name, "upload.delete", "slug="+slug+" file="+u.Filename)
	dashboardHome(w, r)
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
		s.renderWebError(w, http.StatusNotFound, "Stats not found", "Stats for this page could not be found.")
		return
	}
	if u.OwnerAccountID != owner.ID && !owner.IsAdmin {
		s.renderWebError(w, http.StatusNotFound, "Stats not found", "Stats for this page could not be found.")
		return
	}
	total, unique, err := s.store.CountVisits(u.ID)
	if err != nil {
		slog.Error("dashboard stats count failed", "slug", slug, "err", err)
		s.renderDashboardStatsError(w, slug, u.Filename)
		return
	}
	recent, err := s.store.RecentVisits(u.ID, 100)
	if err != nil {
		slog.Error("dashboard stats visits failed", "slug", slug, "err", err)
		s.renderDashboardStatsError(w, slug, u.Filename)
		return
	}
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

func (s *Server) renderDashboardStatsError(w http.ResponseWriter, slug, filename string) {
	s.renderHTML(w, http.StatusInternalServerError, webui.TemplateStats, statsData{
		Slug: slug, Filename: filename, Error: "stats could not be loaded",
	})
}
