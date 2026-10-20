package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"wercture/server/internal/auth"
	"wercture/server/internal/store"
)

type plainHasher struct{}

func (plainHasher) Hash(password []byte) (string, error) {
	if err := auth.ValidatePassword(password); err != nil {
		return "", err
	}
	return "plain:" + string(password), nil
}

func (plainHasher) Verify(password []byte, phc string) (bool, error) {
	return phc == "plain:"+string(password), nil
}

func TestLifecycle(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, err := EnsureSetup(dir, false)
	if err != nil || len(token) != 64 {
		t.Fatalf("setup token: %v %q", err, token)
	}
	app := New(st, plainHasher{})
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}

	setupBody := map[string]string{"username": "admin", "password": "secret1", "confirm": "secret1"}
	res := do(t, client, http.MethodPost, ts.URL+"/api/v1/setup", setupBody, map[string]string{"X-Wercture-Setup": token})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("setup %d %s", res.StatusCode, readBody(res))
	}

	login := do(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "secret1"}, nil)
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login %d %s", login.StatusCode, readBody(login))
	}

	warehouse := decode[store.Warehouse](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Album"}, nil))
	other := decode[store.Warehouse](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Other"}, nil))
	_ = other

	pngBytes := tinyPNG(t)
	uploaded := upload(t, client, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(warehouse.ID, 10)+"/images", "dot.png", pngBytes, nil)
	publicID, _ := uploaded["public_id"].(string)
	if publicID == "" || uploaded["path"] != "/i/"+publicID+".png" || !strings.HasSuffix(uploaded["url"].(string), "/i/"+publicID+".png") {
		t.Fatalf("upload response %#v", uploaded)
	}

	forwarded := &http.Request{Header: http.Header{}}
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	forwarded.Header.Set("X-Forwarded-Host", "img.example.com")
	forwarded.Host = "127.0.0.1:9416"
	if got := publicOrigin(forwarded); got != "https://img.example.com" {
		t.Fatalf("origin %s", got)
	}

	pub, err := http.Get(ts.URL + "/i/" + publicID + ".png")
	if err != nil || pub.StatusCode != http.StatusOK || pub.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("public image %v %d", err, statusOf(pub))
	}
	pub.Body.Close()
	bare, err := http.Get(ts.URL + "/i/" + publicID)
	if err != nil || bare.StatusCode != http.StatusOK {
		t.Fatalf("bare public image %v %d", err, statusOf(bare))
	}
	bare.Body.Close()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	wrong, err := noFollow.Get(ts.URL + "/i/" + publicID + ".jpg")
	if err != nil || wrong.StatusCode != http.StatusPermanentRedirect || !strings.HasSuffix(wrong.Header.Get("Location"), "/i/"+publicID+".png") {
		t.Fatalf("wrong suffix %v %d %s", err, statusOf(wrong), wrong.Header.Get("Location"))
	}
	wrong.Body.Close()

	guest := &http.Client{}
	denied := do(t, guest, http.MethodGet, ts.URL+"/api/v1/warehouses", nil, nil)
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("guest warehouses %d", denied.StatusCode)
	}

	created := decode[store.User](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/users", map[string]string{"username": "ada"}, nil))
	if created.Status != "pending" || len(created.Invite) != 64 {
		t.Fatalf("invite %#v", created)
	}
	adaJar, _ := cookiejar.New(nil)
	ada := &http.Client{Jar: adaJar}
	early := do(t, ada, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "ada", "password": "secret2"}, nil)
	if early.StatusCode != http.StatusUnauthorized {
		t.Fatalf("pending login %d", early.StatusCode)
	}
	early.Body.Close()
	short := do(t, ada, http.MethodPost, ts.URL+"/api/v1/join", map[string]string{"password": "short", "confirm": "short"}, map[string]string{"X-Wercture-Join": created.Invite})
	if short.StatusCode != http.StatusBadRequest || !strings.Contains(readBody(short), "weak_password") {
		t.Fatalf("short password %d", short.StatusCode)
	}
	joined := do(t, ada, http.MethodPost, ts.URL+"/api/v1/join", map[string]string{"password": "secret2", "confirm": "secret2"}, map[string]string{"X-Wercture-Join": created.Invite})
	if joined.StatusCode != http.StatusOK {
		t.Fatalf("join %d %s", joined.StatusCode, readBody(joined))
	}
	adaLogin := do(t, ada, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "ada", "password": "secret2"}, nil)
	if adaLogin.StatusCode != http.StatusOK {
		t.Fatalf("ada login %d %s", adaLogin.StatusCode, readBody(adaLogin))
	}
	hidden := do(t, ada, http.MethodGet, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(warehouse.ID, 10), nil, nil)
	if hidden.StatusCode != http.StatusForbidden {
		t.Fatalf("ada warehouse %d", hidden.StatusCode)
	}
	adaHome := decode[struct {
		Warehouses []store.Warehouse `json:"warehouses"`
	}](t, do(t, ada, http.MethodGet, ts.URL+"/api/v1/warehouses", nil, nil))
	if len(adaHome.Warehouses) != 0 {
		t.Fatalf("ada sees %d warehouses", len(adaHome.Warehouses))
	}
	trashDenied := do(t, ada, http.MethodGet, ts.URL+"/api/v1/trash", nil, nil)
	if trashDenied.StatusCode != http.StatusForbidden {
		t.Fatalf("ada trash %d", trashDenied.StatusCode)
	}

	adaWarehouse := decode[store.Warehouse](t, do(t, ada, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Ada"}, nil))
	tokenRes := decode[store.Token](t, do(t, ada, http.MethodPost, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(adaWarehouse.ID, 10)+"/tokens", map[string]string{"name": "cli"}, nil))
	tokenDenied := do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/images/trash", map[string]any{"ids": []int64{1}}, map[string]string{"Authorization": "Bearer " + tokenRes.Token})
	if tokenDenied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token cannot call session routes, got %d", tokenDenied.StatusCode)
	}
	tokenDenied.Body.Close()

	stopped := do(t, client, http.MethodPost, ts.URL+"/api/v1/users/"+strconv.FormatInt(created.ID, 10)+"/enabled", map[string]bool{"enabled": false}, nil)
	if stopped.StatusCode != http.StatusOK {
		t.Fatalf("disable %d %s", stopped.StatusCode, readBody(stopped))
	}
	if do(t, ada, http.MethodGet, ts.URL+"/api/v1/warehouses", nil, nil).StatusCode != http.StatusUnauthorized {
		t.Fatal("disabled session still worked")
	}
	stoppedLogin := do(t, ada, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "ada", "password": "secret2"}, nil)
	if stoppedLogin.StatusCode != http.StatusForbidden || !strings.Contains(readBody(stoppedLogin), "disabled") {
		t.Fatalf("disabled login %d", stoppedLogin.StatusCode)
	}
	if do(t, client, http.MethodPost, ts.URL+"/api/v1/users/"+strconv.FormatInt(created.ID, 10)+"/enabled", map[string]bool{"enabled": true}, nil).StatusCode != http.StatusOK {
		t.Fatal("enable")
	}
	if do(t, ada, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "ada", "password": "secret2"}, nil).StatusCode != http.StatusOK {
		t.Fatal("login after enable")
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func upload(t *testing.T, client *http.Client, url, name string, data []byte, header map[string]string) map[string]any {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("upload %d %s", res.StatusCode, raw)
	}
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func do(t *testing.T, client *http.Client, method, url string, payload any, header map[string]string) *http.Response {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func decode[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	defer res.Body.Close()
	var out T
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", res.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %v %s", err, raw)
	}
	return out
}

