package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	_ "golang.org/x/image/webp"

	"wercture/server/internal/auth"
	"wercture/server/internal/preview"
	"wercture/server/internal/store"
	"wercture/server/internal/webui"
)

var (
	errTooLarge = errors.New("too large")
	errBadType  = errors.New("bad type")
)

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	_ = user
	users, err := s.store.ListUsers()
	if err != nil {
		writeError(w, err)
		return
	}
	if users == nil {
		users = []store.User{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if _, err := s.admin(r); err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		Username string `json:"username"`
	}
	if err := readJSON(r, &body); err != nil || !auth.ValidUsername(body.Username) {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	user, err := s.store.CreateUser(body.Username)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	if _, err := s.admin(r); err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	var body struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := auth.ValidatePassword([]byte(body.Password)); err != nil {
		writeError(w, err)
		return
	}
	if body.Password != body.Confirm {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if !s.beginHash() {
		writeCode(w, http.StatusTooManyRequests, "locked")
		return
	}
	defer s.endHash()
	phc, err := s.hasher.Hash([]byte(body.Password))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.SetPassword(id, phc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setUserEnabled(w http.ResponseWriter, r *http.Request) {
	actor, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.SetUserEnabled(actor, id, body.Enabled); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "trash"
	}
	if _, err := s.store.DeleteUser(user, id, mode); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listWarehouses(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := s.store.ListWarehouses(user)
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = []store.Warehouse{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"warehouses": items})
}

func (s *Server) createWarehouse(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	warehouse, err := s.store.CreateWarehouse(user, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, warehouse)
}

func (s *Server) getWarehouse(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	warehouse, err := s.store.Warehouse(user, id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, warehouse)
}

func (s *Server) deleteWarehouse(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.DeleteWarehouse(user, id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	items, next, err := s.store.ListImages(user, id, before, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": presentImages(r, items), "next": next})
}

func (s *Server) uploadSession(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if _, err := s.store.Warehouse(user, id); err != nil {
		writeError(w, err)
		return
	}
	image, err := s.acceptUpload(r, id, user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, absoluteLinks(r, image))
}

func (s *Server) uploadToken(w http.ResponseWriter, r *http.Request) {
	plain := bearer(r)
	if plain == "" {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	warehouseID, ownerID, err := s.store.WarehouseByToken(plain)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeCode(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		writeError(w, err)
		return
	}
	image, err := s.acceptUpload(r, warehouseID, ownerID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, absoluteLinks(r, image))
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	items, err := s.store.ListTokens(user, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = []store.Token{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": items})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	token, err := s.store.CreateToken(user, id, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, token)
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.RevokeToken(user, id); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) renameImage(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	image, err := s.store.RenameImage(user, id, body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, absoluteLinks(r, image))
}

func (s *Server) moveImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs         []int64 `json:"ids"`
		WarehouseID int64   `json:"warehouse_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.MoveImages(user, body.IDs, body.WarehouseID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) copyImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs         []int64 `json:"ids"`
		WarehouseID int64   `json:"warehouse_id"`
	}
	if err := readJSON(r, &body); err != nil || len(body.IDs) == 0 || len(body.IDs) > 100 {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if _, err := s.store.Warehouse(user, body.WarehouseID); err != nil {
		writeError(w, err)
		return
	}
	// Validate all sources before creating any files.
	sources := make([]store.Image, 0, len(body.IDs))
	for _, id := range body.IDs {
		image, err := s.store.ImageForUser(user, id)
		if err != nil {
			writeError(w, err)
			return
		}
		if image.DeletedAt != "" || image.WarehouseID == nil {
			writeCode(w, http.StatusBadRequest, "invalid")
			return
		}
		sources = append(sources, image)
	}
	staged := make([]string, 0, len(sources))
	committed := false
	defer func() {
		if !committed {
			for _, path := range staged {
				removeStored(path)
			}
		}
	}()
	records := make([]store.NewImage, 0, len(sources))
	for _, image := range sources {
		data, err := os.ReadFile(s.store.FilePath(image))
		if err != nil {
			writeError(w, err)
			return
		}
		publicID, err := newPublicID()
		if err != nil {
			writeError(w, err)
			return
		}
		rel := filepath.Join("files", publicID)
		abs := filepath.Join(s.store.DataDir(), rel)
		staged = append(staged, abs)
		if err := writeFile(abs, data); err != nil {
			writeError(w, err)
			return
		}
		s.documentPreview(abs, image.MIME, data)
		records = append(records, store.NewImage{
			PublicID: publicID, Name: image.Name, MIME: image.MIME, Size: image.Size,
			Width: image.Width, Height: image.Height, StoragePath: rel, WarehouseID: body.WarehouseID, UploaderID: user.ID,
		})
	}
	if _, err := s.store.InsertImages(records); err != nil {
		writeError(w, err)
		return
	}
	committed = true

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) trashImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.TrashImages(user, body.IDs); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) listTrash(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := s.store.ListTrash(user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"images": presentImages(r, items)})
}

func (s *Server) restoreImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.RestoreImages(user, body.IDs); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) moveTrash(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs         []int64 `json:"ids"`
		WarehouseID int64   `json:"warehouse_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.MoveTrashImages(user, body.IDs, body.WarehouseID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) purgeImages(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.PurgeImages(user, body.IDs); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) emptyTrash(w http.ResponseWriter, r *http.Request) {
	user, err := s.admin(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	if _, err := s.store.EmptyTrash(user); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) rawImage(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	image, err := s.store.ImageForUser(user, id)
	if err != nil {
		writeError(w, err)
		return
	}
	serveFile(w, r, image, s.store.FilePath(image), false)
}

func (s *Server) previewImage(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	image, err := s.store.ImageForUser(user, id)
	if err != nil {
		writeError(w, err)
		return
	}
	if image.MIME != "application/pdf" && image.MIME != "text/markdown" {
		http.NotFound(w, r)
		return
	}
	s.servePreview(w, r, image, false)
}

func (s *Server) publicImage(w http.ResponseWriter, r *http.Request) {
	id, ext := publicRef(r.PathValue("publicID"))
	image, path, err := s.store.PublicFile(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ext == ".html" {
		if image.MIME != "text/markdown" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("raw") == "1" {
			serveFile(w, r, image, path, true)
			return
		}
		s.serveMarkdownPage(w, r)
		return
	}
	if ext == ".preview.png" {
		if image.MIME == "application/pdf" || image.MIME == "text/markdown" {
			s.servePreview(w, r, image, true)
			return
		}
		http.Redirect(w, r, "/i/"+image.PublicID+store.PublicExt(image.MIME), http.StatusPermanentRedirect)
		return
	}
	want := store.PublicExt(image.MIME)
	if ext != "" && ext != want {
		http.Redirect(w, r, "/i/"+image.PublicID+want, http.StatusPermanentRedirect)
		return
	}
	if image.MIME == "text/markdown" && r.URL.Query().Get("raw") != "1" {
		s.serveMarkdownPage(w, r)
		return
	}
	serveFile(w, r, image, path, true)
}

func (s *Server) serveMarkdownPage(w http.ResponseWriter, r *http.Request) {
	page, err := webui.MarkdownPage()
	if err != nil {
		http.Error(w, "page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; worker-src 'self' blob:; frame-ancestors 'self'; object-src 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(page)
}

// publicRef splits /i/{id}.jpg into the stored id and a lowercase suffix.
// A link with no suffix stays valid.
func publicRef(raw string) (string, string) {
	lower := strings.ToLower(raw)
	for _, ext := range []string{".preview.png", ".jpeg", ".jpg", ".png", ".gif", ".webp", ".html", ".pdf", ".markdown", ".md"} {
		if strings.HasSuffix(lower, ext) && len(raw) > len(ext) {
			return raw[:len(raw)-len(ext)], ext
		}
	}
	return raw, ""
}

func (s *Server) acceptUpload(r *http.Request, warehouseID, uploaderID int64) (store.Image, error) {
	data, name, err := readUpload(r.Body, r.Header.Get("Content-Type"))
	if err != nil {
		return store.Image{}, err
	}
	kind, ok := sniff(data, name)
	if !ok {
		return store.Image{}, errBadType
	}
	width, height := 0, 0
	if strings.HasPrefix(kind, "image/") {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || cfg.Width < 1 || cfg.Height < 1 {
			return store.Image{}, errBadType
		}
		width, height = cfg.Width, cfg.Height
	} else if kind == "application/pdf" || kind == "text/markdown" {
		width, height = 900, 1200
	}
	publicID, err := newPublicID()
	if err != nil {
		return store.Image{}, err
	}
	rel := filepath.Join("files", publicID)
	abs := filepath.Join(s.store.DataDir(), rel)
	if err := writeFile(abs, data); err != nil {
		return store.Image{}, err
	}
	s.documentPreview(abs, kind, data)
	image, err := s.store.InsertImage(store.NewImage{
		PublicID: publicID, Name: cleanName(name), MIME: kind, Size: int64(len(data)),
		Width: width, Height: height, StoragePath: rel, WarehouseID: warehouseID, UploaderID: uploaderID,
	})
	if err != nil {
		removeStored(abs)
		return store.Image{}, err
	}
	return image, nil
}

func (s *Server) admin(r *http.Request) (store.User, error) {
	user, err := s.current(r)
	if err != nil {
		return store.User{}, err
	}
	if user.Role != "admin" {
		return store.User{}, store.ErrForbidden
	}
	return user, nil
}

func presentImages(r *http.Request, items []store.Image) []any {
	if items == nil {
		return []any{}
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, absoluteLinks(r, item))
	}
	return out
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(value[len(prefix):])
}

func readUpload(body io.Reader, contentType string) ([]byte, string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return nil, "", errBadType
	}
	reader := multipart.NewReader(io.LimitReader(body, maxUpload+1<<20), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, "", errBadType
		}
		if err != nil {
			var max *http.MaxBytesError
			if errors.As(err, &max) {
				return nil, "", errTooLarge
			}
			return nil, "", errBadType
		}
		if part.FormName() != "file" {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(part, maxUpload+1))
		_ = part.Close()
		if err != nil {
			return nil, "", err
		}
		if len(data) == 0 {
			return nil, "", errBadType
		}
		if len(data) > maxUpload {
			return nil, "", errTooLarge
		}
		return data, part.FileName(), nil
	}
}

func sniff(data []byte, name string) (string, bool) {
	switch {
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg", true
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png", true
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return "image/gif", true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", true
	case bytes.HasPrefix(data, []byte("%PDF-")):
		return "application/pdf", true
	case markdownFile(name, data):
		return "text/markdown", true
	default:
		return "", false
	}
}

func markdownFile(name string, data []byte) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".mdown":
	default:
		return false
	}
	return len(bytes.TrimSpace(data)) > 0 && utf8.Valid(data) && !bytes.Contains(data, []byte{0})
}

func newPublicID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func cleanName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		return "image"
	}
	runes := []rune(name)
	if len(runes) > 255 {
		runes = runes[:255]
	}
	return string(runes)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func removeStored(path string) {
	_ = os.Remove(path + ".tmp")
	_ = os.Remove(path)
	_ = os.Remove(path + ".preview.png")
}

var (
	fontOnce sync.Once
	fontData []byte
)

func documentFont() []byte {
	fontOnce.Do(func() {
		fontData, _ = webui.Font()
	})
	return fontData
}

func (s *Server) documentPreview(path, mime string, data []byte) {
	if mime != "application/pdf" && mime != "text/markdown" {
		return
	}
	_ = preview.Write(path+".preview.png", mime, data, documentFont())
}

func (s *Server) servePreview(w http.ResponseWriter, r *http.Request, image store.Image, public bool) {
	file := s.store.FilePath(image)
	previewPath := file + ".preview.png"
	if _, err := os.Stat(previewPath); err != nil {
		data, err := os.ReadFile(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if err := preview.Write(previewPath, image.MIME, data, documentFont()); err != nil {
			http.Error(w, "preview unavailable", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if public {
		w.Header().Set("Cache-Control", publicCacheControl)
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	http.ServeFile(w, r, previewPath)
}

func nameWithExt(name, ext string) string {
	if ext == "" {
		return name
	}
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ext) || (ext == ".jpg" && strings.HasSuffix(lower, ".jpeg")) {
		return name
	}
	for _, old := range []string{".jpeg", ".jpg", ".png", ".gif", ".webp", ".pdf", ".markdown", ".md"} {
		if strings.HasSuffix(lower, old) {
			return name[:len(name)-len(old)] + ext
		}
	}
	return name + ext
}

func inlineDisposition(name string) string {
	name = strings.NewReplacer("\r", "", "\n", "", `"`, "").Replace(name)
	value := mime.FormatMediaType("inline", map[string]string{"filename": name})
	if value == "" {
		return `inline; filename="file"`
	}
	return value
}

func serveFile(w http.ResponseWriter, r *http.Request, image store.Image, path string, public bool) {
	name := image.Name
	if public {
		name = nameWithExt(name, store.PublicExt(image.MIME))
	}
	contentType := image.MIME
	if contentType == "text/markdown" {
		contentType = "text/markdown; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", inlineDisposition(name))
	if public {
		w.Header().Set("Cache-Control", publicCacheControl)
	} else {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	http.ServeFile(w, r, path)
}

const publicCacheControl = "public, max-age=0, must-revalidate"
