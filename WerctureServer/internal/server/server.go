// Package server is the HTTP API and embedded management UI.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wercture/server/internal/auth"
	"wercture/server/internal/store"
	"wercture/server/internal/webui"
)

const (
	cookieName    = "wercture_session"
	setupHeader   = "X-Wercture-Setup"
	joinHeader    = "X-Wercture-Join"
	maxUpload     = 32 << 20
	loginWindow   = 10 * time.Minute
	loginBan      = 30 * time.Minute
	loginFailures = 5
)

// Server serves the management UI, the upload API, and public image links.
type Server struct {
	store     *store.Store
	hasher    auth.Hasher
	guard     *loginGuard
	hashSlots chan struct{}
}

// New returns an HTTP server using the given password hasher.
func New(st *store.Store, hasher auth.Hasher) *Server {
	return &Server{store: st, hasher: hasher, guard: newLoginGuard(), hashSlots: make(chan struct{}, 2)}
}

// Handler is the full HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /api/v1/setup", s.setupState)
	mux.HandleFunc("POST /api/v1/setup", s.setup)
	mux.HandleFunc("GET /api/v1/join", s.joinState)
	mux.HandleFunc("POST /api/v1/join", s.join)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/session", s.session)
	mux.HandleFunc("POST /api/v1/auth/password", s.changePassword)
	mux.HandleFunc("GET /api/v1/settings", s.getSettings)
	mux.HandleFunc("PUT /api/v1/settings", s.putSettings)
	mux.HandleFunc("GET /api/v1/users", s.listUsers)
	mux.HandleFunc("POST /api/v1/users", s.createUser)
	mux.HandleFunc("POST /api/v1/users/{id}/password", s.resetUserPassword)
	mux.HandleFunc("POST /api/v1/users/{id}/enabled", s.setUserEnabled)
	mux.HandleFunc("DELETE /api/v1/users/{id}", s.deleteUser)
	mux.HandleFunc("GET /api/v1/warehouses", s.listWarehouses)
	mux.HandleFunc("POST /api/v1/warehouses", s.createWarehouse)
	mux.HandleFunc("GET /api/v1/warehouses/{id}", s.getWarehouse)
	mux.HandleFunc("DELETE /api/v1/warehouses/{id}", s.deleteWarehouse)
	mux.HandleFunc("GET /api/v1/warehouses/{id}/images", s.listImages)
	mux.HandleFunc("POST /api/v1/warehouses/{id}/images", s.uploadSession)
	mux.HandleFunc("GET /api/v1/warehouses/{id}/tokens", s.listTokens)
	mux.HandleFunc("POST /api/v1/warehouses/{id}/tokens", s.createToken)
	mux.HandleFunc("DELETE /api/v1/tokens/{id}", s.revokeToken)
	mux.HandleFunc("PATCH /api/v1/images/{id}", s.renameImage)
	mux.HandleFunc("GET /api/v1/images/{id}/raw", s.rawImage)
	mux.HandleFunc("GET /api/v1/images/{id}/preview", s.previewImage)
	mux.HandleFunc("POST /api/v1/images/move", s.moveImages)
	mux.HandleFunc("POST /api/v1/images/copy", s.copyImages)
	mux.HandleFunc("POST /api/v1/images/trash", s.trashImages)
	mux.HandleFunc("GET /api/v1/trash", s.listTrash)
	mux.HandleFunc("POST /api/v1/trash/restore", s.restoreImages)
	mux.HandleFunc("POST /api/v1/trash/move", s.moveTrash)
	mux.HandleFunc("POST /api/v1/trash/purge", s.purgeImages)
	mux.HandleFunc("POST /api/v1/trash/empty", s.emptyTrash)
	mux.HandleFunc("POST /api/v1/upload", s.uploadToken)
	mux.HandleFunc("GET /i/{publicID}", s.publicImage)
	mux.HandleFunc("/", s.pages)
	return mux
}

// PurgeLoop deletes trash older than 72 hours once a minute.
func (s *Server) PurgeLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	s.purgeOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeOnce()
		}
	}
}

func (s *Server) purgeOnce() {
	if _, err := s.store.PurgeExpired(time.Now()); err != nil {
		log.Printf("purge trash: %v", err)
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) pages(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/i/") {
		http.NotFound(w, r)
		return
	}
	webui.Serve(w, r)
}

