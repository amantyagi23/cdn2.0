package storage

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestClient creates an isolated StorageClient backed by a temp directory.
// The returned cleanup func removes the directory.
func newTestClient(t *testing.T) (*StorageClient, func()) {
	t.Helper()
	dir := t.TempDir()
	c, err := NewClient(Config{
		BasePath:      dir,
		SigningSecret: "test-secret-1234",
		PublicBaseURL: "http://localhost:9000",
	}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, func() { os.RemoveAll(dir) }
}

// ----------------------------------------------------------------------------
// Atomic write: temp-file + rename
// ----------------------------------------------------------------------------

// TestPutAtomicWrite verifies that a completed put creates exactly one file at
// the expected path and no stray temp files remain.
func TestPutAtomicWrite(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "testbucket", ""); err != nil {
		t.Fatalf("MakeBucket: %v", err)
	}

	const content = "hello, storage"
	_, err := c.PutObject(ctx, "testbucket", "hello.txt",
		strings.NewReader(content), int64(len(content)),
		PutObjectOptions{ContentType: "text/plain"})
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}

	// Confirm the final file exists.
	full := filepath.Join(c.basePath, "testbucket", "hello.txt")
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("expected object file at %s: %v", full, err)
	}

	// Confirm no temp files remain.
	bucketDir := filepath.Join(c.basePath, "testbucket")
	entries, _ := os.ReadDir(bucketDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Errorf("stray temp file found: %s", e.Name())
		}
	}
}

// TestGetObjectRoundTrip verifies that a stored object can be retrieved intact.
func TestGetObjectRoundTrip(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "b", ""); err != nil {
		t.Fatal(err)
	}

	want := "content data"
	if _, err := c.PutObject(ctx, "b", "k.txt", strings.NewReader(want), int64(len(want)),
		PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	rc, err := c.GetObject(ctx, "b", "k.txt")
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		t.Fatal(err)
	}
	if buf.String() != want {
		t.Errorf("content mismatch: got %q, want %q", buf.String(), want)
	}
}

// ----------------------------------------------------------------------------
// Path resolution — traversal is neutralised by path.Clean
// ----------------------------------------------------------------------------

// TestResolvePath_TraversalNeutralised verifies that keys containing ".."
// sequences are cleaned to a path that remains inside the bucket root.
// The implementation uses path.Clean("/"+key) which consumes all ".."
// segments before the key reaches the filesystem, so no traversal escape
// is possible.
func TestResolvePath_TraversalNeutralised(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	// These all contain ".." but path.Clean reduces them to a safe suffix.
	keys := []string{
		"../secret",
		"../../etc/passwd",
		"foo/../../outside",
		"foo/../../../root",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			full, err := c.resolvePath("mybucket", key)
			// Must either succeed (cleaned path inside bucket) or return ErrInvalidPath.
			// It must never return a path outside the bucket root.
			if err == nil {
				bucketRoot := filepath.Join(c.basePath, "mybucket")
				if !strings.HasPrefix(full, bucketRoot+string(filepath.Separator)) &&
					full != bucketRoot {
					t.Errorf("resolvePath(%q) = %q escapes bucket root %q", key, full, bucketRoot)
				}
			}
		})
	}
}

func TestValidateBucket_BadNames(t *testing.T) {
	bad := []string{"", ".", "..", ".meta", "a/b", `a\b`, "a..b"}
	for _, name := range bad {
		t.Run(name, func(t *testing.T) {
			if err := validateBucket(name); err == nil {
				t.Errorf("validateBucket(%q) should have returned an error", name)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// HTTP handler — internal error does not leak details
// ----------------------------------------------------------------------------

func TestWriteError_NoLeakOnInternalError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, os.ErrPermission) // a non-classified error
	resp := rr.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "permission") {
		t.Errorf("response body leaks internal error: %q", string(body))
	}
}

// ----------------------------------------------------------------------------
// servePut — body size limit
// ----------------------------------------------------------------------------

func TestServePut_SizeLimitEnforced(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "bkt", ""); err != nil {
		t.Fatal(err)
	}

	// Build a signed URL for the PUT.
	exp := time.Now().Add(5 * time.Minute).Unix()
	sig := signURL(c.signingSecret, http.MethodPut, "bkt", "bigfile.bin", exp)

	url := "/bkt/bigfile.bin?expires=" + itoa(exp) + "&sig=" + sig

	// Content-Length larger than maxPresignedPutBytes should be rejected.
	req := httptest.NewRequest(http.MethodPut, url, strings.NewReader("x"))
	req.ContentLength = maxPresignedPutBytes + 1

	rr := httptest.NewRecorder()
	c.serveHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %d (body: %s)", rr.Code, rr.Body.String())
	}
}

// ----------------------------------------------------------------------------
// HTTP handler — unauthorised access
// ----------------------------------------------------------------------------

func TestServeHTTP_PrivateBucketNoSignature(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "priv", ""); err != nil {
		t.Fatal(err)
	}
	// Store an object.
	if _, err := c.PutObject(ctx, "priv", "secret.txt",
		strings.NewReader("secret"), 6,
		PutObjectOptions{ContentType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	// Request without signature should be rejected.
	req := httptest.NewRequest(http.MethodGet, "/priv/secret.txt", nil)
	rr := httptest.NewRecorder()
	c.serveHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unsigned private request, got %d", rr.Code)
	}
}

func TestServeHTTP_ExpiredSignatureRejected(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "bkt2", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutObject(ctx, "bkt2", "file.txt",
		strings.NewReader("data"), 4,
		PutObjectOptions{}); err != nil {
		t.Fatal(err)
	}

	// Signature that expired 1 second ago.
	exp := time.Now().Add(-1 * time.Second).Unix()
	sig := signURL(c.signingSecret, http.MethodGet, "bkt2", "file.txt", exp)
	url := "/bkt2/file.txt?expires=" + itoa(exp) + "&sig=" + sig

	req := httptest.NewRequest(http.MethodGet, url, nil)
	rr := httptest.NewRecorder()
	c.serveHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for expired signature, got %d", rr.Code)
	}
}

// itoa is a minimal int64-to-string helper for building test URLs.
func itoa(n int64) string {
	return string([]byte(strings.TrimSpace(itoa64(n))))
}

func itoa64(n int64) string {
	// strconv.FormatInt without importing strconv at package level — keep test
	// dependencies minimal.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	neg := n < 0
	if neg {
		n = -n
	}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ----------------------------------------------------------------------------
// X-Content-Type-Options on served objects
// ----------------------------------------------------------------------------

func TestServeObject_XContentTypeOptionsSet(t *testing.T) {
	c, cleanup := newTestClient(t)
	defer cleanup()

	ctx := context.Background()
	if err := c.MakeBucket(ctx, "pub", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.SetPublicRead(ctx, "pub"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.PutObject(ctx, "pub", "img.png",
		strings.NewReader("fake-png"), 8,
		PutObjectOptions{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/pub/img.png", nil)
	rr := httptest.NewRecorder()
	c.serveHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if v := rr.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", v)
	}
}