func readBody(res *http.Response) string {
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	return string(raw)
}

func statusOf(res *http.Response) int {
	if res == nil {
		return 0
	}
	return res.StatusCode
}

func TestTrashKeepsPublicID(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, err := EnsureSetup(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	app := New(st, plainHasher{})
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	do(t, client, http.MethodPost, ts.URL+"/api/v1/setup", map[string]string{"username": "admin", "password": "secret1", "confirm": "secret1"}, map[string]string{"X-Wercture-Setup": token}).Body.Close()
	do(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "secret1"}, nil).Body.Close()
	first := decode[store.Warehouse](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "One"}, nil))
	second := decode[store.Warehouse](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Two"}, nil))
	uploaded := upload(t, client, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(first.ID, 10)+"/images", "dot.png", tinyPNG(t), nil)
	publicID := uploaded["public_id"].(string)
	id := int64(uploaded["id"].(float64))

	renamed := decode[map[string]any](t, do(t, client, http.MethodPatch, ts.URL+"/api/v1/images/"+strconv.FormatInt(id, 10), map[string]string{"name": "renamed.png"}, nil))
	if renamed["public_id"] != publicID {
		t.Fatalf("rename changed id")
	}
	do(t, client, http.MethodPost, ts.URL+"/api/v1/images/move", map[string]any{"ids": []int64{id}, "warehouse_id": second.ID}, nil).Body.Close()
	do(t, client, http.MethodPost, ts.URL+"/api/v1/images/trash", map[string]any{"ids": []int64{id}}, nil).Body.Close()
	missing, err := http.Get(ts.URL + "/i/" + publicID)
	if err != nil || missing.StatusCode != http.StatusNotFound {
		t.Fatalf("trashed link %v %d", err, statusOf(missing))
	}
	missing.Body.Close()
	do(t, client, http.MethodPost, ts.URL+"/api/v1/trash/restore", map[string]any{"ids": []int64{id}}, nil).Body.Close()
	back, err := http.Get(ts.URL + "/i/" + publicID)
	if err != nil || back.StatusCode != http.StatusOK {
		t.Fatalf("restored link %v %d", err, statusOf(back))
	}
	back.Body.Close()

	do(t, client, http.MethodDelete, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(second.ID, 10), nil, nil).Body.Close()
	trash := decode[struct {
		Images []store.Image `json:"images"`
	}](t, do(t, client, http.MethodGet, ts.URL+"/api/v1/trash", nil, nil))
	if len(trash.Images) != 1 || trash.Images[0].PublicID != publicID || trash.Images[0].CanRestore {
		t.Fatalf("trash after warehouse delete %#v", trash.Images)
	}
	gone := do(t, client, http.MethodPost, ts.URL+"/api/v1/trash/restore", map[string]any{"ids": []int64{id}}, nil)
	if gone.StatusCode != http.StatusConflict {
		t.Fatalf("restore without warehouse %d", gone.StatusCode)
	}
	gone.Body.Close()

	tok := decode[store.Token](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(first.ID, 10)+"/tokens", map[string]string{"name": "cli"}, nil))
	bearerClient := &http.Client{}
	viaToken := upload(t, bearerClient, ts.URL+"/api/v1/upload", "dot.png", tinyPNG(t), map[string]string{"Authorization": "Bearer " + tok.Token})
	if viaToken["public_id"] == publicID {
		t.Fatal("token upload reused public id")
	}
	denied := do(t, bearerClient, http.MethodPost, ts.URL+"/api/v1/images/trash", map[string]any{"ids": []int64{int64(viaToken["id"].(float64))}}, map[string]string{"Authorization": "Bearer " + tok.Token})
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token trash %d", denied.StatusCode)
	}
	denied.Body.Close()

	emptied := do(t, client, http.MethodPost, ts.URL+"/api/v1/trash/empty", map[string]any{}, nil)
	if emptied.StatusCode != http.StatusOK {
		t.Fatalf("empty trash %d %s", emptied.StatusCode, readBody(emptied))
	}
	after := decode[struct {
		Images []store.Image `json:"images"`
	}](t, do(t, client, http.MethodGet, ts.URL+"/api/v1/trash", nil, nil))
	if len(after.Images) != 0 {
		t.Fatalf("trash after empty %#v", after.Images)
	}
	kept := do(t, client, http.MethodGet, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(first.ID, 10)+"/images", nil, nil)
	if kept.StatusCode != http.StatusOK || !strings.Contains(readBody(kept), viaToken["public_id"].(string)) {
		t.Fatalf("empty removed an active picture")
	}
}

