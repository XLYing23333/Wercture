// Package config stores named upload profiles in ~/.config/wercture.db.
package config

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Profile is one saved server and processing choice.
type Profile struct {
	Name     string
	Host     string
	Token    string
	Mode     string
	Quality  int
	Selected bool
}

// ErrNotFound means the named profile is missing.
var ErrNotFound = errors.New("profile not found")

// ErrExists means the profile name is already stored.
var ErrExists = errors.New("profile exists")

// DB is the client configuration database.
type DB struct {
	sql *sql.DB
}

// Open creates ~/.config/wercture.db when path is empty.
func Open(path string) (*DB, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(home, ".config")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(dir, "wercture.db")
	} else if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"}).String()
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB}
	if _, err := sqlDB.Exec(`CREATE TABLE IF NOT EXISTS profiles (
		name TEXT PRIMARY KEY,
		host TEXT NOT NULL,
		token TEXT NOT NULL,
		mode TEXT NOT NULL DEFAULT 'none',
		quality INTEGER NOT NULL DEFAULT 80,
		selected INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	if err := migrateModes(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// migrateModes keeps an older single "webp" choice as lossy WebP, which is both switches on.
func migrateModes(sqlDB *sql.DB) error {
	var version int
	if err := sqlDB.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 1 {
		return nil
	}
	if _, err := sqlDB.Exec(`UPDATE profiles SET mode='both' WHERE mode='webp'`); err != nil {
		return err
	}
	_, err := sqlDB.Exec(`PRAGMA user_version = 1`)
	return err
}

// Close closes the database.
func (db *DB) Close() error { return db.sql.Close() }

// Add inserts a profile.
func (db *DB) Add(profile Profile) error {
	profile = normalize(profile)
	if profile.Name == "" || profile.Host == "" || profile.Token == "" {
		return errors.New("name, host, and token are required")
	}
	selected := 0
	var n int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM profiles`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		selected = 1
		profile.Selected = true
	}
	_, err := db.sql.Exec(`INSERT INTO profiles(name, host, token, mode, quality, selected) VALUES (?, ?, ?, ?, ?, ?)`,
		profile.Name, profile.Host, profile.Token, profile.Mode, profile.Quality, selected)
	if err != nil && isUnique(err) {
		return ErrExists
	}
	return err
}

// List returns every profile.
func (db *DB) List() ([]Profile, error) {
	rows, err := db.sql.Query(`SELECT name, host, token, mode, quality, selected FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		var profile Profile
		var selected int
		if err := rows.Scan(&profile.Name, &profile.Host, &profile.Token, &profile.Mode, &profile.Quality, &selected); err != nil {
			return nil, err
		}
		profile.Selected = selected == 1
		out = append(out, profile)
	}
	return out, rows.Err()
}

// Get loads one profile.
func (db *DB) Get(name string) (Profile, error) {
	var profile Profile
	var selected int
	err := db.sql.QueryRow(`SELECT name, host, token, mode, quality, selected FROM profiles WHERE name=?`, name).
		Scan(&profile.Name, &profile.Host, &profile.Token, &profile.Mode, &profile.Quality, &selected)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	profile.Selected = selected == 1
	return profile, err
}

// Selected returns the active profile.
func (db *DB) Selected() (Profile, error) {
	var profile Profile
	err := db.sql.QueryRow(`SELECT name, host, token, mode, quality, selected FROM profiles WHERE selected=1 LIMIT 1`).
		Scan(&profile.Name, &profile.Host, &profile.Token, &profile.Mode, &profile.Quality, new(int))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	profile.Selected = true
	return profile, err
}

// Edit updates the fields that are non-zero in patch. Mode is updated when setMode is true.
func (db *DB) Edit(name string, patch Profile, setMode bool) error {
	current, err := db.Get(name)
	if err != nil {
		return err
	}
	if patch.Host != "" {
		current.Host = patch.Host
	}
	if patch.Token != "" {
		current.Token = patch.Token
	}
	if setMode {
		current.Mode = normalize(patch).Mode
	}
	if patch.Quality != 0 {
		current.Quality = normalize(Profile{Quality: patch.Quality}).Quality
	}
	_, err = db.sql.Exec(`UPDATE profiles SET host=?, token=?, mode=?, quality=? WHERE name=?`, current.Host, current.Token, current.Mode, current.Quality, name)
	return err
}

// Remove deletes a profile and keeps one selected when possible.
func (db *DB) Remove(name string) error {
	current, err := db.Get(name)
	if err != nil {
		return err
	}
	if _, err := db.sql.Exec(`DELETE FROM profiles WHERE name=?`, name); err != nil {
		return err
	}
	if current.Selected {
		_, err = db.sql.Exec(`UPDATE profiles SET selected=1 WHERE name=(SELECT name FROM profiles ORDER BY name LIMIT 1)`)
	}
	return err
}

// ParseImport reads a wercture://import link into a profile.
func ParseImport(raw string) (Profile, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "wercture" || parsed.Host != "import" {
		return Profile{}, errors.New("import url is invalid")
	}
	host := strings.TrimRight(parsed.Query().Get("host"), "/")
	token := parsed.Query().Get("token")
	name := strings.TrimSpace(parsed.Query().Get("name"))
	origin, err := url.Parse(host)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || token == "" {
		return Profile{}, errors.New("import url is invalid")
	}
	if name == "" {
		name = "wercture"
	}
	return Profile{Name: name, Host: host, Token: token, Mode: "none", Quality: 80}, nil
}

// Import saves the profile from a link and selects it. An existing name keeps its processing options.
func (db *DB) Import(profile Profile) error {
	_, err := db.Get(profile.Name)
	if errors.Is(err, ErrNotFound) {
		if err := db.Add(profile); err != nil {
			return err
		}
		return db.Use(profile.Name)
	}
	if err != nil {
		return err
	}
	if err := db.Edit(profile.Name, Profile{Host: profile.Host, Token: profile.Token}, false); err != nil {
		return err
	}
	return db.Use(profile.Name)
}

// Use marks one profile as the default.
func (db *DB) Use(name string) error {
	if _, err := db.Get(name); err != nil {
		return err
	}
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE profiles SET selected=0`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`UPDATE profiles SET selected=1 WHERE name=?`, name); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func normalize(profile Profile) Profile {
	switch profile.Mode {
	case "compress", "webp", "both":
	default:
		profile.Mode = "none"
	}
	if profile.Quality < 1 || profile.Quality > 100 {
		profile.Quality = 80
	}
	return profile
}

func isUnique(err error) bool {
	return err != nil && (contains(err.Error(), "UNIQUE") || contains(err.Error(), "unique"))
}

func contains(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || (len(s) > 0 && (stringIndex(s, part) >= 0)))
}

func stringIndex(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
