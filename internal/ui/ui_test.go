package ui

import (
	"bytes"
	"context"
	"diskord/internal/config"
	"diskord/internal/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalConsoleBoundaries(t *testing.T) {
	root := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP", "SQLITE_TMPDIR", "RANDFILE", "SSLKEYLOGFILE", "SSLKEYLOG_FILE"} {
		t.Setenv(k, os.Getenv(k))
	}
	path := filepath.Join(root, "diskord.yaml")
	body := strings.Replace(config.Example, "runtime_dir: .", "runtime_dir: "+filepath.ToSlash(root), 1)
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	m, e := config.New(path)
	if e != nil {
		t.Fatal(e)
	}
	u := &UI{Manager: m, session: "session", csrf: "csrf"}
	handler := u.security(u.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })))
	for _, tt := range []struct {
		host, origin, remote, cookie string
		status                       int
	}{
		{"127.0.0.1:3900", "", "127.0.0.1:4567", "session", 204},
		{"localhost:3900", "http://localhost:3900", "127.0.0.1:4567", "session", 204},
		{"evil.test:3900", "", "127.0.0.1:4567", "session", 403},
		{"127.0.0.1:3900", "https://evil.test", "127.0.0.1:4567", "session", 403},
		{"127.0.0.1:3900", "", "192.0.2.1:4567", "session", 403},
		{"127.0.0.1:3900", "", "127.0.0.1:4567", "", 303},
	} {
		r := httptest.NewRequest("GET", "http://"+tt.host+"/", nil)
		r.Host = tt.host
		r.RemoteAddr = tt.remote
		r.Header.Set("Origin", tt.origin)
		r.AddCookie(&http.Cookie{Name: "diskord_session", Value: tt.cookie})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("%+v => %d", tt, w.Code)
		}
	}
}
func TestMessageHTMLIsEscaped(t *testing.T) {
	var b bytes.Buffer
	v := View{Rows: []model.MessageRow{{ID: "1", Content: `<script>alert("x")</script>`, AuthorName: `<img src=x onerror=alert(1)>`}}}
	if e := MessageList(v).Render(context.Background(), &b); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(b.String(), "<script>") || strings.Contains(b.String(), "<img src=x") {
		t.Fatal("stored XSS")
	}
	if !strings.Contains(b.String(), "&lt;script&gt;") {
		t.Fatal("missing escaped message")
	}
}