func (s *Server) setupState(w http.ResponseWriter, r *http.Request) {
	ready, err := s.store.HasAdmin()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{
		"initialized": ready,
		"authorized":  !ready && s.setupAuthorized(r),
	})
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	ready, err := s.store.HasAdmin()
	if err != nil {
		writeError(w, err)
		return
	}
	if ready || !s.setupAuthorized(r) {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := readJSON(r, &body); err != nil || !auth.ValidUsername(body.Username) {
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
	if err := s.store.CreateAdmin(body.Username, phc); err != nil {
		writeError(w, err)
		return
	}
	_ = os.Remove(filepath.Join(s.store.DataDir(), "setup.secret"))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) joinState(w http.ResponseWriter, r *http.Request) {
	user, err := s.store.UserByInvite(r.Header.Get(joinHeader))
	if err != nil {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
}

func (s *Server) join(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	user, err := s.store.UserByInvite(r.Header.Get(joinHeader))
	if err != nil {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
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
	if err := s.store.ActivateUser(user.ID, phc); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setupAuthorized(r *http.Request) bool {
	want, err := os.ReadFile(filepath.Join(s.store.DataDir(), "setup.secret"))
	if err != nil || len(want) != 64 {
		return false
	}
	got := r.Header.Get(setupHeader)
	return len(got) == 64 && subtle.ConstantTimeCompare(want, []byte(got)) == 1
}

// EnsureSetup writes a 64-hex secret when the server has no admin yet.
func EnsureSetup(dataDir string, hasAdmin bool) (string, error) {
	path := filepath.Join(dataDir, "setup.secret")
	if hasAdmin {
		_ = os.Remove(path)
		return "", nil
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return "", errors.New("invalid setup credential file")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		if _, err := hex.DecodeString(string(b)); err != nil || len(b) != 64 {
			return "", errors.New("invalid setup credential")
		}
		return string(b), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if !auth.ValidUsername(body.Username) {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	user, err := s.store.UserByName(body.Username)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, err)
		return
	}
	// Separate real accounts on shared connections; unknown names share one bucket.
	key := clientIP(r) + "\x00"
	if err == nil {
		key += user.Username
	}
	if s.guard.blocked(key) {
		writeCode(w, http.StatusTooManyRequests, "locked")
		return
	}
	if err == nil && user.Status == "disabled" {
		if s.guard.fail(key) {
			writeCode(w, http.StatusTooManyRequests, "locked")
			return
		}
		writeCode(w, http.StatusForbidden, "disabled")
		return
	}
	ok := false
	if err == nil && user.Status == "active" {
		if !s.beginHash() {
			writeCode(w, http.StatusTooManyRequests, "locked")
			return
		}
		defer s.endHash()
		ok, err = s.hasher.Verify([]byte(body.Password), user.PasswordHash())
		if err != nil {
			writeError(w, err)
			return
		}
	}
	if !ok {
		if s.guard.fail(key) {
			writeCode(w, http.StatusTooManyRequests, "locked")
			return
		}
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.guard.success(key)
	token, exp, err := s.store.CreateSession(user.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, sessionCookie(r, token, exp))
	writeJSON(w, http.StatusOK, publicUser(user))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		_ = s.store.RevokeSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: publicScheme(r) == "https"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicUser(user))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	full, err := s.store.UserByName(user.Username)
	if err != nil {
		writeError(w, err)
		return
	}
	var body struct {
		Current  string `json:"current"`
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
	ok, err := s.hasher.Verify([]byte(body.Current), full.PasswordHash())
	if err != nil {
		writeError(w, err)
		return
	}
	if !ok {
		writeCode(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	phc, err := s.hasher.Hash([]byte(body.Password))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.SetPassword(user.ID, phc); err != nil {
		writeError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: publicScheme(r) == "https"})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSettings()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	if !originOK(r) {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	user, err := s.current(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if user.Role != "admin" {
		writeCode(w, http.StatusForbidden, "forbidden")
		return
	}
	var body store.Settings
	if err := readJSON(r, &body); err != nil {
		writeCode(w, http.StatusBadRequest, "invalid")
		return
	}
	if err := s.store.SetLocale(body.Locale); err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.SetTheme(body.Theme); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) current(r *http.Request) (store.User, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || cookie.Value == "" {
		return store.User{}, errUnauthorized
	}
	user, err := s.store.UserBySession(cookie.Value)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.User{}, errUnauthorized
		}
		return store.User{}, err
	}
	return user, nil
}

func publicUser(user store.User) map[string]any {
	return map[string]any{"id": user.ID, "username": user.Username, "role": user.Role}
}

func sessionCookie(r *http.Request, token string, exp time.Time) *http.Cookie {
	return &http.Cookie{
		Name: cookieName, Value: token, Path: "/", Expires: exp, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: publicScheme(r) == "https",
	}
}

type loginGuard struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	first        time.Time
	blockedUntil time.Time
	failures     int
}

func newLoginGuard() *loginGuard {
	return &loginGuard{attempts: map[string]loginAttempt{}}
}

func (g *loginGuard) blocked(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.sweep(now)
	item, ok := g.attempts[ip]
	return ok && now.Before(item.blockedUntil)
}

func (g *loginGuard) fail(ip string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	g.sweep(now)
	item := g.attempts[ip]
	if item.first.IsZero() || now.Sub(item.first) > loginWindow {
		item = loginAttempt{first: now}
	}
	item.failures++
	if item.failures >= loginFailures {
		item.blockedUntil = now.Add(loginBan)
	}
	g.attempts[ip] = item
	return !item.blockedUntil.IsZero() && now.Before(item.blockedUntil) && item.failures >= loginFailures
}

func (g *loginGuard) success(ip string) {
	g.mu.Lock()
	delete(g.attempts, ip)
	g.mu.Unlock()
}

func (g *loginGuard) sweep(now time.Time) {
	for ip, item := range g.attempts {
		if now.Before(item.blockedUntil) {
			continue
		}
		if item.first.IsZero() || now.Sub(item.first) > loginWindow {
			delete(g.attempts, ip)
		}
	}
}

func (s *Server) beginHash() bool {
	select {
	case s.hashSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) endHash() {
	<-s.hashSlots
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.String()
	}
	return host
}
