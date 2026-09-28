package ui

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
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

//go:embed static/*
var assets embed.FS

type View struct {
	Page, CSRF, Revision, Error, Notice, RuntimeDir, Fingerprint, Expires, Next string
	CATrustNotice                                                               string
	Config                                                                      config.Config
	Counts                                                                      model.Counts
	Stats                                                                       capture.Snapshot
	Rows                                                                        []model.MessageRow
	Filter                                                                      model.Filter
	CAFiles                                                                     []string
}

func Num(v any) string { return fmt.Sprint(v) }
func NextURL(f model.Filter, before string) string {
	q := url.Values{}
	q.Set("q", f.Query)
	q.Set("guild", f.GuildID)
	q.Set("channel", f.ChannelID)
	if before != "" {
		q.Set("before", before)
	}
	return "/messages?" + q.Encode()
}
func FragmentURL(f model.Filter) string {
	return strings.Replace(NextURL(f, f.Before), "/messages?", "/messages/fragment?", 1)
}
func Label(b bool) string {
	if b {
		return "开启"
	}
	return "关闭"
}
func Token(root string) (string, error) {
	p, e := securefs.Within(root, "state/ui-token")
	if e != nil {
		return "", e
	}
	data, e := os.ReadFile(p)
	if e == nil {
		if e = securefs.Private(p); e != nil {
			return "", e
		}
		value := strings.TrimSpace(string(data))
		if len(value) != 64 {
			return "", errors.New("invalid ui-token file")
		}
		return value, nil
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	token, e := securefs.Random(32)
	if e != nil {
		return "", e
	}
	f, e := securefs.NewFile(p)
	if e != nil {
		return "", e
	}
	if _, e = f.WriteString(token + "\n"); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
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

func New(m *config.Manager, a *ca.Live, e *capture.Engine, s *store.Store) (*UI, error) {
	if _, err := fs.Stat(assets, "static/htmx.min.js"); err != nil {
		return nil, errors.New("embedded htmx missing; build with scripts/build.sh or scripts/build.ps1")
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
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	mux.HandleFunc("GET /login", u.loginPage)
	mux.HandleFunc("POST /login", u.login)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "ok\n")
	})
	mux.Handle("GET /{$}", u.auth(http.HandlerFunc(u.home)))
	mux.Handle("GET /status", u.auth(http.HandlerFunc(u.status)))
	mux.Handle("GET /messages", u.auth(http.HandlerFunc(u.messages)))
	mux.Handle("GET /messages/fragment", u.auth(http.HandlerFunc(u.messages)))
	mux.Handle("GET /settings", u.auth(http.HandlerFunc(u.settings)))
	mux.Handle("POST /settings", u.auth(http.HandlerFunc(u.saveSettings)))
	mux.Handle("POST /ca/issue", u.auth(http.HandlerFunc(u.issueCA)))
	mux.Handle("POST /ca/select", u.auth(http.HandlerFunc(u.selectCA)))
	mux.Handle("POST /logout", u.auth(http.HandlerFunc(u.logout)))
	mux.Handle("GET /assets/{hash}", u.auth(http.HandlerFunc(u.asset)))
	ln, err := net.Listen("tcp", m.Current().Web.Listen)
	if err != nil {
		return nil, err
	}
	u.listener = ln
	u.server = &http.Server{Handler: u.security(mux), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 75 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024, ErrorLog: log.New(io.Discard, "", 0)}
	return u, nil
}
func (u *UI) Serve() error                    { return u.server.Serve(u.listener) }
func (u *UI) Close(ctx context.Context) error { _ = u.listener.Close(); return u.server.Shutdown(ctx) }
func (u *UI) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil || !net.ParseIP(remote).IsLoopback() {
			http.Error(w, "loopback only", http.StatusForbidden)
			return
		}
		_, port, _ := net.SplitHostPort(u.Manager.Current().Web.Listen)
		if r.Host != u.Manager.Current().Web.Listen && r.Host != net.JoinHostPort("localhost", port) {
			http.Error(w, "untrusted Host", http.StatusForbidden)
			return
		}
		// A link from another site may carry Origin/Fetch Metadata on a
		// top-level navigation. Opening the console is safe; its forms and
		// background requests must still come from the console itself.
		navigation := r.Method == http.MethodGet && r.Header.Get("Sec-Fetch-Mode") == "navigate" && r.Header.Get("Sec-Fetch-Dest") == "document"
		origin := r.Header.Get("Origin")
		// Some browsers send an opaque Origin for a same-origin form POST.
		// Fetch Metadata is set by the browser, and the form still needs CSRF.
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
func (u *UI) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("diskord_session")
		if e != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(u.session)) != 1 {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(401)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (u *UI) csrfOK(w http.ResponseWriter, r *http.Request) bool {
	if e := r.ParseForm(); e != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return false
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(u.csrf)) != 1 {
		http.Error(w, "invalid CSRF token; reload the page", http.StatusForbidden)
		return false
	}
	return true
}
func (u *UI) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = Login(u.csrf, "").Render(r.Context(), w)
}
func (u *UI) login(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
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
		http.Error(w, "too many login attempts; retry later", http.StatusTooManyRequests)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(u.token)) != 1 {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(401)
		_ = Login(u.csrf, "访问令牌不正确。").Render(r.Context(), w)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "diskord_session", Value: u.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 12 * 3600})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "diskord_session", Path: "/", Value: "", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.Header().Set("Clear-Site-Data", `"cache", "cookies", "storage"`)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
