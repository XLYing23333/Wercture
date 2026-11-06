package server

import (
	"encoding/json"
	"io"
	"testing"
)

func TestUploadLimitSettingsAuthorizationAndCompatibility(t *testing.T) {
	st, admin := regressionStore(t)
	app := New(st, plainHasher{})
	adminSession, _, _ := st.CreateSession(admin.ID)
	user, _ := st.CreateUser("other")
	st.ActivateUser(user.ID, "plain:secret2")
	userSession, _, _ := st.CreateSession(user.ID)
	body := `{"locale":"en","theme":"dark","max_upload_bytes":1048576}`
	for _, session := range []string{"", userSession} {
		res := regressionRequest(t, app, "PUT", "/api/v1/settings", body, session)
		if res.Code != 401 && res.Code != 403 {
			t.Fatal("unauthorized update")
		}
	}
	res := regressionRequest(t, app, "PUT", "/api/v1/settings", body, adminSession)
	if res.Code != 200 {
		t.Fatalf("admin %d %s", res.Code, res.Body.String())
	}
	res = regressionRequest(t, app, "PUT", "/api/v1/settings", `{"locale":"zh-CN","theme":"light"}`, adminSession)
	var value struct {
		Limit int64 `json:"max_upload_bytes"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil || value.Limit != 1<<20 {
		t.Fatal("legacy request reset limit")
	}
	for _, bad := range []string{`0`, `-1`, `1.5`, `"1024"`, `9223372036854775808`} {
		res = regressionRequest(t, app, "PUT", "/api/v1/settings", `{"locale":"en","theme":"dark","max_upload_bytes":`+bad+`}`, adminSession)
		if res.Code != 400 {
			t.Fatalf("accepted %s", bad)
		}
	}
	saved, _ := st.GetSettings()
	if saved.Locale != "zh-CN" || saved.Theme != "light" || saved.MaxUploadBytes != 1<<20 {
		t.Fatal("invalid request partially saved")
	}
}

type firstReadHook struct {
	reader io.ReadCloser
	before func()
}

func (r *firstReadHook) Read(p []byte) (int, error) {
	if r.before != nil {
		fn := r.before
		r.before = nil
		fn()
	}
	return r.reader.Read(p)
}
func (r *firstReadHook) Close() error { return r.reader.Close() }

func TestUploadSnapshotsLimitAtStart(t *testing.T) {
	st, admin := regressionStore(t)
	warehouse, _ := st.CreateWarehouse(admin, "limits")
	app := New(st, plainHasher{})
	data := tinyPNG(t)
	limit := int64(len(data))
	if _, err := st.UpdateSettings("en", "system", &limit); err != nil {
		t.Fatal(err)
	}
	req := uploadRequest(t, "a.png", data)
	req.Body = &firstReadHook{reader: req.Body, before: func() {
		smaller := limit - 1
		if _, err := st.UpdateSettings("en", "system", &smaller); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := app.acceptUpload(req, warehouse.ID, admin.ID); err != nil {
		t.Fatalf("in-flight limit changed: %v", err)
	}
	if _, err := app.acceptUpload(uploadRequest(t, "b.png", data), warehouse.ID, admin.ID); err != errTooLarge {
		t.Fatalf("new request missed new limit: %v", err)
	}
}
