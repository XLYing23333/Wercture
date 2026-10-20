package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"wercture/server/internal/auth"
	"wercture/server/internal/store"
)

var errUnauthorized = errors.New("unauthorized")

func readJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeCode(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errUnauthorized):
		writeCode(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, store.ErrNotFound):
		writeCode(w, http.StatusNotFound, "notfound")
	case errors.Is(err, store.ErrForbidden):
		writeCode(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, store.ErrExists):
		writeCode(w, http.StatusConflict, "exists")
	case errors.Is(err, store.ErrLastAdmin):
		writeCode(w, http.StatusBadRequest, "last_admin")
	case errors.Is(err, store.ErrWarehouseGone):
		writeCode(w, http.StatusConflict, "warehouse_gone")
	case errors.Is(err, auth.ErrPasswordLength):
		writeCode(w, http.StatusBadRequest, "weak_password")
	case errors.Is(err, auth.ErrUsername), errors.Is(err, store.ErrInvalid):
		writeCode(w, http.StatusBadRequest, "invalid")
	case errors.Is(err, errTooLarge):
		writeCode(w, http.StatusRequestEntityTooLarge, "toolarge")
	case errors.Is(err, errBadType):
		writeCode(w, http.StatusBadRequest, "badtype")
	default:
		writeCode(w, http.StatusBadRequest, "invalid")
	}
}

func publicScheme(r *http.Request) string {
	if scheme := strings.ToLower(firstHeader(r, "X-Forwarded-Proto")); scheme == "https" || scheme == "http" {
		return scheme
	}
	return "http"
}

func publicHost(r *http.Request) string {
	if host := firstHeader(r, "X-Forwarded-Host"); host != "" {
		return host
	}
	return r.Host
}

func publicOrigin(r *http.Request) string {
	return publicScheme(r) + "://" + publicHost(r)
}

func firstHeader(r *http.Request, name string) string {
	value := r.Header.Get(name)
	if i := strings.IndexByte(value, ','); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

func originOK(r *http.Request) bool {
	raw := r.Header.Get("Origin")
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	return strings.EqualFold(parsed.Host, publicHost(r))
}

func absoluteLinks(r *http.Request, image store.Image) map[string]any {
	link := publicOrigin(r) + image.Path
	alt := strings.NewReplacer("\r", "", "\n", "", `"`, "'").Replace(image.Name)
	out := map[string]any{
		"id": image.ID, "public_id": image.PublicID, "name": image.Name, "mime": image.MIME,
		"size": image.Size, "width": image.Width, "height": image.Height,
		"warehouse_id": image.WarehouseID, "warehouse_name": image.WarehouseName,
		"can_restore": image.CanRestore, "uploaded_at": image.UploadedAt, "deleted_at": image.DeletedAt,
		"path": image.Path, "url": link,
		"markdown": "![" + alt + "](" + link + ")",
		"html":     `<img src="` + link + `" alt="` + alt + `">`,
	}
	if image.MIME == "text/markdown" {
		renderPath := "/i/" + image.PublicID + ".html"
		out["render_path"] = renderPath
		out["render_url"] = publicOrigin(r) + renderPath
	}
	return out
}