func (u *UI) view(page string) View {
	a := u.Authority.Current()
	return View{Page: page, CSRF: u.csrf, Revision: u.Manager.Revision(), RuntimeDir: u.Manager.Root, Config: u.Manager.Current(), Stats: u.Engine.Metrics.Snapshot(), Fingerprint: a.Fingerprint(), Expires: a.Root.NotAfter.Format(time.RFC3339)}
}
func (u *UI) render(w http.ResponseWriter, r *http.Request, v View) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if e := Page(v).Render(r.Context(), w); e != nil {
		return
	}
}
func (u *UI) home(w http.ResponseWriter, r *http.Request) {
	v := u.view("overview")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	counts, e := u.Store.Counts(ctx)
	if e != nil {
		v.Error = "数据库暂时不可读；请检查状态计数与磁盘空间。"
	} else {
		v.Counts = counts
	}
	u.render(w, r, v)
}
func (u *UI) status(w http.ResponseWriter, r *http.Request) {
	v := u.view("overview")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	c, e := u.Store.Counts(ctx)
	if e != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	v.Counts = c
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = Status(v).Render(ctx, w)
}
func (u *UI) messages(w http.ResponseWriter, r *http.Request) {
	v := u.view("messages")
	q := r.URL.Query()
	v.Filter = model.Filter{Query: q.Get("q"), GuildID: q.Get("guild"), ChannelID: q.Get("channel"), Before: q.Get("before"), Limit: 50}
	if len(v.Filter.Query) > 256 {
		http.Error(w, "search query too long", http.StatusBadRequest)
		return
	}
	for _, id := range []string{v.Filter.GuildID, v.Filter.ChannelID, v.Filter.Before} {
		if id != "" && !model.ID(id) {
			http.Error(w, "invalid entity ID", http.StatusBadRequest)
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, e := u.Store.Messages(ctx, v.Filter)
	if e != nil {
		http.Error(w, "message query temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	v.Rows = rows
	if len(rows) == 50 {
		v.Next = NextURL(v.Filter, rows[len(rows)-1].ID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.URL.Path == "/messages/fragment" {
		_ = MessageList(v).Render(ctx, w)
		return
	}
	u.render(w, r, v)
}
func (u *UI) settings(w http.ResponseWriter, r *http.Request) { u.settingsResult(w, r, "", "") }
func (u *UI) settingsResult(w http.ResponseWriter, r *http.Request, notice, problem string) {
	v := u.view("settings")
	v.Notice = notice
	v.Error = problem
	if !u.Authority.Current().TrustedForTLS() {
		v.CATrustNotice = ca.TrustInstructions()
	}
	if entries, e := os.ReadDir(filepath.Join(u.Manager.Root, "ca")); e == nil {
		for _, f := range entries {
			if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".pem") {
				v.CAFiles = append(v.CAFiles, "ca/"+f.Name())
			}
		}
	}
	u.render(w, r, v)
}
func (u *UI) saveSettings(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	_, e := u.Manager.Patch(r.PostForm.Get("revision"), map[string]any{"resources.enabled": r.PostForm.Get("resources") == "on"}, nil)
	if e != nil {
		u.settingsResult(w, r, "", e.Error())
		return
	}
	u.settingsResult(w, r, "设置已保存。资源拦截策略对新连接生效；既有 CDN 连接需要重连。", "")
}
func (u *UI) issueCA(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	e := ca.Issue(r.Context(), u.Manager.Root, r.PostForm.Get("cert"), r.PostForm.Get("key"), u.Manager.Current().CA.OpenSSL)
	if e != nil {
		u.settingsResult(w, r, "", e.Error())
		return
	}
	u.settingsResult(w, r, "CA 已签发但尚未选择。请核验指纹、显式选择，并手动安装公钥证书信任。", "")
}
func (u *UI) selectCA(w http.ResponseWriter, r *http.Request) {
	if !u.csrfOK(w, r) {
		return
	}
	u.changeMu.Lock()
	defer u.changeMu.Unlock()
	var selected *ca.Authority
	_, e := u.Manager.Patch(r.PostForm.Get("revision"), map[string]any{"ca.cert": r.PostForm.Get("cert"), "ca.key": r.PostForm.Get("key")}, func(c config.Config) error {
		var e error
		selected, e = ca.Load(u.Manager.Root, c.CA.Cert, c.CA.Key)
		return e
	})
	if e != nil {
		u.settingsResult(w, r, "", e.Error())
		return
	}
	u.Authority.Swap(selected)
	u.settingsResult(w, r, "证书已校验并选择。新 TLS 握手使用新 CA；已有连接不被中断。", "")
}
func (u *UI) asset(w http.ResponseWriter, r *http.Request) {
	if !u.Manager.Current().Resources.Enabled {
		http.NotFound(w, r)
		return
	}
	path, mime, e := u.Store.Asset(r.Context(), r.PathValue("hash"))
	if e != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline; filename="+strconv.Quote(filepath.Base(path)))
	http.ServeFile(w, r, path)
}
