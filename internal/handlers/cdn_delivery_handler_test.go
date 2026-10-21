package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// StripHostPort
// ---------------------------------------------------------------------------

func TestStripHostPort(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// IPv4
		{"localhost", "localhost"},
		{"localhost:8080", "localhost"},
		{"127.0.0.1", "127.0.0.1"},
		{"127.0.0.1:9000", "127.0.0.1"},
		// IPv6 with brackets + port
		{"[::1]:8080", "::1"},
		// IPv6 with brackets, no port
		{"[::1]", "::1"},
		// Bare IPv6 (no brackets, no port)
		{"::1", "::1"},
		// Public hostname
		{"cdn.example.com", "cdn.example.com"},
		{"cdn.example.com:443", "cdn.example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := StripHostPort(tc.in)
			if got != tc.want {
				t.Errorf("StripHostPort(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isLocalOrIP
// ---------------------------------------------------------------------------

func TestIsLocalOrIP(t *testing.T) {
	trueTests := []string{
		"localhost",
		"LOCALHOST",                 // case-insensitive
		"app.localhost",             // single-label subdomain
		"foo.bar.localhost",         // multi-label subdomain (RFC 6761)
		"deep.nested.app.localhost", // deeply nested
		"127.0.0.1",
		"::1",
		"192.168.1.10",
		"0.0.0.0",
		"2001:db8::1",
	}
	falseTests := []string{
		"cdn.example.com",
		"localhost.evil.com", // NOT a .localhost subdomain — evil.com is the TLD
		"example.com",
		"",
	}

	for _, h := range trueTests {
		t.Run("true/"+h, func(t *testing.T) {
			if !isLocalOrIP(h) {
				t.Errorf("isLocalOrIP(%q) = false, want true", h)
			}
		})
	}
	for _, h := range falseTests {
		t.Run("false/"+h, func(t *testing.T) {
			if isLocalOrIP(h) {
				t.Errorf("isLocalOrIP(%q) = true, want false", h)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// requestHostname — tested via a real Fiber request
// ---------------------------------------------------------------------------

// requestHostnameViaFiber sends a synthetic request through a Fiber test app
// and returns the value produced by requestHostname.
// The Fiber context is recycled after app.Test returns, so we must capture
// the result string inside the handler, not the *fiber.Ctx pointer itself.
func requestHostnameViaFiber(t *testing.T, host, xForwardedHost string) string {
	t.Helper()
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	var captured string
	app.Get("/", func(c *fiber.Ctx) error {
		captured = requestHostname(c)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = host
	if xForwardedHost != "" {
		req.Header.Set("X-Forwarded-Host", xForwardedHost)
	}
	if _, err := app.Test(req); err != nil {
		t.Fatalf("fiber.Test: %v", err)
	}
	return captured
}

func TestRequestHostname(t *testing.T) {
	tests := []struct {
		name           string
		host           string
		xForwardedHost string
		want           string
	}{
		{
			name: "plain localhost with port",
			host: "localhost:8080",
			want: "localhost",
		},
		{
			name: "127.0.0.1 with port",
			host: "127.0.0.1:9000",
			want: "127.0.0.1",
		},
		{
			name: "bracketed IPv6 with port",
			host: "[::1]:8080",
			want: "::1",
		},
		{
			name: "bracketed IPv6 no port",
			host: "[::1]",
			want: "::1",
		},
		{
			name: "public hostname no port",
			host: "cdn.example.com",
			want: "cdn.example.com",
		},
		{
			name: "public hostname with port",
			host: "cdn.example.com:443",
			want: "cdn.example.com",
		},
		// X-Forwarded-Host takes precedence over Host
		{
			name:           "X-Forwarded-Host single value",
			host:           "internal.host:4500",
			xForwardedHost: "cdn.example.com",
			want:           "cdn.example.com",
		},
		// Comma-separated chain — first (leftmost) value wins
		{
			name:           "X-Forwarded-Host chain",
			host:           "internal.host:4500",
			xForwardedHost: "cdn.example.com, proxy1.internal, proxy2.internal",
			want:           "cdn.example.com",
		},
		// Leading/trailing spaces in header value
		{
			name:           "X-Forwarded-Host with surrounding spaces",
			host:           "internal.host",
			xForwardedHost: "  cdn.example.com  ",
			want:           "cdn.example.com",
		},
		// Leading space before first element in a chain
		{
			name:           "X-Forwarded-Host chain with leading space",
			host:           "internal.host",
			xForwardedHost: "  cdn.example.com, proxy.internal",
			want:           "cdn.example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := requestHostnameViaFiber(t, tc.host, tc.xForwardedHost)
			if got != tc.want {
				t.Errorf("requestHostname() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isLocalOrIP + localDomain fallback — logic-only unit test
// ---------------------------------------------------------------------------

// TestLocalDomainFallbackLogic verifies the condition used in Serve:
//
//	if h.localDomain != "" && isLocalOrIP(hostname) { lookupHost = h.localDomain }
func TestLocalDomainFallbackLogic(t *testing.T) {
	const fallback = "cdn.example.com"

	resolve := func(localDomain, hostname string) string {
		if localDomain != "" && isLocalOrIP(hostname) {
			return localDomain
		}
		return hostname
	}

	cases := []struct {
		hostname string
		want     string
	}{
		{"localhost", fallback},
		{"127.0.0.1", fallback},
		{"::1", fallback},
		{"app.localhost", fallback},
		{"foo.bar.localhost", fallback},
		{"cdn.example.com", "cdn.example.com"}, // public domain — unchanged
	}

	for _, tc := range cases {
		t.Run(tc.hostname, func(t *testing.T) {
			got := resolve(fallback, tc.hostname)
			if got != tc.want {
				t.Errorf("resolve(%q) = %q, want %q", tc.hostname, got, tc.want)
			}
		})
	}

	// Also verify that an empty localDomain disables the fallback.
	for _, h := range []string{"localhost", "127.0.0.1", "::1"} {
		t.Run("no-fallback/"+h, func(t *testing.T) {
			got := resolve("", h)
			if got != h {
				t.Errorf("resolve(%q) with empty localDomain = %q, want %q", h, got, h)
			}
		})
	}
}

