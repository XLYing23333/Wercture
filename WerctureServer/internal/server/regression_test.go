package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"wercture/server/internal/store"
)

func regressionStore(t *testing.T) (*store.Store, store.User) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateAdmin("admin", "plain:secret1"); err != nil {
		t.Fatal(err)
	}
	admin, err := st.UserByName("admin")
	if err != nil {
		t.Fatal(err)
	}
	return st, admin
}

func regressionRequest(t *testing.T, app *Server, method, path, body, session string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if session != "" {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: session})
	}
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	return w
}

func TestSharedAddressAccountsHaveIndependentLoginLimits(t *testing.T) {
	st, _ := regressionStore(t)
	app := New(st, plainHasher{})
	login := func(username, password, forwarded string) int {
		body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
		r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
		r.RemoteAddr = "192.0.2.1:12345"
		r.Header.Set("X-Forwarded-For", forwarded)
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		login("unknown", "wrong", "198.51.100.1")
	}
	if got := login("admin", "secret1", "198.51.100.2"); got != 200 {
		t.Fatalf("unrelated account locked: %d", got)
	}
	if got := login("another-unknown", "wrong", "198.51.100.99"); got != 429 {
		t.Fatalf("forwarding header bypassed limit: %d", got)
	}
	for i := 0; i < 5; i++ {
		login("admin", "wrong", "198.51.100.2")
	}
	if got := login("admin", "secret1", "198.51.100.3"); got != 429 {
		t.Fatalf("failing account not locked: %d", got)
	}
}

func TestAllPasswordEndpointsRespectHashCapacity(t *testing.T) {
	st, admin := regressionStore(t)
	user, err := st.CreateUser("pending")
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := st.CreateSession(admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	app := New(st, plainHasher{})
	app.beginHash()
	app.beginHash()
	defer app.endHash()
	defer app.endHash()
	for _, path := range []string{"/api/v1/auth/password", "/api/v1/users/" + strconv.FormatInt(user.ID, 10) + "/password"} {
		w := regressionRequest(t, app, "POST", path, `{"current":"secret1","password":"secret2","confirm":"secret2"}`, session)
		// Reset-password does not accept the "current" field.
		if strings.Contains(path, "/users/") {
			w = regressionRequest(t, app, "POST", path, `{"password":"secret2","confirm":"secret2"}`, session)
		}
		if w.Code != 429 {
			t.Fatalf("%s bypassed capacity: %d %s", path, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/join", strings.NewReader(`{"password":"secret1","confirm":"secret1"}`))
	r.Header.Set(joinHeader, user.Invite)
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatalf("join bypassed capacity: %d", w.Code)
	}
	if _, err := st.UserByInvite(user.Invite); err != nil {
		t.Fatal("busy join consumed invite")
	}
	setupStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer setupStore.Close()
	secret, err := EnsureSetup(setupStore.DataDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	setupApp := New(setupStore, plainHasher{})
	setupApp.beginHash()
	setupApp.beginHash()
	r = httptest.NewRequest("POST", "/api/v1/setup", strings.NewReader(`{"username":"admin","password":"secret1","confirm":"secret1"}`))
	r.Header.Set(setupHeader, secret)
	w = httptest.NewRecorder()
	setupApp.Handler().ServeHTTP(w, r)
	if w.Code != 429 {
		t.Fatalf("setup bypassed capacity: %d", w.Code)
	}
	setupApp.endHash()
	setupApp.endHash()
}

func TestBatchCopyRollsBackOnInvalidSourceAndMissingFile(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			st, admin := regressionStore(t)
			src, _ := st.CreateWarehouse(admin, "source")
			dst, _ := st.CreateWarehouse(admin, "target")
			var ids []int64
			for i := 0; i < 2; i++ {
				rel := fmt.Sprintf("files/original%d", i)
				if i == 0 || !missing {
					if err := os.WriteFile(filepath.Join(st.DataDir(), rel), tinyPNG(t), 0600); err != nil {
						t.Fatal(err)
					}
				}
				image, err := st.InsertImage(store.NewImage{PublicID: fmt.Sprintf("original%d", i), Name: "test.png", MIME: "image/png", Size: 1, Width: 1, Height: 1, StoragePath: rel, WarehouseID: src.ID, UploaderID: admin.ID})
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, image.ID)
			}
			if !missing {
				ids[1] = 999999
			}
			session, _, _ := st.CreateSession(admin.ID)
			w := regressionRequest(t, New(st, plainHasher{}), "POST", "/api/v1/images/copy", fmt.Sprintf(`{"ids":[%d,%d],"warehouse_id":%d}`, ids[0], ids[1], dst.ID), session)
			if w.Code == 200 {
				t.Fatal("expected error")
			}
			images, _, err := st.ListImages(admin, dst.ID, 0, 100)
			if err != nil || len(images) != 0 {
				t.Fatalf("partial copies: %d %v", len(images), err)
			}
			files, _ := os.ReadDir(filepath.Join(st.DataDir(), "files"))
			want := 2
			if missing {
				want = 1
			}
			if len(files) != want {
				t.Fatalf("staged files leaked: %d", len(files))
			}
		})
	}
}

func TestDeleteUserModesKeepOrRemoveFiles(t *testing.T) {
	for _, mode := range []string{"trash", "purge"} {
		t.Run(mode, func(t *testing.T) {
			st, admin := regressionStore(t)
			pending, err := st.CreateUser("other")
			if err != nil {
				t.Fatal(err)
			}
			if err := st.ActivateUser(pending.ID, "plain:secret1"); err != nil {
				t.Fatal(err)
			}
			user, _ := st.UserByName("other")
			warehouse, _ := st.CreateWarehouse(user, "other")
			rel := "files/document"
			if err := os.WriteFile(filepath.Join(st.DataDir(), rel), []byte("test"), 0600); err != nil {
				t.Fatal(err)
			}
			image, err := st.InsertImage(store.NewImage{PublicID: "document", Name: "test.md", MIME: "text/markdown", StoragePath: rel, WarehouseID: warehouse.ID, UploaderID: user.ID})
			if err != nil {
				t.Fatal(err)
			}
			session, _, _ := st.CreateSession(admin.ID)
			w := regressionRequest(t, New(st, plainHasher{}), "DELETE", fmt.Sprintf("/api/v1/users/%d?mode=%s", user.ID, mode), "", session)
			if w.Code != 200 {
				t.Fatalf("delete: %d %s", w.Code, w.Body.String())
			}
			_, statErr := os.Stat(filepath.Join(st.DataDir(), rel))
			trash, _ := st.ListTrash(admin)
			if mode == "purge" {
				if !os.IsNotExist(statErr) || len(trash) != 0 {
					t.Fatal("purge kept files")
				}
			} else {
				if statErr != nil || len(trash) != 1 || trash[0].CanRestore || trash[0].PublicID != image.PublicID {
					t.Fatal("trash did not preserve detached file")
				}
			}
		})
	}
}
