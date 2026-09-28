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
		{"POST", "127.0.0.1:3900", "null", "127.0.0.1:4567", "session", "navigate", "document", "cross-site", 403},
		{"POST", "127.0.0.1:3900", "null", "127.0.0.1:4567", "session", "navigate", "document", "", 403},
		{"GET", "127.0.0.1:3900", "", "127.0.0.1:4567", "session", "cors", "empty", "cross-site", 403},
		{"GET", "127.0.0.1:3900", "", "192.0.2.1:4567", "session", "", "", "", 403},
		{"GET", "127.0.0.1:3900", "", "127.0.0.1:4567", "", "", "", "", 303},
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
	u.token = "correct"
	login := u.security(http.HandlerFunc(u.login))
	for _, tt := range []struct {
		form   string
		status int
	}{
		{"csrf=csrf&token=wrong", http.StatusUnauthorized},
		{"csrf=csrf&token=correct", http.StatusSeeOther},
		{"csrf=wrong&token=correct", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:3900/login", strings.NewReader(tt.form))
		r.RemoteAddr = "127.0.0.1:4567"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "null")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		login.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("opaque-origin login %q => %d, want %d", tt.form, w.Code, tt.status)
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

func TestLocalesAndArchiveNavigation(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/messages", nil)
	r.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if requestLocale(r) != English {
		t.Fatal("English language detection failed")
	}
	r.AddCookie(&http.Cookie{Name: "diskord_lang", Value: "zh-CN"})
	if requestLocale(r) != Chinese {
		t.Fatal("language preference did not override header")
	}
	v := View{Page: "messages", Locale: English, SelectedGuild: "1", SelectedChannel: "3", Filter: model.Filter{GuildID: "1", ChannelID: "3"}, Guilds: []model.GuildRow{{ID: "1", Name: "Server"}}, Channels: []model.ChannelRow{{ID: "2", Name: "Category", Kind: 4}, {ID: "3", ParentID: "2", Name: "General"}}}
	var b bytes.Buffer
	if err := Page(v).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	for _, want := range []string{`lang="en"`, "All channels", "Category", "General", "/messages?guild=1&amp;channel=3"} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in English archive", want)
		}
	}
	if strings.Contains(html, "尚未观察到") {
		t.Fatal("untranslated archive text")
	}
}

func TestLanguagePreferenceRequiresCSRFAndSafeRedirect(t *testing.T) {
	u := &UI{csrf: "secret"}
	for _, tt := range []struct {
		form     string
		status   int
		location string
	}{
		{"csrf=secret&lang=en&next=%2Fmessages", 303, "/messages"},
		{"csrf=secret&lang=en&next=https%3A%2F%2Fevil.test", 303, "/"},
		{"csrf=wrong&lang=en&next=%2Fmessages", 403, ""},
		{"csrf=secret&lang=fr&next=%2Fmessages", 400, ""},
	} {
		r := httptest.NewRequest(http.MethodPost, "/language", strings.NewReader(tt.form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		u.language(w, r)
		if w.Code != tt.status || w.Header().Get("Location") != tt.location {
			t.Fatalf("%q: status %d, location %q", tt.form, w.Code, w.Header().Get("Location"))
		}
		if tt.location == "/messages" && !strings.Contains(w.Header().Get("Set-Cookie"), "diskord_lang=en") {
			t.Fatal("language cookie missing")
		}
	}
}
