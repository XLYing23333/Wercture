package config

import (
	"path/filepath"
	"testing"
)

func TestParseImport(t *testing.T) {
	profile, err := ParseImport("wercture://import?host=http%3A%2F%2F127.0.0.1%3A9416&token=wer-2zDlSbz0woKzzhrd&name=main")
	if err != nil {
		t.Fatal(err)
	}
	if profile.Host != "http://127.0.0.1:9416" || profile.Token != "wer-2zDlSbz0woKzzhrd" || profile.Name != "main" {
		t.Fatalf("%+v", profile)
	}
	if _, err := ParseImport("https://example.com/import"); err == nil {
		t.Fatal("accepted a normal url")
	}
}

func TestImportUpdatesExistingProfile(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "wercture.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := ParseImport("wercture://import?host=http%3A%2F%2F127.0.0.1%3A9416&token=wer-aaaaaaaaaaaaaaaa&name=main")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Import(first); err != nil {
		t.Fatal(err)
	}
	if err := db.Edit("main", Profile{Mode: "webp", Quality: 60}, true); err != nil {
		t.Fatal(err)
	}
	next, err := ParseImport("wercture://import?host=https%3A%2F%2Fimg.example.com&token=wer-bbbbbbbbbbbbbbbb&name=main")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Import(next); err != nil {
		t.Fatal(err)
	}
	got, err := db.Selected()
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "https://img.example.com" || got.Token != "wer-bbbbbbbbbbbbbbbb" || got.Mode != "webp" || got.Quality != 60 {
		t.Fatalf("%+v", got)
	}
}
