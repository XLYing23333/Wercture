// Package store persists accounts, warehouses, and images in SQLite.
package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"wercture/server/internal/auth"
	"wercture/server/migrations"
)

const (
	keepTrash   = 72 * time.Hour
	sessionTTL  = 7 * 24 * time.Hour
	keyAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

func newKey() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i, b := range raw {
		raw[i] = keyAlphabet[int(b)%len(keyAlphabet)]
	}
	return "wer-" + string(raw), nil
}

var (
	// ErrNotFound means the requested row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrExists means a unique name or token is already stored.
	ErrExists = errors.New("exists")
	// ErrForbidden means the account cannot touch this row.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalid means the input is outside the accepted form.
	ErrInvalid = errors.New("invalid")
	// ErrLastAdmin means the only admin account would be removed.
	ErrLastAdmin = errors.New("last admin")
	// ErrWarehouseGone means a trashed image no longer has its original warehouse.
	ErrWarehouseGone = errors.New("warehouse gone")
	// ErrNoAdmin means password reset found no admin account.
	ErrNoAdmin = errors.New("no admin")
)

// User is a login account.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status,omitempty"`
	Invite    string `json:"invite,omitempty"`
	CreatedAt string `json:"created_at"`
	phc       string
}

// Settings is the single server-wide appearance row.
type Settings struct {
	Locale string `json:"locale"`
	Theme  string `json:"theme"`
}

// Warehouse is one image collection owned by a user.
type Warehouse struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	OwnerID    int64    `json:"owner_id"`
	Owner      string   `json:"owner"`
	CreatedAt  string   `json:"created_at"`
	ImageCount int64    `json:"image_count"`
	Previews   []string `json:"previews"`
}

// Token is an upload credential for one warehouse.
type Token struct {
	ID          int64  `json:"id"`
	WarehouseID int64  `json:"warehouse_id"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	CreatedAt   string `json:"created_at"`
}

// Image is one stored picture.
type Image struct {
	ID            int64  `json:"id"`
	PublicID      string `json:"public_id"`
	Name          string `json:"name"`
	MIME          string `json:"mime"`
	Size          int64  `json:"size"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	WarehouseID   *int64 `json:"warehouse_id"`
	WarehouseName string `json:"warehouse_name,omitempty"`
	CanRestore    bool   `json:"can_restore"`
	UploadedAt    string `json:"uploaded_at"`
	DeletedAt     string `json:"deleted_at,omitempty"`
	Path          string `json:"path"`
	storagePath   string
}

// NewImage is the metadata written for a file already stored on disk.
type NewImage struct {
	PublicID    string
	Name        string
	MIME        string
	Size        int64
	Width       int
	Height      int
	StoragePath string
	WarehouseID int64
	UploaderID  int64
}

// Store is the process-wide database.
type Store struct {
	db      *sql.DB
	mu      sync.Mutex
	dataDir string
}

// Open creates the data directory, applies migrations, and opens SQLite.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dataDir, "files"), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dataDir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, "wercture.db")
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, dataDir: dataDir}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DataDir returns the data directory.
func (s *Store) DataDir() string { return s.dataDir }

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, name, now()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (s *Store) write(fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// HasAdmin reports whether the first account exists.
func (s *Store) HasAdmin() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&n)
	return n > 0, err
}

// CreateAdmin inserts the first admin. A second call returns ErrExists.
func (s *Store) CreateAdmin(username, phc string) error {
	if !auth.ValidUsername(username) {
		return ErrInvalid
	}
	return s.write(func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrExists
		}
		_, err := tx.Exec(`INSERT INTO users(username, password_phc, role, created_at) VALUES (?, ?, 'admin', ?)`, username, phc, now())
		if isUnique(err) {
			return ErrExists
		}
		return err
	})
}

