package store

import "testing"

func TestUploadLimitMigrationAndAtomicSettings(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSettings()
	if err != nil || got.MaxUploadBytes != 1<<30 {
		t.Fatalf("default: %+v %v", got, err)
	}
	if _, err := st.UpdateSettings("zh-CN", "dark", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`ALTER TABLE settings DROP COLUMN max_upload_bytes`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM schema_migrations WHERE version='0003_upload_limit.sql'`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err = st.GetSettings()
	if err != nil || got.MaxUploadBytes != 1<<30 || got.Locale != "zh-CN" || got.Theme != "dark" {
		t.Fatalf("migration: %+v %v", got, err)
	}
	limit := int64(2 << 20)
	if _, err := st.UpdateSettings("en", "light", &limit); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateSettings("zh-CN", "invalid", nil); err != ErrInvalid {
		t.Fatal("invalid theme")
	}
	got, _ = st.GetSettings()
	if got.Locale != "en" || got.Theme != "light" || got.MaxUploadBytes != limit {
		t.Fatal("partial settings update")
	}
	for _, value := range []int64{0, -1, 9007199254740992} {
		if _, err := st.UpdateSettings("zh-CN", "dark", &value); err != ErrInvalid {
			t.Fatalf("limit accepted: %d", value)
		}
	}
	got, err = st.UpdateSettings("zh-CN", "system", nil)
	if err != nil || got.MaxUploadBytes != limit {
		t.Fatal("omitted limit reset")
	}
}
