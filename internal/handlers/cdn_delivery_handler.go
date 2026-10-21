package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/amantyagi23/cdn-network/internal/domain/cdn"
	"github.com/amantyagi23/cdn-network/internal/usecase"
	"github.com/amantyagi23/cdn-network/pkg/response"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// CDNDeliveryHandler serves public CDN requests.
// Mounted at the root so custom hostnames route directly here:
//
//	GET /cdn/<object-key>
//
// The Host header (or X-Forwarded-Host) identifies the CDN domain, which
// maps to a bucket via the usecase layer, which proxies from local storage.
type CDNDeliveryHandler struct {
	uc  usecase.CDNDeliveryUsecase
	log *zap.Logger

	// localDomain is used when the request host is localhost or a bare IP
	// (e.g. http://localhost:8080/cdn/foo, http://127.0.0.1:9000/cdn/foo).
	// If empty, localhost/IP requests go through the normal lookup and
	// normally return 404 unless you've registered them as domains.
	localDomain string
}

type Option func(*CDNDeliveryHandler)

// WithLocalDomain routes localhost/IP-hosted requests to domain.
// Useful for local development and smoke tests.
func WithLocalDomain(domain string) Option {
	return func(h *CDNDeliveryHandler) { h.localDomain = domain }
}

func NewCDNDeliveryHandler(uc usecase.CDNDeliveryUsecase, log *zap.Logger, opts ...Option) *CDNDeliveryHandler {
	h := &CDNDeliveryHandler{uc: uc, log: log}
	for _, o := range opts {
		o(h)
	}
	return h
}

// RegisterRoutes mounts the delivery endpoint.
func (h *CDNDeliveryHandler) RegisterRoutes(router fiber.Router) {
	router.Get("/*", h.Serve)
}

func (h *CDNDeliveryHandler) Serve(c *fiber.Ctx) error {
	hostname := requestHostname(c) // port + brackets already stripped
	if hostname == "" {
		return response.BadRequest(c, "BAD_HOST", "could not determine request host")
	}

	objectURL := strings.TrimPrefix(c.Params("*"), "/")

	if objectURL == "" {
		return response.BadRequest(c, "MISSING_KEY", "object key is required")
	}

	bucketName, objectKey, err := extractBucketName(objectURL)
	fmt.Println(bucketName, objectKey)
	// Localhost / bare-IP requests: either route to a configured fallback
	// domain or let normal lookup handle it (usually 404).
	lookupHost := hostname
	if h.localDomain != "" && isLocalOrIP(hostname) {
		lookupHost = h.localDomain
	}

	domain, err := h.uc.Resolve(c.Context(), lookupHost, bucketName)
	if err != nil {
		if !errors.Is(err, cdn.ErrDomainNotFound) {
			h.log.Warn("CDNDelivery: resolve error",
				zap.String("hostname", hostname),
				zap.String("lookup", lookupHost),
				zap.Error(err),
			)
		}
		return response.NotFound(c, "CDN domain not found")
	}

	// Buffer the object locally so we can set the correct headers before
	// committing any bytes to the response.  This prevents a partial 200
	// body from being sent when the stream fails mid-way, and avoids the
	// situation where headers (status, Content-Type, Cache-Control) are
	// written after bytes have already been flushed.
	var buf bytes.Buffer

	meta, err := h.uc.Stream(c.Context(), domain, objectKey, &buf)
	if err != nil {
		h.log.Warn("CDNDelivery: stream error",
			zap.String("hostname", hostname),
			zap.String("objectKey", objectKey),
			zap.Error(err),
		)
		return response.NotFound(c, "object not found")
	}

	ct := meta.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	c.Set(fiber.HeaderContentType, ct)
	if meta.ETag != "" {
		c.Set(fiber.HeaderETag, meta.ETag)
	}
	if meta.CacheControl != "" {
		c.Set(fiber.HeaderCacheControl, meta.CacheControl)
	} else {
		c.Set(fiber.HeaderCacheControl, "public, max-age=3600")
	}
	c.Set("X-CDN-Origin", "storage")

	return c.Send(buf.Bytes())
}

// -----------------------------------------------------------------------------
// Host helpers
// -----------------------------------------------------------------------------

// requestHostname returns the request host with port and IPv6 brackets removed.
//
//	"localhost:8080"        -> "localhost"
//	"127.0.0.1:9000"        -> "127.0.0.1"
//	"[::1]:8080"            -> "::1"
//	"[::1]"                 -> "::1"
//	"::1"                   -> "::1"
//	"cdn.example.com:443"   -> "cdn.example.com"
//	"cdn.example.com"       -> "cdn.example.com"
//
// X-Forwarded-Host is only honoured when the request arrives from a
// trusted proxy (Fiber's EnableTrustedProxyCheck + TrustedProxies).
// Without that configuration every connection is treated as direct and
// only the Host header is used, preventing hostname spoofing.
func requestHostname(c *fiber.Ctx) string {
	// c.IsProxyTrusted() returns true when either proxy checking is
	// disabled (no TRUSTED_PROXIES configured) or the peer IP is in the
	// trusted list.  Only accept X-Forwarded-Host in the trusted case.
	if c.IsProxyTrusted() {
		if raw := strings.TrimSpace(c.Get("X-Forwarded-Host")); raw != "" {
			// X-Forwarded-Host may be a comma-separated proxy chain; use
			// only the first (leftmost) entry.
			if i := strings.IndexByte(raw, ','); i >= 0 {
				raw = strings.TrimSpace(raw[:i])
			}
			if raw != "" {
				return StripHostPort(raw)
			}
		}
	}

	// Fall back to the raw Host header.
	// c.Hostname() returns the Host header without stripping the port
	// in Fiber v2, so we always run it through StripHostPort.
	return StripHostPort(c.Hostname())
}

// StripHostPort removes an optional :port and any IPv6 brackets.
// Safe on bare IPv6 (no brackets, no port).
func StripHostPort(s string) string {
	// Fast path: standard host:port (also handles [v6]:port).
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	// Bracketed IPv6 with no port.
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return s[1 : len(s)-1]
	}
	// Bare host or bare IPv6.

	return s
}

// isLocalOrIP reports whether host is "localhost", any subdomain of
// ".localhost" (RFC 6761, all labels accepted), or a literal IP address.
func isLocalOrIP(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	_, err := netip.ParseAddr(host)
	return err == nil
}

func extractBucketName(objectPath string) (string, string, error) {
	// Remove query parameters.
	objectPath = strings.SplitN(objectPath, "?", 2)[0]
	objectPath = strings.TrimLeft(objectPath, "/")

	parts := strings.SplitN(objectPath, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid object path")
	}

	return parts[0], parts[0] + "/" + parts[1], nil
}