// CreateUser inserts a pending account and returns its one-time activation token.
func (s *Store) CreateUser(username string) (User, error) {
	if !auth.ValidUsername(username) {
		return User{}, ErrInvalid
	}
	invite, err := newInvite()
	if err != nil {
		return User{}, err
	}
	var user User
	err = s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO users(username, password_phc, role, created_at, status, invite_plain) VALUES (?, '', 'user', ?, 'pending', ?)`, username, now(), invite)
		if isUnique(err) {
			return ErrExists
		}
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		user = User{ID: id, Username: username, Role: "user", Status: "pending", Invite: invite}
		return nil
	})
	return user, err
}

func newInvite() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// UserByInvite loads a pending account for an activation token.
func (s *Store) UserByInvite(token string) (User, error) {
	if len(token) != 64 {
		return User{}, ErrNotFound
	}
	var user User
	err := s.db.QueryRow(`SELECT id, username, role, created_at, status FROM users WHERE invite_plain=? AND status='pending'`, token).
		Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &user.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

// ActivateUser stores the first password and consumes the activation token.
func (s *Store) ActivateUser(id int64, phc string) error {
	return s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE users SET password_phc=?, status='active', invite_plain='' WHERE id=? AND status='pending'`, phc, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// UserByName loads an account, including the password hash for login.
func (s *Store) UserByName(username string) (User, error) {
	var user User
	err := s.db.QueryRow(`SELECT id, username, role, created_at, status, password_phc FROM users WHERE username=?`, username).
		Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &user.Status, &user.phc)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

// PasswordHash returns the stored PHC for a loaded user.
func (u User) PasswordHash() string { return u.phc }

// UserByID loads an account without the password hash.
func (s *Store) UserByID(id int64) (User, error) {
	var user User
	err := s.db.QueryRow(`SELECT id, username, role, created_at FROM users WHERE id=?`, id).Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return user, err
}

// ListUsers returns every account.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at, status, invite_plain FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &user.Status, &user.Invite); err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

// SetPassword replaces the hash and revokes that account's sessions.
func (s *Store) SetPassword(id int64, phc string) error {
	return s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE users SET password_phc=?, status='active', invite_plain='' WHERE id=?`, phc, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(`UPDATE sessions SET revoked=1 WHERE user_id=?`, id)
		return err
	})
}

// ResetAdminPassword replaces the earliest admin password.
func (s *Store) ResetAdminPassword(phc string) error {
	return s.write(func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRow(`SELECT id FROM users WHERE role='admin' ORDER BY id LIMIT 1`).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoAdmin
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE users SET password_phc=? WHERE id=?`, phc, id); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE sessions SET revoked=1 WHERE user_id=?`, id)
		return err
	})
}

// SetUserEnabled suspends or restores an account. A suspended pending account stays pending when restored.
func (s *Store) SetUserEnabled(actor User, id int64, enabled bool) error {
	if actor.Role != "admin" {
		return ErrForbidden
	}
	if actor.ID == id {
		return ErrInvalid
	}
	return s.write(func(tx *sql.Tx) error {
		var role, status, invite string
		err := tx.QueryRow(`SELECT role, status, invite_plain FROM users WHERE id=?`, id).Scan(&role, &status, &invite)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !enabled && role == "admin" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin' AND status!='disabled'`).Scan(&n); err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
		next := "disabled"
		if enabled {
			if invite != "" {
				next = "pending"
			} else {
				next = "active"
			}
		}
		if _, err := tx.Exec(`UPDATE users SET status=? WHERE id=?`, next, id); err != nil {
			return err
		}
		if !enabled {
			_, err = tx.Exec(`UPDATE sessions SET revoked=1 WHERE user_id=?`, id)
		}
		return err
	})
}

