// Package scope contains explicit, auditable interception boundaries.
package scope

import (
	"net"
	"net/url"
	"regexp"
	"strings"
)

var resumeGateway = regexp.MustCompile(`^gateway(?:-[a-z0-9]+(?:-[a-z0-9]+)*)?\.discord\.gg$`)
var apiPrefix = regexp.MustCompile(`^/api/(?:v[0-9]+/)?`)
var entityPath = regexp.MustCompile(`^(?:users/(?:@me|[0-9]+)(?:/(?:guilds|channels))?|guilds/[0-9]+(?:/(?:channels|messages/search))?|channels/[0-9]+(?:/messages(?:/(?:[0-9]+|search))?)?)$`)

func Host(s string) string {
	if h, _, e := net.SplitHostPort(s); e == nil {
		s = h
	}
	return strings.ToLower(strings.TrimSuffix(s, "."))
}
func API(host string) bool {
	switch Host(host) {
	case "discord.com", "canary.discord.com", "ptb.discord.com", "discordapp.com":
		return true
	}
	return false
}
func Gateway(host string) bool { return resumeGateway.MatchString(Host(host)) }
func CDN(host string) bool {
	switch Host(host) {
	case "cdn.discordapp.com", "media.discordapp.net":
		return true
	}
	return false
}
func Endpoint(path string) string {
	prefix := apiPrefix.FindString(path)
	if prefix == "" {
		return ""
	}
	p := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/")
	if !entityPath.MatchString(p) {
		return ""
	}
	return p
}
func AssetURL(raw string) (host, path string, ok bool) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || !CDN(u.Host) || (u.Port() != "" && u.Port() != "443") {
		return "", "", false
	}
	// Never retain signed URL query strings, fragments, credentials, or arbitrary hosts.
	return Host(u.Host), u.EscapedPath(), true
}
func ImageExtension(mime string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	}
	return ""
}
