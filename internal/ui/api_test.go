package ui

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/store"
)

func TestCloseCancelsActiveArchiveRequests(t *testing.T) {
	base := fixture(t)
	reservation, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reservation.Addr().String()
	reservation.Close()
	path := filepath.Join(base.Manager.Root, "diskord.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), "127.0.0.1:3900", address, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	manager, err := config.New(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := New(manager, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	started, canceled := make(chan struct{}), make(chan struct{})
	u.server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	served := make(chan error, 1)
	go func() { served <- u.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = u.Close(ctx)
		<-served
	})
	requested := make(chan error, 1)
	go func() {
		client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}, Timeout: 5 * time.Second}
		response, err := client.Get("http://" + address + "/api/v1/export")
		if response != nil {
			response.Body.Close()
		}
		requested <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := u.Close(ctx); err != nil {
		t.Fatal("shutdown did not release active request", err)
	}
	select {
	case <-canceled:
	default:
		t.Fatal("shutdown did not cancel request context")
	}
	if err := <-requested; err != nil {
		t.Fatal(err)
	}
}

func apiFixture(t *testing.T) (*UI, http.Handler) {
	t.Helper()
	u := fixture(t)
	db, err := store.Open(u.Manager.Root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	u.Store = db
	u.apiToken = strings.Repeat("a", 64)
	u.exportSlot = make(chan struct{}, 1)
	if err := db.Apply(context.Background(), model.Batch{Source: "ws", Channels: []model.Channel{{ID: "2", GuildID: model.Ptr("1"), Type: model.Ptr(11), Name: model.Ptr("Thread")}},
		Messages: []model.Message{{ID: "10", ChannelID: model.Ptr("2"), Content: model.Ptr("hello"), Timestamp: model.Ptr("2026-10-01T00:00:00Z")}}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	u.archiveRoutes(mux)
	mux.Handle("GET /api/export", u.auth(http.HandlerFunc(u.archiveExport)))
	mux.Handle("POST /api/settings", u.auth(http.HandlerFunc(u.saveSettings)))
	return u, u.security(mux)
}

func TestArchiveAPIBoundariesAndQueries(t *testing.T) {
	u, handler := apiFixture(t)
	for _, tt := range []struct {
		method, path, token, host, origin string
		status                            int
	}{
		{"GET", "/api/v1/status", u.apiToken, "127.0.0.1:3900", "", 200},
		{"GET", "/api/v1/status", u.token, "127.0.0.1:3900", "", 401},
		{"GET", "/api/v1/status?token=" + u.apiToken, "", "127.0.0.1:3900", "", 401},
		{"GET", "/api/v1/status", u.apiToken, "evil.test:3900", "", 403},
		{"GET", "/api/v1/status", u.apiToken, "127.0.0.1:3900", "https://evil.test", 403},
		{"POST", "/api/v1/messages", u.apiToken, "127.0.0.1:3900", "", 405},
		{"POST", "/api/settings", u.apiToken, "127.0.0.1:3900", "", 401},
		{"GET", "/api/v1/messages?thread=2&limit=1", u.apiToken, "127.0.0.1:3900", "", 200},
		{"GET", "/api/v1/messages?thread=2&since=2026-10-01", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/messages?limit=1001", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/messages?after=../10", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/messages?thread=2&thread=3", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/export?format=html", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/export?attachments=perhaps", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/export?raw=true", u.apiToken, "127.0.0.1:3900", "", 400},
		{"GET", "/api/v1/channels?guild=invalid", u.apiToken, "127.0.0.1:3900", "", 400},
	} {
		r := httptest.NewRequest(tt.method, "http://"+tt.host+tt.path, nil)
		r.RemoteAddr = "127.0.0.1:4567"
		r.Header.Set("Authorization", "Bearer "+tt.token)
		r.Header.Set("Origin", tt.origin)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatalf("%+v: %d %s", tt, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:3900/api/v1/channels", nil)
	r.RemoteAddr = "127.0.0.1:4567"
	r.Header.Set("Authorization", "Bearer "+u.apiToken)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var body struct {
		Channels []ArchiveChannel `json:"channels"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Channels) != 1 || body.Channels[0].ID != "2" {
		t.Fatal(body, err)
	}
}

func TestArchiveAPIExportAndAdmission(t *testing.T) {
	u, handler := apiFixture(t)
	for _, format := range []string{"md", "jsonl"} {
		for _, attachments := range []bool{false, true} {
			path := "/api/v1/export?thread=2&format=" + format
			if attachments {
				path += "&attachments=true"
			}
			r := httptest.NewRequest("GET", "http://127.0.0.1:3900"+path, nil)
			r.RemoteAddr = "127.0.0.1:4567"
			r.Header.Set("Authorization", "Bearer "+u.apiToken)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
				t.Fatal(w.Code, w.Header(), w.Body.String())
			}
			if attachments {
				if _, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len())); err != nil {
					t.Fatal(err)
				}
			} else if !strings.Contains(w.Body.String(), "hello") {
				t.Fatal(w.Body.String())
			}
		}
	}
	u.exportSlot <- struct{}{}
	r := httptest.NewRequest("GET", "/api/v1/export", nil)
	w := httptest.NewRecorder()
	u.archiveExport(w, r)
	<-u.exportSlot
	if w.Code != 429 {
		t.Fatal(w.Code)
	}
	files, err := os.ReadDir(filepath.Join(u.Manager.Root, "tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("export staging leaked", files, err)
	}
}

func TestReadOnlyTokenLifecycle(t *testing.T) {
	u := fixture(t)
	if _, err := ReadAPIToken(u.Manager.Root); err == nil {
		t.Fatal("missing token accepted")
	}
	path := filepath.Join(u.Manager.Root, "state", "api-token")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read command created token")
	}
	token, err := ensureAPIToken(u.Manager.Root)
	if err != nil {
		t.Fatal(err)
	}
	if token == u.token {
		t.Fatal("read-only token reused admin token")
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadAPIToken(u.Manager.Root)
	if err != nil || got != token {
		t.Fatal(got, err)
	}
	after, err := os.Stat(path)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("token read changed file", err)
	}
}

func TestExportAlwaysDeliversOneCompleteSnapshot(t *testing.T) {
	u, handler := apiFixture(t)
	for _, headers := range []map[string]string{
		{"Range": "bytes=0-10"},
		{"If-Modified-Since": "Wed, 01 Jan 2099 00:00:00 GMT"},
		{"If-Match": `"older-export"`},
	} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:3900/api/v1/export?format=jsonl", nil)
		r.RemoteAddr = "127.0.0.1:4567"
		r.Header.Set("Authorization", "Bearer "+u.apiToken)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"type":"summary"`) || w.Header().Get("Content-Range") != "" {
			t.Fatalf("incomplete snapshot for headers %v: %d %s", headers, w.Code, w.Body.String())
		}
	}
}

type blockedExportResponse struct {
	header               http.Header
	started, interrupted chan struct{}
	startOnce, stopOnce  sync.Once
}

func (w *blockedExportResponse) Header() http.Header { return w.header }
func (w *blockedExportResponse) WriteHeader(int)     {}
func (w *blockedExportResponse) Write([]byte) (int, error) {
	w.startOnce.Do(func() { close(w.started) })
	<-w.interrupted
	return 0, os.ErrDeadlineExceeded
}
func (w *blockedExportResponse) SetWriteDeadline(deadline time.Time) error {
	if !deadline.After(time.Now()) {
		w.stopOnce.Do(func() { close(w.interrupted) })
	}
	return nil
}

func TestCanceledExportInterruptsDeliveryAndCleansStaging(t *testing.T) {
	u, _ := apiFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/api/v1/export", nil).WithContext(ctx)
	w := &blockedExportResponse{header: http.Header{}, started: make(chan struct{}), interrupted: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		u.archiveExport(w, r)
		close(done)
	}()
	select {
	case <-w.started:
	case <-time.After(2 * time.Second):
		t.Fatal("export delivery did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled delivery remained blocked")
	}
	files, err := os.ReadDir(filepath.Join(u.Manager.Root, "tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("canceled export leaked staging", files, err)
	}
}

func TestConsoleExportAndSharedNavigation(t *testing.T) {
	u, handler := apiFixture(t)
	for _, tt := range []struct {
		path    string
		session bool
		token   string
		status  int
	}{
		{"/api/export?thread=2&format=jsonl", true, "", 200},
		{"/api/export?thread=2", false, u.apiToken, 401},
		{"/api/v1/export?thread=2", true, "", 401},
		{"/api/export?guild=1&scope=dm", true, "", 400},
		{"/api/export?include_threads=true", true, "", 400},
		{"/api/export?thread=2&include_threads=maybe", true, "", 400},
		{"/api/v1/navigation", false, u.apiToken, 200},
		{"/api/v1/messages?guild=1", false, u.apiToken, 200},
		{"/api/v1/messages?scope=dm", false, u.apiToken, 200},
	} {
		r := httptest.NewRequest("GET", "http://127.0.0.1:3900"+tt.path, nil)
		r.RemoteAddr = "127.0.0.1:4567"
		if tt.session {
			r.AddCookie(&http.Cookie{Name: "diskord_session", Value: u.session})
		}
		r.Header.Set("Authorization", "Bearer "+tt.token)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tt.status {
			t.Fatal(tt, w.Code, w.Body.String())
		}
		if w.Code == 200 && strings.Contains(tt.path, "scope=dm") && strings.Contains(w.Body.String(), "hello") {
			t.Fatal("DM scope included a guild message")
		}
	}
}
