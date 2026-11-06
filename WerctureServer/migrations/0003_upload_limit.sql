ALTER TABLE settings ADD COLUMN max_upload_bytes INTEGER NOT NULL DEFAULT 1073741824 CHECK (max_upload_bytes > 0);
