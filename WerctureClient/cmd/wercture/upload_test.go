package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"wercture/client/internal/config"
	"wercture/client/internal/image"
)

func TestExplicitUploadTypeFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--audio", "--video", "a.mp3"}, {"--audio", "--md", "a.mp3"}, {"--video", "--pdf", "a.mp4"},
		{"--audio", "--compress", "a.mp3"}, {"--video", "--webp", "a.mp4"}, {"--video", "--none", "a.mp4"},
		{"--audio", "--quality", "80", "a.mp3"}, {"--audio", "--compress=false", "a.mp3"},
	} {
		if err := runUpload(args); err == nil || strings.Contains(err.Error(), "profile") {
			t.Fatalf("invalid flags reached config: %v %v", args, err)
		}
	}
}

func TestFileTypeValidationAndRawMedia(t *testing.T) {
	dir := t.TempDir()
	media := []byte("RIFF\x10\x00\x00\x00WAVEfmt \x00\x00\x00\x00")
	file := filepath.Join(dir, "song.wav")
	if err := os.WriteFile(file, media, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"image", "video", "pdf", "markdown"} {
		if _, err := openUpload(file, kind, image.ModeBoth, 80); err == nil {
			t.Fatalf("accepted as %s", kind)
		}
	}
	source, err := openUpload(file, "audio", image.ModeBoth, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if source.MIME != "audio/wav" {
		t.Fatal(source.MIME)
	}
	raw, _ := io.ReadAll(source.Reader)
	if !bytes.Equal(raw, media) {
		t.Fatal("media modified by image mode")
	}
	if err := os.WriteFile(filepath.Join(dir, "fake.mp3"), []byte("not music"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := openUpload(filepath.Join(dir, "fake.mp3"), "audio", image.ModeNone, 80); err == nil {
		t.Fatal("fake audio accepted")
	}
}

func TestLargeOriginalStreamsMultipart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "large.jpg")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte{0xff, 0xd8, 0xff, 0xe0})
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	source, err := openUpload(file, "image", image.ModeNone, 80)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, ok := source.Reader.(*os.File); !ok {
		t.Fatal("original was buffered")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upload-secret" {
			t.Error("missing bearer")
		}
		multipart, err := r.MultipartReader()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		part, err := multipart.NextPart()
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		size, err := io.Copy(io.Discard, part)
		if err != nil || size != 64<<20 || part.FileName() != "large.jpg" {
			t.Errorf("multipart: %d %v", size, err)
		}
		fmt.Fprint(w, `{"url":"http://example.test/i/abc.jpg"}`)
	}))
	defer server.Close()
	link, err := postSource(server.Client(), server.URL, "upload-secret", source)
	if err != nil || link != "http://example.test/i/abc.jpg" {
		t.Fatalf("post: %s %v", link, err)
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("allocated %d bytes for 64 MiB original", allocated)
	}
}

type uploadTerminal struct{ keys []string }

func (term *uploadTerminal) Key() (string, error) {
	if len(term.keys) == 0 {
		return "", io.EOF
	}
	key := term.keys[0]
	term.keys = term.keys[1:]
	return key, nil
}
func (*uploadTerminal) Size() (int, int) { return 80, 24 }
func (*uploadTerminal) Write(string)     {}
func (*uploadTerminal) Close() error     { return nil }

func TestTUIUploadTypeAndProfileProcessing(t *testing.T) {
	for _, tc := range []struct {
		keys []string
		kind string
		ok   bool
	}{
		{[]string{"enter"}, "image", true}, {[]string{"down", "enter"}, "video", true},
		{[]string{"down", "down", "enter"}, "audio", true}, {[]string{"esc"}, "", false},
	} {
		kind, ok := selectUploadType(&uploadTerminal{keys: tc.keys}, texts("zh-CN"))
		if kind != tc.kind || ok != tc.ok {
			t.Fatalf("selection %s %v", kind, ok)
		}
	}
	data := []byte("RIFF\x10\x00\x00\x00WAVEfmt \x00\x00\x00\x00")
	file := filepath.Join(t.TempDir(), "song.wav")
	os.WriteFile(file, data, 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		multipart, _ := r.MultipartReader()
		part, _ := multipart.NextPart()
		raw, _ := io.ReadAll(part)
		if !bytes.Equal(raw, data) {
			t.Error("TUI media altered")
		}
		fmt.Fprint(w, `{"url":"http://example.test/i/song.wav"}`)
	}))
	defer server.Close()
	message := uploadTypedPaths(config.Profile{Host: server.URL, Token: "secret", Mode: "both"}, []string{file}, texts("en"), "audio")
	if !strings.Contains(message, "http://example.test/i/song.wav") {
		t.Fatal(message)
	}
}

func TestMediaLinkEscaping(t *testing.T) {
	for _, kind := range []string{"audio/mpeg", "video/mp4"} {
		markdown, err := formatLink("markdown", "https://example.test/i/abc", "x]\n.mp3", kind)
		if err != nil || markdown != "[x .mp3](https://example.test/i/abc)" {
			t.Fatal(markdown, err)
		}
		html, err := formatLink("html", `https://example.test/i/a?x=1&y=2`, `x"<&>.mp3`, kind)
		if err != nil || !strings.Contains(html, `controls src="https://example.test/i/a?x=1&amp;y=2" title="x&quot;&lt;&amp;&gt;.mp3"`) {
			t.Fatal(html, err)
		}
	}
}