// DeleteUser removes an account. mode "purge" deletes picture files. mode "trash" keeps them without a warehouse.
func (s *Store) DeleteUser(actor User, id int64, mode string) ([]string, error) {
	if actor.Role != "admin" {
		return nil, ErrForbidden
	}
	if actor.ID == id {
		return nil, ErrInvalid
	}
	if mode != "purge" && mode != "trash" {
		return nil, ErrInvalid
	}
	var paths []string
	err := s.write(func(tx *sql.Tx) error {
		var role string
		err := tx.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if role == "admin" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role='admin'`).Scan(&n); err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
		if mode == "purge" {
			rows, err := tx.Query(`SELECT storage_path FROM images WHERE warehouse_id IN (SELECT id FROM warehouses WHERE owner_id=?)`, id)
			if err != nil {
				return err
			}
			for rows.Next() {
				var path string
				if err := rows.Scan(&path); err != nil {
					rows.Close()
					return err
				}
				paths = append(paths, path)
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			if _, err := tx.Exec(`DELETE FROM images WHERE warehouse_id IN (SELECT id FROM warehouses WHERE owner_id=?)`, id); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(`UPDATE images SET status='trash', deleted_at=?, warehouse_id=NULL WHERE warehouse_id IN (SELECT id FROM warehouses WHERE owner_id=?)`, now(), id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`DELETE FROM tokens WHERE warehouse_id IN (SELECT id FROM warehouses WHERE owner_id=?)`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM warehouses WHERE owner_id=?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
			return err
		}
		_, err = tx.Exec(`DELETE FROM users WHERE id=?`, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	removeFiles(s.dataDir, paths)
	return paths, nil
}

// CreateSession stores a new login session and returns the raw cookie value.
func (s *Store) CreateSession(userID int64) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	exp := time.Now().UTC().Add(sessionTTL)
	err := s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO sessions(user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?)`, userID, sum[:], exp.Format(time.RFC3339Nano), now())
		return err
	})
	return token, exp, err
}

// UserBySession loads the account for a raw cookie value.
func (s *Store) UserBySession(token string) (User, error) {
	sum := sha256.Sum256([]byte(token))
	var user User
	var exp string
	err := s.db.QueryRow(`SELECT u.id, u.username, u.role, u.created_at, u.status, s.expires_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=? AND s.revoked=0`, sum[:]).Scan(&user.ID, &user.Username, &user.Role, &user.CreatedAt, &user.Status, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	until, err := time.Parse(time.RFC3339Nano, exp)
	if err != nil || time.Now().After(until) || user.Status != "active" {
		return User{}, ErrNotFound
	}
	return user, nil
}

// RevokeSession marks a cookie revoked.
func (s *Store) RevokeSession(token string) error {
	sum := sha256.Sum256([]byte(token))
	_, err := s.db.Exec(`UPDATE sessions SET revoked=1 WHERE token_hash=?`, sum[:])
	return err
}

// GetSettings reads the appearance row.
func (s *Store) GetSettings() (Settings, error) {
	var out Settings
	err := s.db.QueryRow(`SELECT locale, theme FROM settings WHERE id=1`).Scan(&out.Locale, &out.Theme)
	return out, err
}

// SetLocale updates the shared language.
func (s *Store) SetLocale(locale string) error {
	if locale != "en" && locale != "zh-CN" {
		return ErrInvalid
	}
	_, err := s.db.Exec(`UPDATE settings SET locale=? WHERE id=1`, locale)
	return err
}

// SetTheme updates the shared theme.
func (s *Store) SetTheme(theme string) error {
	if theme != "system" && theme != "light" && theme != "dark" {
		return ErrInvalid
	}
	_, err := s.db.Exec(`UPDATE settings SET theme=? WHERE id=1`, theme)
	return err
}

// ListWarehouses returns warehouses visible to the account.
func (s *Store) ListWarehouses(user User) ([]Warehouse, error) {
	query := `SELECT w.id, w.name, w.owner_id, u.username, w.created_at,
		(SELECT COUNT(*) FROM images i WHERE i.warehouse_id=w.id AND i.status='active')
		FROM warehouses w JOIN users u ON u.id=w.owner_id`
	args := []any{}
	if user.Role != "admin" {
		query += ` WHERE w.owner_id=?`
		args = append(args, user.ID)
	}
	query += ` ORDER BY w.id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var out []Warehouse
	for rows.Next() {
		var warehouse Warehouse
		if err := rows.Scan(&warehouse.ID, &warehouse.Name, &warehouse.OwnerID, &warehouse.Owner, &warehouse.CreatedAt, &warehouse.ImageCount); err != nil {
			rows.Close()
			return nil, err
		}
		warehouse.Previews = []string{}
		out = append(out, warehouse)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		previews, err := s.db.Query(`SELECT public_id, mime FROM images WHERE warehouse_id=? AND status='active' ORDER BY uploaded_at DESC, id DESC LIMIT 3`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for previews.Next() {
			var publicID, mime string
			if err := previews.Scan(&publicID, &mime); err != nil {
				previews.Close()
				return nil, err
			}
			out[i].Previews = append(out[i].Previews, CardPath(publicID, mime))
		}
		if err := previews.Err(); err != nil {
			previews.Close()
			return nil, err
		}
		previews.Close()
	}
	return out, nil
}

// CreateWarehouse inserts a warehouse owned by the caller.
func (s *Store) CreateWarehouse(user User, name string) (Warehouse, error) {
	name = strings.TrimSpace(name)
	if !validTitle(name, 64) {
		return Warehouse{}, ErrInvalid
	}
	var warehouse Warehouse
	err := s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO warehouses(name, owner_id, created_at) VALUES (?, ?, ?)`, name, user.ID, now())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		return scanWarehouse(tx.QueryRow(`SELECT w.id, w.name, w.owner_id, u.username, w.created_at, 0
			FROM warehouses w JOIN users u ON u.id=w.owner_id WHERE w.id=?`, id), &warehouse)
	})
	return warehouse, err
}

