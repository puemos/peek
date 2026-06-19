package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

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

	r.Body = http.MaxBytesReader(w, r.Body, s.maxUpload+1024)

	var (
		data     []byte
		filename string
		password string
	)

	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(s.maxUpload); err != nil {
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
		data, err = io.ReadAll(io.LimitReader(file, s.maxUpload+1))
		if err != nil || int64(len(data)) > s.maxUpload {
			jsonError(w, http.StatusRequestEntityTooLarge, "file too large")
			return
		}
		filename = header.Filename
	} else {
		// Raw body upload.
		password = strings.TrimSpace(r.URL.Query().Get("password"))
		filename = r.URL.Query().Get("filename")
		if filename == "" {
			filename = "page.html"
		}
		var err error
		data, err = io.ReadAll(io.LimitReader(r.Body, s.maxUpload+1))
		if err != nil || int64(len(data)) > s.maxUpload {
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

	slug, err := randID(10)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "slug generation failed")
		return
	}
	if err := writeAtomic(s.uploadPath(slug), data); err != nil {
		jsonError(w, http.StatusInternalServerError, "storage failed")
		return
	}

	pwHash := ""
	if password != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "hash failed")
			return
		}
		pwHash = string(h)
	}
	if err := s.store.CreateUpload(slug, owner.ID, filename, int64(len(data)), pwHash); err != nil {
		_ = removeFile(s.uploadPath(slug))
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}

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
	_ = removeFile(s.uploadPath(slug))
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
		Name string `json:"name"`
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
	if err := s.store.CreateToken(t, strings.TrimSpace(body.Name), false); err != nil {
		jsonError(w, http.StatusInternalServerError, "db failed")
		return
	}
	jsonOK(w, map[string]any{"token": t, "name": body.Name})
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.store.ListTokens()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "db error")
		return
	}
	// Tokens are stored hashed and never returned after creation.
	type trow struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Admin bool   `json:"admin"`
	}
	out := make([]trow, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, trow{ID: t.ID, Name: t.Name, Admin: t.IsAdmin})
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
	jsonOK(w, map[string]any{"revoked": id})
}

// --- helpers ---

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
