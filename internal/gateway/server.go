// Package gateway is the public loopback ingress and bounded WebSocket relay.
// Only Discord API/CDN CONNECTs enter go-mitmproxy. Other TLS is an opaque tunnel.
package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"diskord/internal/ca"
	"diskord/internal/capture"
	"diskord/internal/config"
	"diskord/internal/scope"
	"github.com/gorilla/websocket"
)

type Server struct {
	HTTP      *http.Server
	work      sync.WaitGroup
	tracked   *trackedListener
	Backend   string
	Authority *ca.Live
	Engine    *capture.Engine
	Config    func() config.Config
}

func New(addr, backend string, a *ca.Live, e *capture.Engine, current func() config.Config) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{Backend: backend, Authority: a, Engine: e, Config: current, tracked: newTracked(ln, current().Proxy.MaxConnections)}
	s.HTTP = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 64 * 1024, ErrorLog: log.New(io.Discard, "", 0)}
	return s, nil
}
func (s *Server) Serve() error { return s.HTTP.Serve(s.tracked) }
func (s *Server) Close(ctx context.Context) error {
	plainTransport.CloseIdleConnections()
	s.tracked.CloseAll()
	err := s.HTTP.Shutdown(ctx)
	s.tracked.wg.Wait()
	s.work.Wait()
	return err
}
func authority(host string) (string, string, error) {
	h, p, e := net.SplitHostPort(host)
	if e != nil {
		return "", "", errors.New("CONNECT requires host:443")
	}
	h = scope.Host(h)
	if p != "443" || h == "" || len(h) > 253 || strings.ContainsAny(h, "/@\\\r\n\t ") || h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return "", "", errors.New("invalid CONNECT authority")
	}
	if ip := net.ParseIP(h); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		return "", "", errors.New("local CONNECT targets are not allowed")
	}
	return h, net.JoinHostPort(h, p), nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.work.Add(1)
	defer s.work.Done()
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		http.Error(w, "loopback clients only", http.StatusForbidden)
		return
	}
	if r.Method != "CONNECT" {
		s.plain(w, r)
		return
	}
	host, target, err := authority(r.Host)
	if err != nil {
		http.Error(w, "invalid CONNECT target", http.StatusBadRequest)
		return
	}
	if scope.Gateway(host) {
		s.gatewayCONNECT(w, r, host)
		return
	}
	interested := scope.API(host) || (s.Config().Resources.Enabled && scope.CDN(host))
	dialTarget := target
	if interested {
		dialTarget = s.Backend
	}
	back, err := (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext(r.Context(), "tcp", dialTarget)
	if err != nil {
		http.Error(w, "upstream connection failed", http.StatusBadGateway)
		return
	}
	defer back.Close()
	var upstream io.Reader = back
	if interested {
		_ = back.SetDeadline(time.Now().Add(15 * time.Second))
		req := &http.Request{Method: "CONNECT", URL: &url.URL{Opaque: target}, Host: target, Header: make(http.Header)}
		if err = req.Write(back); err != nil {
			http.Error(w, "internal proxy connection failed", http.StatusBadGateway)
			return
		}
		br := bufio.NewReader(back)
		response, err := http.ReadResponse(br, req)
		if err != nil || response.StatusCode != 200 {
			http.Error(w, "internal proxy rejected CONNECT", http.StatusBadGateway)
			return
		}
		upstream = br
		_ = back.SetDeadline(time.Time{})
	}
	client, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = rw.Flush(); err != nil {
		return
	}
	transfer(client, rw.Reader, back, upstream)
}
func transfer(client net.Conn, clientReader io.Reader, server net.Conn, serverReader io.Reader) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(server, clientReader); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, serverReader); done <- struct{}{} }()
	<-done
	client.Close()
	server.Close()
	<-done
}