// Warehouse loads one warehouse when the account may see it.
func (s *Store) Warehouse(user User, id int64) (Warehouse, error) {
	var warehouse Warehouse
	err := scanWarehouse(s.db.QueryRow(`SELECT w.id, w.name, w.owner_id, u.username, w.created_at,
		(SELECT COUNT(*) FROM images i WHERE i.warehouse_id=w.id AND i.status='active')
		FROM warehouses w JOIN users u ON u.id=w.owner_id WHERE w.id=?`, id), &warehouse)
	if err != nil {
		return Warehouse{}, err
	}
	if user.Role != "admin" && warehouse.OwnerID != user.ID {
		return Warehouse{}, ErrForbidden
	}
	return warehouse, nil
}

func scanWarehouse(row interface{ Scan(...any) error }, warehouse *Warehouse) error {
	err := row.Scan(&warehouse.ID, &warehouse.Name, &warehouse.OwnerID, &warehouse.Owner, &warehouse.CreatedAt, &warehouse.ImageCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// DeleteWarehouse moves its pictures to the trash and removes the warehouse.
func (s *Store) DeleteWarehouse(user User, id int64) error {
	if _, err := s.Warehouse(user, id); err != nil {
		return err
	}
	return s.write(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE images SET status='trash', deleted_at=?, warehouse_id=NULL WHERE warehouse_id=?`, now(), id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tokens WHERE warehouse_id=?`, id); err != nil {
			return err
		}
		res, err := tx.Exec(`DELETE FROM warehouses WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// CreateToken issues an upload token.
func (s *Store) CreateToken(user User, warehouseID int64, name string) (Token, error) {
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return Token{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "upload"
	}
	if !validTitle(name, 64) {
		return Token{}, ErrInvalid
	}
	plain, err := newKey()
	if err != nil {
		return Token{}, err
	}
	sum := sha256.Sum256([]byte(plain))
	var token Token
	err = s.write(func(tx *sql.Tx) error {
		res, err := tx.Exec(`INSERT INTO tokens(warehouse_id, name, token_hash, token_plain, created_at) VALUES (?, ?, ?, ?, ?)`, warehouseID, name, sum[:], plain, now())
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		token = Token{ID: id, WarehouseID: warehouseID, Name: name, Token: plain, CreatedAt: now()}
		return tx.QueryRow(`SELECT created_at FROM tokens WHERE id=?`, id).Scan(&token.CreatedAt)
	})
	return token, err
}

// ListTokens returns live upload tokens for a warehouse.
func (s *Store) ListTokens(user User, warehouseID int64) ([]Token, error) {
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id, warehouse_id, name, token_plain, created_at FROM tokens WHERE warehouse_id=? ORDER BY id`, warehouseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.WarehouseID, &token.Name, &token.Token, &token.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

// RevokeToken deletes an upload token.
func (s *Store) RevokeToken(user User, id int64) error {
	var warehouseID int64
	err := s.db.QueryRow(`SELECT warehouse_id FROM tokens WHERE id=?`, id).Scan(&warehouseID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM tokens WHERE id=?`, id)
	return err
}

// WarehouseByToken resolves a live upload token to its warehouse and owner.
func (s *Store) WarehouseByToken(plain string) (warehouseID, ownerID int64, err error) {
	sum := sha256.Sum256([]byte(plain))
	err = s.db.QueryRow(`SELECT t.warehouse_id, w.owner_id FROM tokens t JOIN warehouses w ON w.id=t.warehouse_id JOIN users u ON u.id=w.owner_id WHERE t.token_hash=? AND u.status='active'`, sum[:]).Scan(&warehouseID, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	return warehouseID, ownerID, err
}

// InsertImage records a picture. The file is already on disk.
func (s *Store) InsertImage(image NewImage) (Image, error) {
	images, err := s.InsertImages([]NewImage{image})
	if err != nil {
		return Image{}, err
	}
	return images[0], nil
}

// InsertImages commits all file records together. Callers remove staged files on error.
func (s *Store) InsertImages(images []NewImage) ([]Image, error) {
	if len(images) == 0 || len(images) > 100 {
		return nil, ErrInvalid
	}
	out := make([]Image, 0, len(images))
	err := s.write(func(tx *sql.Tx) error {
		for _, image := range images {
			res, err := tx.Exec(`INSERT INTO images(public_id, original_name, mime, size_bytes, width, height, storage_path, warehouse_id, uploader_id, status, uploaded_at)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', ?)`,
				image.PublicID, image.Name, image.MIME, image.Size, image.Width, image.Height, image.StoragePath, image.WarehouseID, image.UploaderID, now())
			if isUnique(err) {
				return ErrExists
			}
			if err != nil {
				return err
			}
			id, err := res.LastInsertId()
			if err != nil {
				return err
			}
			var saved Image
			if err := scanImage(tx.QueryRow(imageSelect+` WHERE i.id=?`, id), &saved); err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const imageSelect = `SELECT i.id, i.public_id, i.original_name, i.mime, i.size_bytes, i.width, i.height, i.warehouse_id, COALESCE(w.name, ''),
	i.uploaded_at, COALESCE(i.deleted_at, ''), i.storage_path, i.status
	FROM images i LEFT JOIN warehouses w ON w.id=i.warehouse_id`

func scanImage(row interface{ Scan(...any) error }, image *Image) error {
	var status string
	var warehouseID sql.NullInt64
	err := row.Scan(&image.ID, &image.PublicID, &image.Name, &image.MIME, &image.Size, &image.Width, &image.Height, &warehouseID, &image.WarehouseName,
		&image.UploadedAt, &image.DeletedAt, &image.storagePath, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if warehouseID.Valid {
		id := warehouseID.Int64
		image.WarehouseID = &id
		image.CanRestore = status == "trash"
	}
	image.Path = "/i/" + image.PublicID + PublicExt(image.MIME)
	return nil
}

// CardPath is the picture shown on a warehouse card.
func CardPath(publicID, mime string) string {
	if mime == "application/pdf" || mime == "text/markdown" {
		return "/i/" + publicID + ".preview.png"
	}
	return "/i/" + publicID + PublicExt(mime)
}

// PublicExt is the direct-link suffix for a stored image type.
func PublicExt(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/markdown":
		return ".md"
	default:
		return ""
	}
}

// ListImages returns one page of active pictures, newest first.
// beforeID is the last id of the previous page. next is 0 when the page is the last.
func (s *Store) ListImages(user User, warehouseID, beforeID int64, limit int) ([]Image, int64, error) {
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 100 {
		limit = 40
	}
	query := imageSelect + ` WHERE i.warehouse_id=? AND i.status='active'`
	args := []any{warehouseID}
	if beforeID > 0 {
		query += ` AND (i.uploaded_at, i.id) < (SELECT uploaded_at, id FROM images WHERE id=?)`
		args = append(args, beforeID)
	}
	query += ` ORDER BY i.uploaded_at DESC, i.id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectImages(rows)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	return items, next, nil
}

// ImageForUser loads one image the account may manage. Trash is admin-only.
func (s *Store) ImageForUser(user User, id int64) (Image, error) {
	var image Image
	if err := scanImage(s.db.QueryRow(imageSelect+` WHERE i.id=?`, id), &image); err != nil {
		return Image{}, err
	}
	if image.DeletedAt != "" && image.WarehouseID == nil {
		if user.Role != "admin" {
			return Image{}, ErrForbidden
		}
		return image, nil
	}
	if image.WarehouseID == nil {
		if user.Role != "admin" {
			return Image{}, ErrForbidden
		}
		return image, nil
	}
	if _, err := s.Warehouse(user, *image.WarehouseID); err != nil {
		return Image{}, err
	}
	if image.DeletedAt != "" && user.Role != "admin" {
		return Image{}, ErrForbidden
	}
	return image, nil
}

// PublicFile returns the on-disk path for an active picture.
func (s *Store) PublicFile(publicID string) (Image, string, error) {
	var image Image
	err := scanImage(s.db.QueryRow(imageSelect+` WHERE i.public_id=? AND i.status='active'`, publicID), &image)
	if err != nil {
		return Image{}, "", err
	}
	return image, filepath.Join(s.dataDir, image.storagePath), nil
}

// FilePath returns the absolute path of a managed image.
func (s *Store) FilePath(image Image) string {
	return filepath.Join(s.dataDir, image.storagePath)
}

// RenameImage changes the display name and keeps the public id.
func (s *Store) RenameImage(user User, id int64, name string) (Image, error) {
	name = strings.TrimSpace(name)
	if !validTitle(name, 255) {
		return Image{}, ErrInvalid
	}
	image, err := s.ImageForUser(user, id)
	if err != nil {
		return Image{}, err
	}
	if image.DeletedAt != "" {
		return Image{}, ErrInvalid
	}
	if err := s.write(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE images SET original_name=? WHERE id=? AND status='active'`, name, id)
		return err
	}); err != nil {
		return Image{}, err
	}
	image.Name = name
	return image, nil
}

// MoveImages moves active pictures to another warehouse without changing public ids.
func (s *Store) MoveImages(user User, ids []int64, warehouseID int64) error {
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalid
	}
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return err
	}
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			var ownerID int64
			var status string
			err := tx.QueryRow(`SELECT w.owner_id, i.status FROM images i JOIN warehouses w ON w.id=i.warehouse_id WHERE i.id=?`, id).Scan(&ownerID, &status)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if status != "active" {
				return ErrInvalid
			}
			if user.Role != "admin" && ownerID != user.ID {
				return ErrForbidden
			}
			if _, err := tx.Exec(`UPDATE images SET warehouse_id=? WHERE id=?`, warehouseID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// TrashImages marks pictures deleted. Files stay until purge.
func (s *Store) TrashImages(user User, ids []int64) error {
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalid
	}
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			var ownerID sql.NullInt64
			var status string
			err := tx.QueryRow(`SELECT w.owner_id, i.status FROM images i LEFT JOIN warehouses w ON w.id=i.warehouse_id WHERE i.id=?`, id).Scan(&ownerID, &status)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if status != "active" || !ownerID.Valid {
				return ErrInvalid
			}
			if user.Role != "admin" && ownerID.Int64 != user.ID {
				return ErrForbidden
			}
			if _, err := tx.Exec(`UPDATE images SET status='trash', deleted_at=? WHERE id=?`, now(), id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListTrash returns every picture in the recycle bin.
func (s *Store) ListTrash(user User) ([]Image, error) {
	if user.Role != "admin" {
		return nil, ErrForbidden
	}
	rows, err := s.db.Query(imageSelect + ` WHERE i.status='trash' ORDER BY i.deleted_at DESC, i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectImages(rows)
}

// RestoreImages returns pictures to their original warehouse when it still exists.
func (s *Store) RestoreImages(user User, ids []int64) error {
	if user.Role != "admin" {
		return ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalid
	}
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			var warehouseID sql.NullInt64
			var status string
			err := tx.QueryRow(`SELECT warehouse_id, status FROM images WHERE id=?`, id).Scan(&warehouseID, &status)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if status != "trash" || !warehouseID.Valid {
				return ErrWarehouseGone
			}
			if _, err := tx.Exec(`UPDATE images SET status='active', deleted_at=NULL WHERE id=?`, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// MoveTrashImages activates pictures in a chosen warehouse.
func (s *Store) MoveTrashImages(user User, ids []int64, warehouseID int64) error {
	if user.Role != "admin" {
		return ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalid
	}
	if _, err := s.Warehouse(user, warehouseID); err != nil {
		return err
	}
	return s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			res, err := tx.Exec(`UPDATE images SET status='active', deleted_at=NULL, warehouse_id=? WHERE id=? AND status='trash'`, warehouseID, id)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrNotFound
			}
		}
		return nil
	})
}

// PurgeImages deletes picture rows and returns relative paths to remove.
func (s *Store) PurgeImages(user User, ids []int64) error {
	if user.Role != "admin" {
		return ErrForbidden
	}
	if len(ids) == 0 || len(ids) > 100 {
		return ErrInvalid
	}
	var paths []string
	err := s.write(func(tx *sql.Tx) error {
		for _, id := range ids {
			var path, status string
			err := tx.QueryRow(`SELECT storage_path, status FROM images WHERE id=?`, id).Scan(&path, &status)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if status != "trash" {
				return ErrInvalid
			}
			if _, err := tx.Exec(`DELETE FROM images WHERE id=?`, id); err != nil {
				return err
			}
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	removeFiles(s.dataDir, paths)
	return nil
}

// EmptyTrash deletes every picture in the trash and returns how many were removed.
func (s *Store) EmptyTrash(user User) (int, error) {
	if user.Role != "admin" {
		return 0, ErrForbidden
	}
	var paths []string
	err := s.write(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT storage_path FROM images WHERE status='trash'`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return err
			}
			paths = append(paths, path)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		_, err = tx.Exec(`DELETE FROM images WHERE status='trash'`)
		return err
	})
	if err != nil {
		return 0, err
	}
	removeFiles(s.dataDir, paths)
	return len(paths), nil
}

// PurgeExpired removes trash older than 72 hours and returns how many rows were removed.
func (s *Store) PurgeExpired(at time.Time) (int, error) {
	cutoff := at.UTC().Add(-keepTrash).Format(time.RFC3339Nano)
	var paths []string
	err := s.write(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT storage_path FROM images WHERE status='trash' AND deleted_at IS NOT NULL AND deleted_at<=?`, cutoff)
		if err != nil {
			return err
		}
		for rows.Next() {
			var path string
			if err := rows.Scan(&path); err != nil {
				rows.Close()
				return err
			}
			paths = append(paths, path)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		_, err = tx.Exec(`DELETE FROM images WHERE status='trash' AND deleted_at IS NOT NULL AND deleted_at<=?`, cutoff)
		return err
	})
	if err != nil {
		return 0, err
	}
	removeFiles(s.dataDir, paths)
	return len(paths), nil
}

func collectImages(rows *sql.Rows) ([]Image, error) {
	var out []Image
	for rows.Next() {
		var image Image
		if err := scanImage(rows, &image); err != nil {
			return nil, err
		}
		out = append(out, image)
	}
	return out, rows.Err()
}

func removeFiles(dataDir string, paths []string) {
	root := filepath.Clean(filepath.Join(dataDir, "files"))
	for _, rel := range paths {
		abs := filepath.Clean(filepath.Join(dataDir, rel))
		if abs != root && strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			_ = os.Remove(abs)
			_ = os.Remove(abs + ".preview.png")
		}
	}
}

func validTitle(name string, maxRunes int) bool {
	if name == "" {
		return false
	}
	n := 0
	for _, r := range name {
		n++
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			return false
		}
	}
	return n <= maxRunes
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}
