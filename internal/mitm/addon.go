package mitm

import (
	"io"
	"net/http"
	"strings"

	"diskord/internal/capture"
	"diskord/internal/scope"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
)

type Addon struct {
	proxy.BaseAddon
	Health string
	Engine *capture.Engine
	slots  chan struct{}
}

func NewAddon(e *capture.Engine) *Addon       { return &Addon{Engine: e, slots: make(chan struct{}, 8)} }
func (a *Addon) Requestheaders(f *proxy.Flow) { f.Stream = true } // Never buffer any request body.
func (a *Addon) StreamResponseModifier(f *proxy.Flow, in io.Reader) io.Reader {
	if f.Request == nil || f.Request.URL == nil || f.Response == nil || f.Response.StatusCode < 200 || f.Response.StatusCode >= 300 {
		return in
	}
	host := scope.Host(f.Request.URL.Host)
	path := f.Request.URL.EscapedPath()
	mime := f.Response.Header.Get("Content-Type")
	encoding := f.Response.Header.Get("Content-Encoding")
	c := a.Engine.Config()
	resource := scope.CDN(host) && c.Resources.Enabled && scope.ImageExtension(mime) != ""
	api := scope.API(host) && scope.Endpoint(path) != "" && strings.HasPrefix(strings.ToLower(mime), "application/json")
	if !resource && !api {
		return in
	}
	if api && f.Request.Method != "GET" && f.Request.Method != "POST" && f.Request.Method != "PATCH" {
		return in
	}
	limit := c.Capture.MaxHTTPBytes
	if resource {
		limit = c.Resources.MaxBytes
	}
	select {
	case a.slots <- struct{}{}:
	default:
		a.Engine.Metrics.HTTPDropped.Add(1)
		return in
	}
	// Done is closed even when the downstream client aborts mid-response.
	done := f.Done()
	go func() { <-done; <-a.slots }()
	job := capture.HTTPJob{Host: host, Path: path, Mime: mime, Encoding: encoding, Resource: resource}
	return &tapReader{reader: in, max: limit, complete: func(body []byte) { job.Body = body; a.Engine.HTTP(job) }, drop: func() { a.Engine.Metrics.HTTPDropped.Add(1) }}
}

type tapReader struct {
	reader             io.Reader
	max                int
	data               []byte
	finished, overflow bool
	complete           func([]byte)
	drop               func()
}

func (t *tapReader) Read(p []byte) (int, error) {
	n, e := t.reader.Read(p)
	if !t.finished && !t.overflow && n > 0 {
		if len(t.data)+n > t.max {
			t.data = nil
			t.overflow = true
			t.drop()
		} else {
			t.data = append(t.data, p[:n]...)
		}
	}
	if e != nil && !t.finished {
		t.finished = true
		if e == io.EOF && !t.overflow {
			t.complete(t.data)
		} else if !t.overflow {
			t.drop()
		}
		t.data = nil
	}
	return n, e
}

func (a *Addon) AccessProxyServer(r *http.Request, w http.ResponseWriter) {
	if a.Health != "" && r.URL.Path == "/.diskord-health/"+a.Health {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, a.Health)
		return
	}
	http.NotFound(w, r)
}
