package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/puemos/peek/internal/models"
)

type uploadResp struct {
	Slug string `json:"slug"`
	URL  string `json:"url"`
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)

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

	if len(data) == 0 {
		jsonError(w, http.StatusBadRequest, "empty file")
		return
	}
	if !looksLikeHTML(data) {
		jsonError(w, http.StatusUnsupportedMediaType, "file does not look like HTML")
		return
	}

	maxTotalSize := s.settingInt64("max_total_size", 0)
	if maxTotalSize > 0 {
		currentTotal, err := s.store.SumUploadSizes()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
		if currentTotal+int64(len(data)) > maxTotalSize {
			jsonError(w, http.StatusRequestEntityTooLarge, "total storage quota exceeded")
			return
		}
	}

	// Per-token quotas (0 = unlimited).
	maxUploadsPerToken := s.settingInt("max_uploads_per_token", 0)
	if maxUploadsPerToken > 0 {
		count, err := s.store.CountUploadsByOwner(owner.ID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
		if count >= maxUploadsPerToken {
			jsonError(w, http.StatusRequestEntityTooLarge, "per-token upload count quota exceeded")
			return
		}
	}
	maxStoragePerToken := s.settingInt64("max_storage_per_token", 0)
	if maxStoragePerToken > 0 {
		ownerTotal, err := s.store.SumUploadSizesByOwner(owner.ID)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
		if ownerTotal+int64(len(data)) > maxStoragePerToken {
			jsonError(w, http.StatusRequestEntityTooLarge, "per-token storage quota exceeded")
			return
		}
	}

	slug, err := generateSlug(s.store)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "slug generation failed")
		return
	}
	if err := s.storage.Save(r.Context(), slug, data); err != nil {
		jsonError(w, http.StatusInternalServerError, "storage failed")
		return
	}

	pwHash := ""
	if password != "" {
		if !validatePasswordLength(password) {
			jsonError(w, http.StatusBadRequest, "password must be 72 characters or fewer")
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "hash failed")
			return
		}
		pwHash = string(h)
	}
	if err := s.store.CreateUpload(slug, owner.ID, filename, int64(len(data)), pwHash); err != nil {
		_ = s.storage.Delete(r.Context(), slug)
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}
	s.auditRequest(r, owner.Name, "upload.create", "slug="+slug+" file="+filename+" size="+strconv.Itoa(len(data)))

	jsonOK(w, uploadResp{Slug: slug, URL: s.baseURL + "/p/" + slug})
}

func (s *Server) handleListUploads(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	var (
		list []models.Upload
		err  error
	)
	if owner.IsAdmin {
		list, err = s.store.ListAllUploads()
	} else {
		list, err = s.store.ListUploadsByOwner(owner.ID)
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
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	if err := s.store.DeleteUpload(u.ID); err != nil {
		jsonError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	s.auditRequest(r, owner.Name, "upload.delete", "slug="+slug+" file="+u.Filename)
	_ = s.storage.Delete(r.Context(), slug)
	jsonOK(w, map[string]any{"deleted": slug})
}

func (s *Server) handleSetPassword(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	var body struct {
		Password string `json:"password"`
		Clear    bool   `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "bad json")
		return
	}
	hash := ""
	if !body.Clear && body.Password != "" {
		if !validatePasswordLength(body.Password) {
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

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, err := s.store.CountVisits(u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	recent, err := s.store.RecentVisits(u.ID, 50)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type vrow struct {
		Name      string `json:"name"`
		IP        string `json:"ip"`
		UA        string `json:"user_agent"`
		Timestamp int64  `json:"visited_at"`
	}
	rows := make([]vrow, 0, len(recent))
	for _, v := range recent {
		rows = append(rows, vrow{Name: v.VisitorName, IP: v.IP, UA: v.UserAgent, Timestamp: v.VisitedAt.Unix()})
	}
	jsonOK(w, map[string]any{
		"slug":            slug,
		"filename":        u.Filename,
		"total_visits":    total,
		"unique_visitors": unique,
		"recent":          rows,
	})
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		ExpiresH int    `json:"expires_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		jsonError(w, http.StatusBadRequest, "name required")
		return
	}
	t, err := randID(24)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "token gen failed")
		return
	}
	var expiresAt int64
	if body.ExpiresH > 0 {
		expiresAt = time.Now().Add(time.Duration(body.ExpiresH) * time.Hour).Unix()
	}
	if err := s.store.CreateToken(t, strings.TrimSpace(body.Name), false, expiresAt); err != nil {
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}
	actor, _ := s.store.GetToken(bearerToken(r))
	s.auditRequest(r, actorName(actor), "token.create", "name="+body.Name)
	jsonOK(w, map[string]any{"token": t, "name": body.Name})
}

