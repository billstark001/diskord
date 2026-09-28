package ui

import (
	"bytes"
	"diskord/internal/config"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) *UI {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(key, os.Getenv(key))
	}
	path := filepath.Join(root, "diskord.yaml")
	body := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+filepath.ToSlash(root), 1)
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	return &UI{Manager: manager, token: "correct", session: "session", csrf: "csrf"}
}
func TestLocalConsoleBoundaries(t *testing.T) {
	u := fixture(t)
	handler := u.security(u.auth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })))
	for _, tt := range []struct {
		method, host, origin, remote, cookie, fetchMode, fetchDest, fetchSite string
		status                                                                int
	}{
		{"GET", "127.0.0.1:3900", "", "127.0.0.1:4567", "session", "", "", "", 204},
		{"GET", "localhost:3900", "http://localhost:3900", "127.0.0.1:4567", "session", "", "", "", 204},
		{"GET", "evil.test:3900", "", "127.0.0.1:4567", "session", "", "", "", 403},
		{"GET", "127.0.0.1:3900", "https://evil.test", "127.0.0.1:4567", "session", "", "", "", 403},
		{"GET", "127.0.0.1:3900", "https://evil.test", "127.0.0.1:4567", "session", "navigate", "document", "cross-site", 204},
		{"POST", "127.0.0.1:3900", "https://evil.test", "127.0.0.1:4567", "session", "navigate", "document", "cross-site", 403},
		{"POST", "127.0.0.1:3900", "null", "127.0.0.1:4567", "session", "navigate", "document", "same-origin", 204},
		{"GET", "127.0.0.1:3900", "", "192.0.2.1:4567", "session", "", "", "", 403},
		{"GET", "127.0.0.1:3900", "", "127.0.0.1:4567", "", "", "", "", 401},
	} {
		r := httptest.NewRequest(tt.method, "http://"+tt.host+"/", nil)
		r.Host = tt.host
		r.RemoteAddr = tt.remote
		r.Header.Set("Origin", tt.origin)
		r.Header.Set("Sec-Fetch-Mode", tt.fetchMode)
		r.Header.Set("Sec-Fetch-Dest", tt.fetchDest)
		r.Header.Set("Sec-Fetch-Site", tt.fetchSite)
		r.AddCookie(&http.Cookie{Name: "diskord_session", Value: tt.cookie})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("%+v => %d", tt, w.Code)
		}
	}
}
func TestSessionLoginAndLanguage(t *testing.T) {
	u := fixture(t)
	r := httptest.NewRequest("GET", "http://127.0.0.1:3900/api/session", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.8")
	w := httptest.NewRecorder()
	u.sessionInfo(w, r)
	var session map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session["authenticated"] != false || session["locale"] != "en" || session["csrf"] != "csrf" {
		t.Fatal(session)
	}
	for _, tt := range []struct {
		body, csrf string
		status     int
	}{{`{"token":"wrong"}`, "csrf", 401}, {`{"token":"correct"}`, "wrong", 403}, {`{"token":"correct"}`, "csrf", 200}} {
		r = httptest.NewRequest("POST", "/api/login", strings.NewReader(tt.body))
		r.Header.Set("X-CSRF-Token", tt.csrf)
		w = httptest.NewRecorder()
		u.login(w, r)
		if w.Code != tt.status {
			t.Fatalf("login %s => %d", tt.body, w.Code)
		}
		if tt.status == 200 && !strings.Contains(w.Header().Get("Set-Cookie"), "diskord_session=") {
			t.Fatal("session cookie missing")
		}
	}
	r = httptest.NewRequest("POST", "/api/language", strings.NewReader(`{"lang":"en"}`))
	r.Header.Set("X-CSRF-Token", "csrf")
	w = httptest.NewRecorder()
	u.language(w, r)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Set-Cookie"), "diskord_lang=en") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestJSONMessageEscaping(t *testing.T) {
	var b bytes.Buffer
	writeJSON(httptest.NewRecorder(), 200, map[string]string{"content": "<script>alert(1)</script>"})
	if err := json.NewEncoder(&b).Encode(map[string]string{"content": "<script>alert(1)</script>"}); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b.Bytes(), []byte("<script>")) {
		t.Fatal("JSON encoder did not escape HTML")
	}
}
func TestEmbeddedConsole(t *testing.T) {
	u := fixture(t)
	w := httptest.NewRecorder()
	u.index(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/ui/assets/app.js") {
		t.Fatal("built frontend missing from index")
	}
}
