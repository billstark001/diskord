package gateway

import (
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Plain HTTP never enters the collector, even when its hostname is Discord.
// Hop-by-hop protocol headers must be handled to forward an HTTP request.
var plainTransport = &http.Transport{Proxy: nil, DisableCompression: true, MaxIdleConns: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 30 * time.Second, DialContext: (&net.Dialer{Timeout: 15 * time.Second}).DialContext}

func hopHeaders(h http.Header) {
	for _, line := range h.Values("Connection") {
		for _, name := range strings.Split(line, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{"Connection", "Proxy-Connection", "Proxy-Authorization", "Proxy-Authenticate", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(name)
	}
}
func (s *Server) plain(w http.ResponseWriter, r *http.Request) {
	if r.URL.Scheme != "http" || r.URL.Host == "" || r.URL.User != nil {
		http.Error(w, "absolute HTTP URL or HTTPS CONNECT required", http.StatusBadRequest)
		return
	}
	host := strings.TrimSuffix(strings.ToLower(r.URL.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		http.Error(w, "local upstream targets are not allowed", http.StatusForbidden)
		return
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast()) {
		http.Error(w, "local upstream targets are not allowed", http.StatusForbidden)
		return
	}
	if r.Header.Get("Upgrade") != "" {
		http.Error(w, "plain ws:// upgrade is not supported; wss:// uses CONNECT", http.StatusNotImplemented)
		return
	}
	clone := r.Clone(r.Context())
	clone.RequestURI = ""
	clone.Header = r.Header.Clone()
	hopHeaders(clone.Header)
	res, e := plainTransport.RoundTrip(clone)
	if e != nil {
		http.Error(w, "upstream HTTP request failed", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	hopHeaders(res.Header)
	for k, values := range res.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
