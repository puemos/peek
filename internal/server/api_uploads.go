package server

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/models"
	"github.com/puemos/peek/internal/uploads"
)

type uploadResp struct {
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}

	maxUpload := s.settingInt64("max_upload", 2<<20)

	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024)

	var (
		data     []byte
		filename string
		password string
	)

	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(maxUpload); err != nil {
			jsonError(w, http.StatusBadRequest, "file too large or invalid form")
			return
		}
		password = strings.TrimSpace(r.FormValue("password"))
		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "missing 'file'")
			return
		}
		defer file.Close()
		data, err = io.ReadAll(io.LimitReader(file, maxUpload+1))
		if err != nil || int64(len(data)) > maxUpload {
			jsonError(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		filename = header.Filename
	} else {
		password = strings.TrimSpace(r.URL.Query().Get("password"))
		filename = r.URL.Query().Get("filename")
		if filename == "" {
			filename = "page.html"
		}
		var err error
		data, err = io.ReadAll(io.LimitReader(r.Body, maxUpload+1))
		if err != nil || int64(len(data)) > maxUpload {
			jsonError(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
	}

	up, err := s.uploadService().Create(r.Context(), uploads.CreateInput{
		OwnerAccountID: owner.AccountID,
		OwnerTokenID:   owner.ID,
		Filename:       filename,
		Password:       password,
		Data:           data,
		Limits:         s.uploadLimits(),
	})
	if err != nil {
		logUploadError(err)
		status, msg := uploadHTTPError(err)
		jsonError(w, status, msg)
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+up.Slug+" file="+up.Filename+" size="+strconv.Itoa(up.Size))

	jsonOK(w, uploadResp{Slug: up.Slug, URL: up.URL})
}

func (s *Server) handleListUploads(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	var (
		list []models.Upload
		err  error
	)
	if owner.IsAdmin {
		list, err = s.store.ListAllUploads()
	} else {
		list, err = s.store.ListUploadsByOwner(owner.AccountID)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type item struct {
		Slug      string `json:"slug"`
		Filename  string `json:"filename"`
		Owner     string `json:"owner"`
		Size      int64  `json:"size"`
		Protected bool   `json:"protected"`
		URL       string `json:"url"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]item, 0, len(list))
	for _, u := range list {
		out = append(out, item{
			Slug: u.Slug, Filename: u.Filename, Owner: u.OwnerName,
			Size: u.Size, Protected: u.PasswordHash != "",
			URL: s.baseURL + "/p/" + u.Slug, CreatedAt: u.CreatedAt.Unix(),
		})
	}
	jsonOK(w, out)
}

func (s *Server) handleDeleteUpload(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	if err := s.store.DeleteUpload(u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.auditRequest(r, owner.Name, "upload.delete", "slug="+slug+" file="+u.Filename)
	if err := s.storage.Delete(r.Context(), slug); err != nil {
		slog.Warn("api upload storage cleanup failed", "slug", slug, "err", err)
	}
	jsonOK(w, map[string]any{"deleted": slug})
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	if u.OwnerAccountID != owner.AccountID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	var body struct {
		Password string `json:"password"`
		Clear    bool   `json:"clear"`
	}
	if err := decodeJSON(w, r, &body, smallJSONBodyLimit); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	hash := ""
	if !body.Clear && body.Password != "" {
		if !uploads.ValidatePasswordLength(body.Password) {
			jsonError(w, http.StatusBadRequest, "password must be 72 characters or fewer")
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "hash failed")
			return
		}
		hash = string(h)
	}
	if err := s.store.SetUploadPassword(u.ID, hash); err != nil {
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}
	action := "cleared"
	if hash != "" {
		action = "set"
	}
	s.auditRequest(r, owner.Name, "upload.password."+action, "slug="+slug)
	jsonOK(w, map[string]any{"protected": hash != ""})
}

func (s *Server) handleDeleteAllByOwner(w http.ResponseWriter, r *http.Request) {
	owner, ok := requireAPIToken(w, r)
	if !ok {
		return
	}
	uploads, err := s.store.ListUploadsByOwner(owner.AccountID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	deleted := 0
	for _, u := range uploads {
		if err := s.store.DeleteUpload(u.ID); err != nil {
			slog.Warn("api upload delete_all db delete failed", "slug", u.Slug, "err", err)
			continue
		}
		if err := s.storage.Delete(r.Context(), u.Slug); err != nil {
			slog.Warn("api upload delete_all storage cleanup failed", "slug", u.Slug, "err", err)
		}
		deleted++
	}
	s.auditRequest(r, owner.Name, "upload.delete_all", "count="+strconv.Itoa(deleted))
	jsonOK(w, map[string]any{"deleted": deleted})
}
