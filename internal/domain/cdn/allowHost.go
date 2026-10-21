package cdn

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// AllowedHost reports whether hostport ("host" or "host:port")
// is localhost or a literal IP address with an optional port.
//
// Accepted:
//
//	localhost
//	localhost:8080
//	127.0.0.1:9000
//	192.168.1.10
//	[::1]:8080
//	::1                       (raw v6 without brackets is also accepted)
func AllowedHost(hostport string) bool {
	if hostport == "" {
		return false
	}

	host, port, err := splitHostPortLoose(hostport)
	if err != nil {
		return false
	}
	if port != "" && !validPort(port) {
		return false
	}

	// Localhost (case-insensitive). Reject "localhost.evil.com".
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// Optional: allow *.localhost subdomains per RFC 6761.
	if strings.HasSuffix(strings.ToLower(host), ".localhost") &&
		!strings.Contains(strings.TrimSuffix(strings.ToLower(host), ".localhost"), ".") {
		return true
	}

	// IPv4 or IPv6 literal.
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	return false
}

// AllowedOrigin validates an Origin / URL string like
// "http://localhost:3000" or "https://192.168.1.10:8443".
// Only http/https schemes are accepted.
func AllowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if s := strings.ToLower(u.Scheme); s != "http" && s != "https" {
		return false
	}
	return AllowedHost(u.Host)
}

// splitHostPortLoose splits "host", "host:port", "[v6]:port",
// or raw "v6" without brackets.
func splitHostPortLoose(s string) (host, port string, err error) {
	if h, p, e := net.SplitHostPort(s); e == nil {
		return h, p, nil
	}
	// No port supplied.
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"), "", nil
	}
	// Raw IPv6 without brackets/port, e.g. "::1".
	if strings.Count(s, ":") > 1 {
		if _, e := netip.ParseAddr(s); e == nil {
			return s, "", nil
		}
		return "", "", errors.New("hostallow: invalid ipv6")
	}
	// Bare host, bare IPv4, or malformed like "host:port:extra".
	if strings.Contains(s, ":") {
		return "", "", errors.New("hostallow: malformed host")
	}
	return s, "", nil
}

func validPort(p string) bool {
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535
}