func TestDocuments(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, err := EnsureSetup(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	app := New(st, plainHasher{})
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	do(t, client, http.MethodPost, ts.URL+"/api/v1/setup", map[string]string{"username": "admin", "password": "secret1", "confirm": "secret1"}, map[string]string{"X-Wercture-Setup": token}).Body.Close()
	do(t, client, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "secret1"}, nil).Body.Close()
	warehouse := decode[store.Warehouse](t, do(t, client, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Docs"}, nil))
	endpoint := ts.URL + "/api/v1/warehouses/" + strconv.FormatInt(warehouse.ID, 10) + "/images"

	pdf := upload(t, client, endpoint, "notes.pdf", samplePDF(), nil)
	if pdf["mime"] != "application/pdf" || pdf["path"] != "/i/"+pdf["public_id"].(string)+".pdf" || pdf["width"].(float64) != 900 {
		t.Fatalf("pdf %#v", pdf)
	}
	if got := inlineDisposition("PDF测试文档.pdf"); !strings.Contains(got, "filename*=utf-8''PDF%E6%B5%8B%E8%AF%95%E6%96%87%E6%A1%A3.pdf") {
		t.Fatalf("disposition %s", got)
	}
	body := httpGet(t, ts.URL+pdf["path"].(string))
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatal("pdf body")
	}
	shot := httpGet(t, ts.URL+"/i/"+pdf["public_id"].(string)+".preview.png")
	cfg, _, err := image.DecodeConfig(bytes.NewReader(shot))
	if err != nil || cfg.Width < 100 || cfg.Height < 100 {
		t.Fatalf("pdf preview %v %d", err, len(shot))
	}
	rejected := uploadRaw(t, client, endpoint, "notes.txt", []byte("# hello\n"), nil)
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("txt %d", rejected.StatusCode)
	}
	rejected.Body.Close()

	md := upload(t, client, endpoint, "note.md", []byte("# Title\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n$x^2$\n"), nil)
	if md["mime"] != "text/markdown" || !strings.HasSuffix(md["path"].(string), ".md") {
		t.Fatalf("markdown %#v", md)
	}
	page, err := http.Get(ts.URL + md["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	defer page.Body.Close()
	html, err := io.ReadAll(page.Body)
	if err != nil || page.StatusCode != http.StatusOK || !strings.HasPrefix(page.Header.Get("Content-Type"), "text/html") || !bytes.Contains(html, []byte("id=\"doc\"")) {
		t.Fatalf("markdown page %d %s %s", page.StatusCode, page.Header.Get("Content-Type"), html)
	}
	renderPath, _ := md["render_path"].(string)
	if !strings.HasSuffix(renderPath, ".html") {
		t.Fatalf("render path %#v", md["render_path"])
	}
	rendered, err := http.Get(ts.URL + renderPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rendered.Body.Close()
	if rendered.StatusCode != http.StatusOK || !strings.HasPrefix(rendered.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("render page %d %s", rendered.StatusCode, rendered.Header.Get("Content-Type"))
	}
	pdfRes, err := http.Get(ts.URL + pdf["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	pdfRes.Body.Close()
	if pdfRes.Header.Get("Cache-Control") != publicCacheControl {
		t.Fatalf("cache %q", pdfRes.Header.Get("Cache-Control"))
	}
	htmlForPDF, err := http.Get(ts.URL + "/i/" + pdf["public_id"].(string) + ".html")
	if err != nil {
		t.Fatal(err)
	}
	htmlForPDF.Body.Close()
	if htmlForPDF.StatusCode != http.StatusNotFound {
		t.Fatalf("pdf html %d", htmlForPDF.StatusCode)
	}
	raw := httpGet(t, ts.URL+md["path"].(string)+"?raw=1")
	if !bytes.Contains(raw, []byte("# Title")) {
		t.Fatalf("raw markdown %s", raw)
	}
	renderRaw, err := http.Get(ts.URL + renderPath + "?raw=1")
	if err != nil {
		t.Fatal(err)
	}
	defer renderRaw.Body.Close()
	renderRawBody, err := io.ReadAll(renderRaw.Body)
	if err != nil || renderRaw.StatusCode != http.StatusOK || !strings.HasPrefix(renderRaw.Header.Get("Content-Type"), "text/markdown") || !bytes.Contains(renderRawBody, []byte("# Title")) {
		t.Fatalf("render raw %d %s %s", renderRaw.StatusCode, renderRaw.Header.Get("Content-Type"), renderRawBody)
	}
	mdShot := httpGet(t, ts.URL+"/i/"+md["public_id"].(string)+".preview.png")
	if _, _, err := image.DecodeConfig(bytes.NewReader(mdShot)); err != nil {
		t.Fatal(err)
	}
	if previewRes, err := http.Get(ts.URL + "/i/" + md["public_id"].(string) + ".preview.png"); err != nil {
		t.Fatal(err)
	} else {
		previewRes.Body.Close()
		if previewRes.Header.Get("Cache-Control") != publicCacheControl {
			t.Fatalf("preview cache %q", previewRes.Header.Get("Cache-Control"))
		}
	}
	trashed := do(t, client, http.MethodPost, ts.URL+"/api/v1/images/trash", map[string]any{"ids": []int64{int64(md["id"].(float64))}}, nil)
	if trashed.StatusCode != http.StatusOK {
		t.Fatalf("trash %d %s", trashed.StatusCode, readBody(trashed))
	}
	trashed.Body.Close()
	gone, err := http.Get(ts.URL + renderPath)
	if err != nil {
		t.Fatal(err)
	}
	gone.Body.Close()
	if gone.StatusCode != http.StatusNotFound {
		t.Fatalf("trashed render %d", gone.StatusCode)
	}
}

func TestLoginLockIgnoresForwardedFor(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	token, err := EnsureSetup(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	app := New(st, plainHasher{})
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/setup", map[string]string{"username": "admin", "password": "secret1", "confirm": "secret1"}, map[string]string{"X-Wercture-Setup": token}).Body.Close()
	for i := 0; i < 4; i++ {
		res := do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong1"}, map[string]string{"X-Forwarded-For": "203.0.113." + strconv.Itoa(i)})
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status %d %s", i, res.StatusCode, readBody(res))
		}
		res.Body.Close()
	}
	locked := do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong1"}, map[string]string{"X-Forwarded-For": "198.51.100.8"})
	if locked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("lock %d %s", locked.StatusCode, readBody(locked))
	}
	locked.Body.Close()
	again := do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong1"}, map[string]string{"X-Forwarded-For": "198.51.100.9"})
	if again.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("forwarded %d %s", again.StatusCode, readBody(again))
	}
	again.Body.Close()
}

func TestLoginGuardDropsExpired(t *testing.T) {
	guard := newLoginGuard()
	guard.attempts["203.0.113.5"] = loginAttempt{first: time.Now().Add(-time.Hour), failures: 5, blockedUntil: time.Now().Add(-time.Minute)}
	if guard.blocked("203.0.113.5") {
		t.Fatal("expired ban still blocked")
	}
	guard.mu.Lock()
	_, ok := guard.attempts["203.0.113.5"]
	guard.mu.Unlock()
	if ok {
		t.Fatal("expired attempt kept")
	}
}

func TestHashSlots(t *testing.T) {
	app := New(nil, plainHasher{})
	if !app.beginHash() || !app.beginHash() {
		t.Fatal("slots")
	}
	if app.beginHash() {
		t.Fatal("third hash started")
	}
	app.endHash()
	if !app.beginHash() {
		t.Fatal("released slot")
	}
}

func TestDisabledTokenRejected(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	setup, err := EnsureSetup(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	app := New(st, plainHasher{})
	ts := httptest.NewServer(app.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	admin := &http.Client{Jar: jar}
	do(t, admin, http.MethodPost, ts.URL+"/api/v1/setup", map[string]string{"username": "admin", "password": "secret1", "confirm": "secret1"}, map[string]string{"X-Wercture-Setup": setup}).Body.Close()
	do(t, admin, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "admin", "password": "secret1"}, nil).Body.Close()
	created := decode[store.User](t, do(t, admin, http.MethodPost, ts.URL+"/api/v1/users", map[string]string{"username": "bob"}, nil))
	do(t, &http.Client{}, http.MethodPost, ts.URL+"/api/v1/join", map[string]string{"password": "secret1", "confirm": "secret1"}, map[string]string{"X-Wercture-Join": created.Invite}).Body.Close()
	userJar, _ := cookiejar.New(nil)
	user := &http.Client{Jar: userJar}
	do(t, user, http.MethodPost, ts.URL+"/api/v1/auth/login", map[string]string{"username": "bob", "password": "secret1"}, nil).Body.Close()
	warehouse := decode[store.Warehouse](t, do(t, user, http.MethodPost, ts.URL+"/api/v1/warehouses", map[string]string{"name": "Bob"}, nil))
	token := decode[store.Token](t, do(t, user, http.MethodPost, ts.URL+"/api/v1/warehouses/"+strconv.FormatInt(warehouse.ID, 10)+"/tokens", map[string]string{"name": "ci"}, nil))
	upload(t, &http.Client{}, ts.URL+"/api/v1/upload", "dot.png", tinyPNG(t), map[string]string{"Authorization": "Bearer " + token.Token})
	stopped := do(t, admin, http.MethodPost, ts.URL+"/api/v1/users/"+strconv.FormatInt(created.ID, 10)+"/enabled", map[string]bool{"enabled": false}, nil)
	if stopped.StatusCode != http.StatusOK {
		t.Fatalf("disable %d %s", stopped.StatusCode, readBody(stopped))
	}
	stopped.Body.Close()
	denied := uploadRaw(t, &http.Client{}, ts.URL+"/api/v1/upload", "dot.png", tinyPNG(t), map[string]string{"Authorization": "Bearer " + token.Token})
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("disabled token %d %s", denied.StatusCode, readBody(denied))
	}
	denied.Body.Close()
}

func samplePDF() []byte {
	stream := []byte("BT /F1 24 Tf 72 200 Td (Hello PDF) Tj ET\n")
	return buildPDF([]string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 400] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>\nendobj\n",
		"4 0 obj\n<< /Length " + strconv.Itoa(len(stream)) + " >>\nstream\n" + string(stream) + "endstream\nendobj\n",
		"5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n",
	})
}

func buildPDF(objs []string) []byte {
	var body strings.Builder
	body.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, obj := range objs {
		offsets[i] = body.Len()
		body.WriteString(obj)
	}
	start := body.Len()
	body.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for _, off := range offsets {
		body.WriteString(fmtXref(off))
	}
	body.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n")
	body.WriteString(strconv.Itoa(start))
	body.WriteString("\n%%EOF\n")
	return []byte(body.String())
}

func fmtXref(off int) string {
	return sprintf10(off) + " 00000 n \n"
}

func sprintf10(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}

func httpGet(t *testing.T, url string) []byte {
	t.Helper()
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("get %s %d %v", url, res.StatusCode, err)
	}
	return body
}

func uploadRaw(t *testing.T, client *http.Client, url, name string, data []byte, header map[string]string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	req, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	for key, value := range header {
		req.Header.Set(key, value)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