func (s *Server) handleExportUpload(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	u, err := s.store.GetUpload(slug)
	if err != nil {
		jsonError(w, http.StatusNotFound, "not found")
		return
	}
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	if u.OwnerTokenID != owner.ID && !owner.IsAdmin {
		jsonError(w, http.StatusForbidden, "not owner")
		return
	}
	total, unique, _ := s.store.CountVisits(u.ID)
	recent, _ := s.store.RecentVisits(u.ID, 500)
	comments, _ := s.store.ListComments(u.ID)

	type exportComment struct {
		Author    string `json:"author"`
		Body      string `json:"body"`
		Selector  string `json:"selector"`
		Text      string `json:"element_text"`
		CreatedAt int64  `json:"created_at"`
	}
	type exportVisit struct {
		Name      string `json:"name"`
		IP        string `json:"ip"`
		UA        string `json:"user_agent"`
		Timestamp int64  `json:"visited_at"`
	}
	export := map[string]any{
		"slug":            slug,
		"filename":        u.Filename,
		"size":            u.Size,
		"protected":       u.PasswordHash != "",
		"created_at":      u.CreatedAt.Unix(),
		"total_visits":    total,
		"unique_visitors": unique,
	}
	cmts := make([]exportComment, 0, len(comments))
	for _, c := range comments {
		cmts = append(cmts, exportComment{
			Author: c.AuthorName, Body: c.Body, Selector: c.ElementSelector,
			Text: c.ElementText, CreatedAt: c.CreatedAt.Unix(),
		})
	}
	export["comments"] = cmts
	visits := make([]exportVisit, 0, len(recent))
	for _, v := range recent {
		visits = append(visits, exportVisit{
			Name: v.VisitorName, IP: v.IP, UA: v.UserAgent, Timestamp: v.VisitedAt.Unix(),
		})
	}
	export["visits"] = visits

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+slug+`-export.json"`)
	_ = json.NewEncoder(w).Encode(export)
}

func (s *Server) handleDeleteAllByOwner(w http.ResponseWriter, r *http.Request) {
	tok := bearerToken(r)
	owner, _ := s.store.GetToken(tok)
	uploads, err := s.store.ListUploadsByOwner(owner.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	deleted := 0
	for _, u := range uploads {
		if err := s.store.DeleteUpload(u.ID); err != nil {
			continue
		}
		_ = s.storage.Delete(r.Context(), u.Slug)
		deleted++
	}
	s.auditRequest(r, owner.Name, "upload.delete_all", "count="+strconv.Itoa(deleted))
	jsonOK(w, map[string]any{"deleted": deleted})
}

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	entries, err := s.store.ListAuditLog(limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	type arow struct {
		ID        int64  `json:"id"`
		Actor     string `json:"actor"`
		Action    string `json:"action"`
		Detail    string `json:"detail"`
		IP        string `json:"ip"`
		CreatedAt int64  `json:"created_at"`
	}
	out := make([]arow, 0, len(entries))
	for _, e := range entries {
		out = append(out, arow{
			ID: e.ID, Actor: e.Actor, Action: e.Action,
			Detail: e.Detail, IP: e.IP, CreatedAt: e.CreatedAt.Unix(),
		})
	}
	jsonOK(w, out)
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListTokens()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	// Tokens are stored hashed and never returned after creation.
	type trow struct {
		ID        int64  `json:"id"`
		Name      string `json:"name"`
		Admin     bool   `json:"admin"`
		ExpiresAt int64  `json:"expires_at"`
	}
	out := make([]trow, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, trow{ID: t.ID, Name: t.Name, Admin: t.IsAdmin, ExpiresAt: t.ExpiresAt})
	}
	jsonOK(w, out)
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad token id")
		return
	}
	t, err := s.store.GetTokenByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "token not found")
		return
	}
	if t.IsAdmin {
		n, err := s.store.CountAdminTokens()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "db error")
			return
		}
		if n <= 1 {
			jsonError(w, http.StatusBadRequest, "cannot revoke the last admin token")
			return
		}
	}
	if err := s.store.DeleteToken(id); err != nil {
		jsonError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	actor, _ := s.store.GetToken(bearerToken(r))
	s.auditRequest(r, actorName(actor), "token.revoke", "id="+strconv.FormatInt(id, 10)+" name="+t.Name)
	jsonOK(w, map[string]any{"revoked": id})
}

// --- helpers ---

func actorName(t *models.Token) string {
	if t != nil {
		return t.Name
	}
	return "unknown"
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
