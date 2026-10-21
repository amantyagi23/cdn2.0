package handlers

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// ---------------------------------------------------------------------------
// requestHostname — trusted-proxy gate
// ---------------------------------------------------------------------------

// requestHostnameWithTrustedProxy creates a Fiber app with trusted proxy
// checking enabled for the given trustedIP and returns the resolved hostname.
func requestHostnameWithTrustedProxy(t *testing.T, trustedIP, host, xForwardedHost string) string {
	t.Helper()
	app := fiber.New(fiber.Config{
		DisableStartupMessage:   true,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{trustedIP},
		ProxyHeader:             fiber.HeaderXForwardedFor,
	})
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
	if _, err := app.Test(req, -1); err != nil {
		t.Fatalf("fiber.Test: %v", err)
	}
	return captured
}

// TestRequestHostname_XForwardedHostHonouredWithoutTrustedProxyCheck verifies
// Fiber v2's semantics: when EnableTrustedProxyCheck is false (the default),
// IsProxyTrusted() returns true for every request, so X-Forwarded-Host IS
// honoured.  This is safe when the application is only reachable via a
// trusted reverse proxy at the network layer.  To restrict proxy-header trust
// to explicit IPs, set TRUSTED_PROXIES in the environment.
func TestRequestHostname_XForwardedHostHonouredWithoutTrustedProxyCheck(t *testing.T) {
	// requestHostnameViaFiber uses the default fiber.Config (EnableTrustedProxyCheck: false),
	// so IsProxyTrusted() returns true and the X-Forwarded-Host value is used.
	got := requestHostnameViaFiber(t, "internal.host", "cdn.example.com")
	if got != "cdn.example.com" {
		t.Errorf("requestHostname() = %q, want %q (X-Forwarded-Host should be honoured when proxy check disabled)",
			got, "cdn.example.com")
	}
}

// TestRequestHostname_XForwardedHostHonouredFromTrustedProxy verifies that
// when the request arrives from a known trusted proxy IP the X-Forwarded-Host
// header is used.
func TestRequestHostname_XForwardedHostHonouredFromTrustedProxy(t *testing.T) {
	// We cannot trivially fake the remote IP in Fiber's test helper, so
	// this test configures the trusted list to include 0.0.0.0/0 (all IPs),
	// which makes every test connection trusted.
	app := fiber.New(fiber.Config{
		DisableStartupMessage:   true,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0", "::/0"},
		ProxyHeader:             fiber.HeaderXForwardedFor,
	})
	var captured string
	app.Get("/", func(c *fiber.Ctx) error {
		captured = requestHostname(c)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "internal.host:4500"
	req.Header.Set("X-Forwarded-Host", "cdn.example.com")
	if _, err := app.Test(req, -1); err != nil {
		t.Fatalf("fiber.Test: %v", err)
	}
	if captured != "cdn.example.com" {
		t.Errorf("requestHostname() = %q, want %q when coming from trusted proxy",
			captured, "cdn.example.com")
	}
}

// TestRequestHostname_SpacesInXForwardedHostChainFromTrustedProxy verifies the
// comma-split and trim logic still works under the new trusted-proxy gate.
func TestRequestHostname_SpacesInXForwardedHostChainFromTrustedProxy(t *testing.T) {
	app := fiber.New(fiber.Config{
		DisableStartupMessage:   true,
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0", "::/0"},
		ProxyHeader:             fiber.HeaderXForwardedFor,
	})
	var captured string
	app.Get("/", func(c *fiber.Ctx) error {
		captured = requestHostname(c)
		return c.SendStatus(200)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "internal.host"
	req.Header.Set("X-Forwarded-Host", "  cdn.example.com , proxy1.internal")
	if _, err := app.Test(req, -1); err != nil {
		t.Fatalf("fiber.Test: %v", err)
	}
	if captured != "cdn.example.com" {
		t.Errorf("requestHostname() = %q, want %q", captured, "cdn.example.com")
	}
}
