package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func mediaFixture(t *testing.T, ext string) []byte {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	name := filepath.Join(t.TempDir(), "sample"+ext)
	args := []string{"-v", "error", "-f", "lavfi"}
	if ext == ".mp4" || ext == ".webm" {
		args = append(args, "-i", "testsrc=size=960x540:rate=10", "-t", "0.4", "-pix_fmt", "yuv420p")
		if ext == ".mp4" {
			args = append(args, "-c:v", "libx264")
		} else {
			args = append(args, "-c:v", "libvpx-vp9")
		}
	} else {
		args = append(args, "-i", "sine=frequency=440:sample_rate=16000", "-t", "0.4")
	}
	args = append(args, "-threads", "1", name)
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("fixture %s: %v %s", ext, err, out)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func uploadRequest(t *testing.T, name string, data []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	writer.Close()
	r := httptest.NewRequest("POST", "/api/v1/upload", &buf)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	return r
}

func TestMediaUploadPlaybackAndLifecycle(t *testing.T) {
	st, admin := regressionStore(t)
	wh, _ := st.CreateWarehouse(admin, "media")
	target, _ := st.CreateWarehouse(admin, "copies")
	token, err := st.CreateToken(admin, wh.ID, "uploader")
	if err != nil {
		t.Fatal(err)
	}
	session, _, _ := st.CreateSession(admin.ID)
	app := New(st, plainHasher{})
	for _, ext := range []string{".mp4", ".webm", ".mp3", ".m4a", ".aac", ".wav", ".ogg", ".flac"} {
		t.Run(ext, func(t *testing.T) {
			data := mediaFixture(t, ext)
			r := uploadRequest(t, "sample"+ext, data)
			r.Header.Set("Authorization", "Bearer "+token.Token)
			w := httptest.NewRecorder()
			app.Handler().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("upload: %d %s", w.Code, w.Body.String())
			}
			var item struct {
				ID       int64  `json:"id"`
				PublicID string `json:"public_id"`
				Path     string `json:"path"`
				MIME     string `json:"mime"`
				HTML     string `json:"html"`
				Markdown string `json:"markdown"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(item.Path, ext) || !strings.Contains(item.HTML, "controls") || strings.HasPrefix(item.Markdown, "!") {
				t.Fatalf("links: %s", w.Body.String())
			}
			for _, path := range []string{item.Path, "/i/" + item.PublicID} {
				res := regressionRequest(t, app, "GET", path, "", "")
				if res.Code != 200 || !bytes.Equal(res.Body.Bytes(), data) || res.Header().Get("Content-Type") != item.MIME {
					t.Fatalf("direct link %d", res.Code)
				}
			}
			head := regressionRequest(t, app, "HEAD", item.Path, "", "")
			if head.Code != 200 || head.Body.Len() != 0 {
				t.Fatal("HEAD")
			}
			for _, rangeCase := range []struct {
				value  string
				status int
			}{{"bytes=2-5", 206}, {"bytes=999999999999-", 416}} {
				req := httptest.NewRequest("GET", item.Path, nil)
				req.Header.Set("Range", rangeCase.value)
				res := httptest.NewRecorder()
				app.Handler().ServeHTTP(res, req)
				if res.Code != rangeCase.status {
					t.Fatalf("range %s: %d", rangeCase.value, res.Code)
				}
				if res.Code == 206 && (!bytes.Equal(res.Body.Bytes(), data[2:6]) || res.Header().Get("Content-Range") == "") {
					t.Fatal("range content")
				}
			}
			thumbPath := "/i/" + item.PublicID + ".preview.png"
			thumb := regressionRequest(t, app, "GET", thumbPath, "", "")
			if thumb.Code != 200 {
				t.Fatalf("thumbnail %d", thumb.Code)
			}
			if strings.HasPrefix(item.MIME, "video/") {
				cfg, _, err := image.DecodeConfig(bytes.NewReader(thumb.Body.Bytes()))
				if err != nil || cfg.Width > 640 || cfg.Height > 640 || cfg.Width != 640 || cfg.Height != 360 {
					t.Fatalf("dimensions: %+v %v", cfg, err)
				}
			} else if thumb.Header().Get("Content-Type") != "image/svg+xml" {
				t.Fatal("audio icon")
			}
			body := fmt.Sprintf(`{"ids":[%d],"warehouse_id":%d}`, item.ID, target.ID)
			if res := regressionRequest(t, app, "POST", "/api/v1/images/copy", body, session); res.Code != 200 {
				t.Fatalf("copy %d %s", res.Code, res.Body.String())
			}
			body = fmt.Sprintf(`{"ids":[%d]}`, item.ID)
			if res := regressionRequest(t, app, "POST", "/api/v1/images/trash", body, session); res.Code != 200 {
				t.Fatal("trash")
			}
			for _, path := range []string{item.Path, thumbPath} {
				if res := regressionRequest(t, app, "GET", path, "", ""); res.Code != 404 {
					t.Fatal("trashed file public")
				}
			}
			raw := fmt.Sprintf("/api/v1/images/%d/raw", item.ID)
			if res := regressionRequest(t, app, "GET", raw, "", ""); res.Code != 401 {
				t.Fatal("guest raw")
			}
			if res := regressionRequest(t, app, "GET", raw, "", session); res.Code != 200 {
				t.Fatal("admin raw")
			}
			if res := regressionRequest(t, app, "GET", fmt.Sprintf("/api/v1/images/%d/preview", item.ID), "", session); res.Code != 200 {
				t.Fatal("admin thumbnail")
			}
			if res := regressionRequest(t, app, "POST", "/api/v1/trash/restore", body, session); res.Code != 200 {
				t.Fatal("restore")
			}
			if res := regressionRequest(t, app, "GET", item.Path, "", ""); res.Code != 200 {
				t.Fatal("restored link")
			}
			if err := st.TrashImages(admin, []int64{item.ID}); err != nil {
				t.Fatal(err)
			}
			if err := st.PurgeImages(admin, []int64{item.ID}); err != nil {
				t.Fatal(err)
			}
		})
	}
	copied, _, err := st.ListImages(admin, target.ID, 0, 100)
	if err != nil || len(copied) != 8 {
		t.Fatalf("copies %d %v", len(copied), err)
	}
	for _, item := range copied {
		if strings.HasPrefix(item.MIME, "video/") {
			if _, err := os.Stat(st.FilePath(item) + ".preview.png"); err != nil {
				t.Fatal("missing copied thumbnail")
			}
		}
	}
}

func TestUploadBoundariesAndCleanup(t *testing.T) {
	st, admin := regressionStore(t)
	wh, _ := st.CreateWarehouse(admin, "files")
	app := New(st, plainHasher{})
	data := tinyPNG(t)
	limit := int64(len(data))
	if _, err := st.UpdateSettings("en", "system", &limit); err != nil {
		t.Fatal(err)
	}
	item, err := app.acceptUpload(uploadRequest(t, "a.png", data), wh.ID, admin.ID)
	if err != nil {
		t.Fatal("exact limit", err)
	}
	if item.Size != limit {
		t.Fatal("size")
	}
	over := uploadRequest(t, "a.png", append(data, 0))
	if _, err := app.acceptUpload(over, wh.ID, admin.ID); err != errTooLarge {
		t.Fatalf("over limit: %v", err)
	}
	token, _ := st.CreateToken(admin, wh.ID, "limited")
	over = uploadRequest(t, "a.png", append(data, 0))
	over.Header.Set("Authorization", "Bearer "+token.Token)
	response := httptest.NewRecorder()
	app.Handler().ServeHTTP(response, over)
	if response.Code != 413 {
		t.Fatalf("expected 413: %d", response.Code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := app.acceptUpload(uploadRequest(t, "a.png", data).WithContext(ctx), wh.ID, admin.ID); err == nil {
		t.Fatal("cancel accepted")
	}
	for _, name := range []string{"fake.mp4", "fake.mp3", "fake.wav", "bad.md"} {
		if _, err := app.acceptUpload(uploadRequest(t, name, []byte{1, 2, 0, 0xff}), wh.ID, admin.ID); err == nil {
			t.Fatalf("fake accepted: %s", name)
		}
	}
	files, _ := os.ReadDir(filepath.Join(st.DataDir(), "files"))
	if len(files) != 1 {
		t.Fatalf("staged files leaked: %v", files)
	}
}

func TestMediaCancellationAndBrokenThumbnailCleanup(t *testing.T) {
	st, admin := regressionStore(t)
	wh, _ := st.CreateWarehouse(admin, "files")
	app := New(st, plainHasher{})
	data := mediaFixture(t, ".mp4")
	// Probe succeeds, but the thumbnail process fails.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := app.acceptUpload(uploadRequest(t, "a.mp4", data), wh.ID, admin.ID); err == nil {
		t.Fatal("broken thumbnail accepted")
	}
	files, _ := os.ReadDir(filepath.Join(st.DataDir(), "files"))
	if len(files) != 0 {
		t.Fatal("partial file leaked")
	}
	// A saturated probe queue obeys the request deadline without starting another process.
	app.mediaSlots <- struct{}{}
	app.mediaSlots <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, _, _, err := app.inspectMedia(ctx, "unused", "a.mp4", nil)
	if err == nil {
		t.Fatal("deadline ignored")
	}
	<-app.mediaSlots
	<-app.mediaSlots
}

func TestFileUploadDoesNotBufferLargeOriginal(t *testing.T) {
	st, admin := regressionStore(t)
	wh, _ := st.CreateWarehouse(admin, "large")
	app := New(st, plainHasher{})
	pngData := tinyPNG(t)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	done := make(chan error, 1)
	go func() {
		part, err := multipartWriter.CreateFormFile("file", "large.png")
		if err == nil {
			_, err = part.Write(pngData)
		}
		if err == nil {
			_, err = io.CopyN(part, zeroReader{}, 64<<20)
		}
		if err == nil {
			err = multipartWriter.Close()
		}
		writer.CloseWithError(err)
		done <- err
	}()
	req := httptest.NewRequest("POST", "/", reader)
	req.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	item, err := app.acceptUpload(req, wh.ID, admin.ID)
	if err != nil {
		reader.Close()
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if item.Size < 64<<20 {
		t.Fatal("truncated")
	}
	target := filepath.Join(t.TempDir(), "copy")
	if err := copyStored(st.FilePath(item), target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Size() != item.Size {
		t.Fatal("copy size")
	}
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 16<<20 {
		t.Fatalf("upload/copy allocated %d bytes for a 64 MiB original", allocated)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestMediaThumbnailRotationAndPixelAspect(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	st, admin := regressionStore(t)
	wh, _ := st.CreateWarehouse(admin, "shapes")
	app := New(st, plainHasher{})
	dir := t.TempDir()
	original := filepath.Join(dir, "original.mp4")
	rotated := filepath.Join(dir, "rotated.mp4")
	for _, args := range [][]string{
		{"-v", "error", "-f", "lavfi", "-i", "color=c=blue:size=320x180:rate=10", "-t", "0.3", "-vf", "setsar=2", "-c:v", "libx264", "-threads", "1", original},
	} {
		if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	// Write a display matrix directly so the fixture works across FFmpeg versions.
	rotatedData, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	matrix := bytes.Index(rotatedData, []byte("tkhd")) + 44
	if matrix < 44 || matrix+36 > len(rotatedData) {
		t.Fatal("missing track header")
	}
	for i, value := range []int32{0, 65536, 0, -65536, 0, 0, 0, 0, 1 << 30} {
		binary.BigEndian.PutUint32(rotatedData[matrix+4*i:], uint32(value))
	}
	if err := os.WriteFile(rotated, rotatedData, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		w, h int
	}{{original, 640, 180}, {rotated, 180, 640}} {
		data, _ := os.ReadFile(tc.path)
		item, err := app.acceptUpload(uploadRequest(t, "sample.mp4", data), wh.ID, admin.ID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Width != tc.w || item.Height != tc.h {
			t.Fatalf("display metadata %dx%d, want %dx%d", item.Width, item.Height, tc.w, tc.h)
		}
		f, err := os.Open(st.FilePath(item) + ".preview.png")
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width > 640 || cfg.Height > 640 || cfg.Width*tc.h != cfg.Height*tc.w {
			t.Fatalf("thumbnail %dx%d, want %dx%d (%v)", cfg.Width, cfg.Height, tc.w, tc.h, err)
		}
	}
}

func TestMediaProcessDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runMedia(ctx, "sh", "-c", "exec sleep 60"); err == nil {
		t.Fatal("process ignored deadline")
	}
	if time.Since(start) > time.Second {
		t.Fatal("media process was not stopped promptly")
	}
}

func TestLargePDFUsesBoundedPlaceholder(t *testing.T) {
	st, _ := regressionStore(t)
	app := New(st, plainHasher{})
	file := filepath.Join(st.DataDir(), "files", "large-pdf")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("%PDF-1.4\n")
	if err := f.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Warm the shared font before measuring per-file preview allocations.
	warm := filepath.Join(st.DataDir(), "files", "warm.md")
	if err := os.WriteFile(warm, []byte("# Preview"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := app.filePreview(warm, "text/markdown"); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := app.filePreview(file, "application/pdf"); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if after.TotalAlloc-before.TotalAlloc > 16<<20 {
		t.Fatalf("1 GiB PDF preview allocated %d bytes", after.TotalAlloc-before.TotalAlloc)
	}
	imageFile, err := os.Open(file + ".preview.png")
	if err != nil {
		t.Fatal(err)
	}
	defer imageFile.Close()
	cfg, _, err := image.DecodeConfig(imageFile)
	if err != nil || cfg.Width != 900 || cfg.Height != 1200 {
		t.Fatal("missing PDF placeholder")
	}
}

func TestProbeOutputIsBounded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := runMedia(ctx, "head", "-c", "2097152", "/dev/zero"); err == nil {
		t.Fatal("oversized probe output accepted")
	}
}