type bufferedConn struct {
	net.Conn
	reader io.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (s *Server) gatewayCONNECT(w http.ResponseWriter, r *http.Request, host string) {
	client, rw, err := w.(http.Hijacker).Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	if _, err = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err = rw.Flush(); err != nil {
		return
	}
	conn := tls.Server(&bufferedConn{Conn: client, reader: rw.Reader}, &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}, GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if scope.Host(hello.ServerName) != host {
			return nil, errors.New("gateway SNI differs from CONNECT target")
		}
		return s.Authority.GetCert(host)
	}})
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if err = conn.HandshakeContext(r.Context()); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{})
	one := &oneListener{conn: conn, closed: make(chan struct{})}
	defer one.Close()
	server := &http.Server{ReadHeaderTimeout: 10 * time.Second, MaxHeaderBytes: 64 * 1024, ErrorLog: log.New(io.Discard, "", 0)}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer one.Close(); s.relay(host, w, r) })
	server.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			one.Close()
		}
	}
	server.SetKeepAlivesEnabled(false)
	_ = server.Serve(one)
}
func (s *Server) relay(host string, w http.ResponseWriter, r *http.Request) {
	if scope.Host(r.Host) != host || r.Method != "GET" || r.URL.Path != "/" || !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "Gateway websocket required", http.StatusBadRequest)
		return
	}
	// A fresh upstream handshake intentionally excludes cookies/Authorization.
	// Discord Gateway authentication travels in client frames, forwarded untouched.
	hdr := make(http.Header)
	for _, k := range []string{"Origin", "User-Agent", "Accept-Language"} {
		if v := r.Header.Get(k); v != "" {
			hdr.Set(k, v)
		}
	}
	dialer := websocket.Dialer{Proxy: nil, HandshakeTimeout: 15 * time.Second, EnableCompression: true, Subprotocols: websocket.Subprotocols(r), TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}}
	u := url.URL{Scheme: "wss", Host: host, Path: "/", RawQuery: r.URL.RawQuery}
	server, response, err := dialer.DialContext(r.Context(), u.String(), hdr)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		http.Error(w, "Gateway upstream handshake failed", http.StatusBadGateway)
		return
	}
	defer server.Close()
	protocols := []string{}
	if server.Subprotocol() != "" {
		protocols = append(protocols, server.Subprotocol())
	}
	upgrader := websocket.Upgrader{ReadBufferSize: 32 * 1024, WriteBufferSize: 32 * 1024, EnableCompression: true, Subprotocols: protocols, CheckOrigin: func(*http.Request) bool { return true }}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer client.Close()
	// Limits are for the forwarding connection. Decoded captured objects have a
	// smaller independent limit, and overflow disables observation, not forwarding.
	server.SetReadLimit(128 * 1024 * 1024)
	client.SetReadLimit(128 * 1024 * 1024)
	observer := s.Engine.ObserveGateway(r.URL.Query())
	defer observer.Close()
	s.Engine.Metrics.ActiveGateway.Add(1)
	defer s.Engine.Metrics.ActiveGateway.Add(-1)
	finished := make(chan error, 2)
	go func() { finished <- relayMessages(server, client, nil) }() // C->S: NEVER observed.
	go func() { finished <- relayMessages(client, server, observer) }()
	<-finished
	client.Close()
	server.Close()
	<-finished
}
func relayMessages(dst, src *websocket.Conn, observer *capture.Observer) error {
	buf := make([]byte, 32*1024)
	for {
		kind, reader, err := src.NextReader()
		if err != nil {
			return err
		}
		_ = dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
		writer, err := dst.NextWriter(kind)
		if err != nil {
			return err
		}
		for {
			n, re := reader.Read(buf)
			if n > 0 {
				_ = dst.SetWriteDeadline(time.Now().Add(30 * time.Second))
				written, we := writer.Write(buf[:n])
				if we != nil {
					writer.Close()
					return we
				}
				if written != n {
					writer.Close()
					return io.ErrShortWrite
				}
				observer.Push(buf[:n]) // nil receiver is intentional for the client direction.
			}
			if re != nil {
				if re != io.EOF {
					writer.Close()
					return re
				}
				break
			}
		}
		if err = writer.Close(); err != nil {
			return err
		}
	}
}

type oneListener struct {
	conn   net.Conn
	used   bool
	closed chan struct{}
	once   sync.Once
}

func (l *oneListener) Accept() (net.Conn, error) {
	if !l.used {
		l.used = true
		return l.conn, nil
	}
	<-l.closed
	return nil, net.ErrClosed
}
func (l *oneListener) Close() error {
	l.once.Do(func() { close(l.closed); l.conn.Close() })
	return nil
}
func (l *oneListener) Addr() net.Addr { return l.conn.LocalAddr() }
