CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_phc TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    created_at TEXT NOT NULL
);

CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BLOB NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    locale TEXT NOT NULL DEFAULT 'en',
    theme TEXT NOT NULL DEFAULT 'system'
);

INSERT INTO settings(id, locale, theme) VALUES (1, 'en', 'system');

CREATE TABLE warehouses (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    owner_id INTEGER NOT NULL REFERENCES users(id),
    created_at TEXT NOT NULL
);

CREATE TABLE tokens (
    id INTEGER PRIMARY KEY,
    warehouse_id INTEGER NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE,
    token_plain TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE images (
    id INTEGER PRIMARY KEY,
    public_id TEXT NOT NULL UNIQUE,
    original_name TEXT NOT NULL,
    mime TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    storage_path TEXT NOT NULL,
    warehouse_id INTEGER REFERENCES warehouses(id) ON DELETE SET NULL,
    uploader_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('active', 'trash')),
    uploaded_at TEXT NOT NULL,
    deleted_at TEXT
);

CREATE INDEX images_warehouse ON images(warehouse_id, status);
CREATE INDEX images_status_deleted ON images(status, deleted_at);
