package mitm

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"diskord/internal/ca"
	"diskord/internal/capture"
	"diskord/internal/scope"
	"diskord/internal/securefs"
	"github.com/lqqyt2423/go-mitmproxy/cert"
	"github.com/lqqyt2423/go-mitmproxy/proxy"
	"github.com/sirupsen/logrus"
)

// Backend is an internal loopback-only go-mitmproxy HTTP transport. Gateway WS
// traffic is routed separately so it cannot enter the library's flow history.
type Backend struct {
	Proxy  *proxy.Proxy
	Addr   string
	Errors chan error
}

func Start(authority *ca.Live, engine *capture.Engine) (*Backend, error) {
	// Do not register LogAddon, upstream web UI, save/dump plugins or key logging.
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.PanicLevel)
	reserve, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return nil, e
	}
	addr := reserve.Addr().String()
	reserve.Close()
	p, e := proxy.NewProxy(&proxy.Options{Addr: addr, StreamLargeBodies: 1, NewCaFunc: func() (cert.CA, error) { return authority, nil }, SslInsecure: false})
	if e != nil {
		return nil, e
	}
	p.SetShouldInterceptRule(func(r *http.Request) bool {
		return scope.API(r.Host) || (engine.Config().Resources.Enabled && scope.CDN(r.Host))
	})
	p.SetUpstreamProxy(func(*http.Request) (*url.URL, error) { return nil, nil }) // Never inherit HTTP_PROXY / HTTPS_PROXY.
	health, e := securefs.Random(24)
	if e != nil {
		p.Close()
		return nil, e
	}
	addon := NewAddon(engine)
	addon.Health = health
	p.AddAddon(addon)
	b := &Backend{Proxy: p, Addr: addr, Errors: make(chan error, 1)}
	go func() { b.Errors <- p.Start() }()
	// The upstream API accepts an address, not an existing listener. A port race
	// fails startup; it must not silently switch to another running service.
	deadline := time.Now().Add(3 * time.Second)
	probe := &http.Client{Timeout: 200 * time.Millisecond, Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	for time.Now().Before(deadline) {
		select {
		case e := <-b.Errors:
			p.Close()
			return nil, e
		default:
		}
		response, err := probe.Get("http://" + addr + "/.diskord-health/" + health)
		if err == nil {
			body, re := io.ReadAll(io.LimitReader(response.Body, 128))
			response.Body.Close()
			if re == nil && response.StatusCode == 200 && string(body) == health {
				return b, nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.Close()
	return nil, errors.New("internal HTTP proxy did not become ready")
}
func (b *Backend) Close(ctx context.Context) error { return b.Proxy.Shutdown(ctx) }
