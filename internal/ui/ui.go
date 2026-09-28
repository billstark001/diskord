package ui

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"diskord/internal/ca"
	"diskord/internal/capture"
	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/securefs"
	"diskord/internal/store"
)

//go:embed webdist
var frontend embed.FS

func Token(root string) (string, error) {
	p, err := securefs.Within(root, "state/ui-token")
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err == nil {
		if err = securefs.Private(p); err != nil {
			return "", err
		}
		value := strings.TrimSpace(string(data))
		if len(value) != 64 {
			return "", errors.New("invalid ui-token file")
		}
		return value, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	token, err := securefs.Random(32)
	if err != nil {
		return "", err
	}
	file, err := securefs.NewFile(p)
	if err != nil {
		return "", err
	}
	if _, err = file.WriteString(token + "\n"); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return token, nil
}

type UI struct {
	Manager              *config.Manager
	Authority            *ca.Live
	Engine               *capture.Engine
	Store                *store.Store
	server               *http.Server
	listener             net.Listener
	token, session, csrf string
	changeMu, loginMu    sync.Mutex
	loginStart           time.Time
	loginCount           int
}

type BackgroundControl struct {
	Token string
	Stop  context.CancelFunc
}

func New(m *config.Manager, a *ca.Live, e *capture.Engine, s *store.Store, background ...BackgroundControl) (*UI, error) {
	if _, err := fs.Stat(frontend, "webdist/index.html"); err != nil {
		return nil, errors.New("embedded console missing; build with scripts/build.sh or scripts/build.ps1")
	}
	token, err := Token(m.Root)
	if err != nil {
		return nil, err
	}
	session, err := securefs.Random(32)
	if err != nil {
		return nil, err
	}
	csrf, err := securefs.Random(32)
	if err != nil {
		return nil, err
	}
	u := &UI{Manager: m, Authority: a, Engine: e, Store: s, token: token, session: session, csrf: csrf}
	mux := http.NewServeMux()
	web, err := fs.Sub(frontend, "webdist")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServer(http.FS(web))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	if len(background) > 0 && background[0].Token != "" && background[0].Stop != nil {
		control := background[0]
		verify := func(w http.ResponseWriter, r *http.Request) bool {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Diskord-Bg-Token")), []byte(control.Token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return false
			}
			return true
		}
		mux.HandleFunc("GET /internal/bg-status", func(w http.ResponseWriter, r *http.Request) {
			if !verify(w, r) {
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, strconv.Itoa(os.Getpid()))
		})
		mux.HandleFunc("POST /internal/bg-stop", func(w http.ResponseWriter, r *http.Request) {
			if !verify(w, r) {
				return
			}
			w.WriteHeader(http.StatusAccepted)
			go control.Stop()
		})
	}
	for _, route := range []string{"GET /{$}", "GET /messages", "GET /settings"} {
		mux.HandleFunc(route, u.index)
	}
	mux.HandleFunc("GET /api/session", u.sessionInfo)
	mux.HandleFunc("POST /api/login", u.login)
	mux.HandleFunc("POST /api/language", u.language)
	mux.Handle("POST /api/logout", u.auth(http.HandlerFunc(u.logout)))
	mux.Handle("GET /api/overview", u.auth(http.HandlerFunc(u.overview)))
	mux.Handle("GET /api/navigation", u.auth(http.HandlerFunc(u.navigation)))
	mux.Handle("GET /api/messages", u.auth(http.HandlerFunc(u.messages)))
	mux.Handle("GET /api/settings", u.auth(http.HandlerFunc(u.settings)))
	mux.Handle("POST /api/settings", u.auth(http.HandlerFunc(u.saveSettings)))
	mux.Handle("POST /api/ca/issue", u.auth(http.HandlerFunc(u.issueCA)))
	mux.Handle("POST /api/ca/select", u.auth(http.HandlerFunc(u.selectCA)))
	mux.Handle("GET /assets/{hash}", u.auth(http.HandlerFunc(u.asset)))
	ln, err := net.Listen("tcp", m.Current().Web.Listen)
	if err != nil {
		return nil, err
	}
	u.listener = ln
	u.server = &http.Server{Handler: u.security(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, ErrorLog: log.New(log.Writer(), "http: ", 0)}
	return u, nil
}
func (u *UI) Serve() error                    { return u.server.Serve(u.listener) }
func (u *UI) Close(ctx context.Context) error { _ = u.listener.Close(); return u.server.Shutdown(ctx) }

func (u *UI) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		_, port, _ := net.SplitHostPort(u.Manager.Current().Web.Listen)
		if r.Host != u.Manager.Current().Web.Listen && r.Host != net.JoinHostPort("localhost", port) {
			http.Error(w, "untrusted Host", http.StatusForbidden)
			return
		}
		navigation := r.Method == http.MethodGet && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
		origin := r.Header.Get("Origin")
		opaqueSameOrigin := origin == "null" && r.Header.Get("Sec-Fetch-Site") == "same-origin"
		if !navigation && !opaqueSameOrigin && origin != "" && origin != "http://"+r.Host {
			http.Error(w, "cross-origin request denied", http.StatusForbidden)
			return
		}
		if !navigation && r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "cross-site request denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'; object-src 'none'")
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		next.ServeHTTP(w, r)
	})
}
func (u *UI) authorized(r *http.Request) bool {
	cookie, err := r.Cookie("diskord_session")
	return err == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(u.session)) == 1
}
func (u *UI) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !u.authorized(r) {
			problem(w, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (u *UI) csrfOK(w http.ResponseWriter, r *http.Request) bool {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(u.csrf)) != 1 {
		problem(w, http.StatusForbidden, "invalid CSRF token; reload the page")
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("exactly one JSON object is required")
	}
	return nil
}
func (u *UI) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", string(requestLocale(r)))
	content, _ := frontend.ReadFile("webdist/index.html")
	_, _ = w.Write(content)
}
func (u *UI) sessionInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": u.authorized(r), "csrf": u.csrf, "locale": requestLocale(r)})
}
func (u *UI) login(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		problem(w, 400, "invalid login request")
		return
	}
	u.loginMu.Lock()
	if time.Since(u.loginStart) > time.Minute {
		u.loginStart = time.Now()
		u.loginCount = 0
	}
	u.loginCount++
	limited := u.loginCount > 10
	u.loginMu.Unlock()
	if limited {
		problem(w, 429, "too many login attempts; retry later")
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Token), []byte(u.token)) != 1 {
		problem(w, 401, "incorrect access token")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "diskord_session", Value: u.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	writeJSON(w, 200, map[string]bool{"authenticated": true})
}
func (u *UI) language(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	var body struct {
		Lang string `json:"lang"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Lang != string(Chinese) && body.Lang != string(English) {
		problem(w, 400, "invalid language")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "diskord_lang", Value: body.Lang, Path: "/", SameSite: http.SameSiteStrictMode, MaxAge: 365 * 24 * 3600})
	writeJSON(w, 200, map[string]string{"locale": body.Lang})
}
func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "diskord_session", Path: "/", Value: "", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	writeJSON(w, 200, map[string]bool{"authenticated": false})
}
func (u *UI) overview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	counts, err := u.Store.Counts(ctx)
	if err != nil {
		problem(w, 503, "database unavailable")
		return
	}
	c := u.Manager.Current()
	writeJSON(w, 200, map[string]any{"counts": counts, "stats": u.Engine.Metrics.Snapshot(), "proxy": c.Proxy.Listen, "web": c.Web.Listen, "runtime": u.Manager.Root, "resourcesEnabled": c.Resources.Enabled})
}
func (u *UI) navigation(w http.ResponseWriter, r *http.Request) {
	guildID, scope := r.URL.Query().Get("guild"), r.URL.Query().Get("scope")
	if guildID != "" && !model.ID(guildID) || scope != "" && scope != "dm" {
		problem(w, 400, "invalid navigation filter")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	guilds, err := u.Store.Guilds(ctx)
	if err != nil {
		problem(w, 503, "navigation unavailable")
		return
	}
	channels := []model.ChannelRow{}
	if guildID != "" || scope == "dm" {
		channels, err = u.Store.Channels(ctx, guildID)
		if err != nil {
			problem(w, 503, "navigation unavailable")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"guilds": guilds, "channels": channels})
}
func (u *UI) messages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := model.Filter{Query: q.Get("q"), GuildID: q.Get("guild"), ChannelID: q.Get("channel"), Before: q.Get("before"), Scope: q.Get("scope"), Limit: 50}
	if filter.Scope != "" && filter.Scope != "dm" || len(filter.Query) > 256 {
		problem(w, 400, "invalid message filter")
		return
	}
	for _, id := range []string{filter.GuildID, filter.ChannelID, filter.Before} {
		if id != "" && !model.ID(id) {
			problem(w, 400, "invalid entity ID")
			return
		}
	}
	if filter.GuildID != "" {
		filter.Scope = ""
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := u.Store.Messages(ctx, filter)
	if err != nil {
		problem(w, 503, "message query unavailable")
		return
	}
	next := ""
	if len(rows) == filter.Limit {
		next = rows[len(rows)-1].ID
	}
	writeJSON(w, 200, map[string]any{"rows": rows, "next": next})
}
func (u *UI) settings(w http.ResponseWriter, r *http.Request) { u.settingsResult(w, r) }
func (u *UI) settingsResult(w http.ResponseWriter, r *http.Request) {
	c := u.Manager.Current()
	a := u.Authority.Current()
	trustNotice := ""
	if !a.TrustedForTLS() {
		trustNotice = ca.TrustInstructions()
		if requestLocale(r) == English {
			trustNotice = ca.TrustInstructionsEnglish()
		}
	}
	files := []string{}
	if entries, err := os.ReadDir(filepath.Join(u.Manager.Root, "ca")); err == nil {
		for _, item := range entries {
			if item.Type().IsRegular() && strings.HasSuffix(item.Name(), ".pem") {
				files = append(files, "ca/"+item.Name())
			}
		}
	}
	writeJSON(w, 200, map[string]any{"revision": u.Manager.Revision(), "resourcesEnabled": c.Resources.Enabled, "cert": c.CA.Cert, "key": c.CA.Key, "fingerprint": a.Fingerprint(), "expires": a.Root.NotAfter.Format(time.RFC3339), "trustNotice": trustNotice, "caFiles": files, "fileLoggerEnabled": c.Logging.File.Enabled, "discordLoggerEnabled": c.Logging.Discord.Enabled})
}
func (u *UI) saveSettings(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	var body struct {
		Revision         string `json:"revision"`
		ResourcesEnabled bool   `json:"resourcesEnabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		problem(w, 400, "invalid settings request")
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	if _, err := u.Manager.Patch(body.Revision, map[string]any{"resources.enabled": body.ResourcesEnabled}, nil); err != nil {
		problem(w, 409, err.Error())
		return
	}
	if body.ResourcesEnabled {
		u.Engine.StartBackfill()
	}
	writeJSON(w, 200, map[string]bool{"saved": true})
}
func (u *UI) issueCA(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	var body struct {
		Cert string `json:"cert"`
		Key  string `json:"key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		problem(w, 400, "invalid CA request")
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	if err := ca.Issue(r.Context(), u.Manager.Root, body.Cert, body.Key, u.Manager.Current().CA.OpenSSL); err != nil {
		problem(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"issued": true})
}
func (u *UI) selectCA(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	var body struct {
		Revision string `json:"revision"`
		Cert     string `json:"cert"`
		Key      string `json:"key"`
	}
	if err := decodeJSON(r, &body); err != nil {
		problem(w, 400, "invalid CA request")
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	var selected *ca.Authority
	_, err := u.Manager.Patch(body.Revision, map[string]any{"ca.cert": body.Cert, "ca.key": body.Key}, func(c config.Config) error {
		var err error
		selected, err = ca.Load(u.Manager.Root, c.CA.Cert, c.CA.Key)
		return err
	})
	if err != nil {
		problem(w, 409, err.Error())
		return
	}
	u.Authority.Swap(selected)
	writeJSON(w, 200, map[string]bool{"selected": true})
}
func (u *UI) asset(w http.ResponseWriter, r *http.Request) {
	if !u.Manager.Current().Resources.Enabled {
		http.NotFound(w, r)
		return
	}
	path, mime, err := u.Store.Asset(r.Context(), r.PathValue("hash"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(filepath.Base(path)))
	http.ServeFile(w, r, path)
}
