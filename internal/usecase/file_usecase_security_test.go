package usecase

import (
	"path"
	"strings"
	"testing"
	"unicode"
)

// objectKeySafe returns true when all rune in the suffix segment of an object
// key are safe: alphanumeric, hyphen, underscore, or a single dot.
func objectKeySafe(key string) bool {
	// Key format: "YYYY/MM/<uuid>-<safe-filename>"
	// We only need to inspect the filename suffix.
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return false
	}
	suffix := parts[2] // "<uuid>-<safe-filename>"
	for _, r := range suffix {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

// objectKeyNoTraversal returns true when the cleaned path does not escape the
// root (i.e. no ".." component remains).
func objectKeyNoTraversal(key string) bool {
	cleaned := path.Clean("/" + key)
	return !strings.Contains(cleaned, "..") && strings.HasPrefix(cleaned, "/")
}

func TestObjectKeySafeFilenames(t *testing.T) {
	cases := []struct {
		name     string
		filename string
	}{
		{"simple", "photo.jpg"},
		{"with spaces", "my file.png"},
		{"uppercase", "IMAGE.JPEG"},
		{"numbers", "file123.mp4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key := objectKey(tc.filename)
			if !objectKeySafe(key) {
				t.Errorf("objectKey(%q) = %q contains unsafe chars", tc.filename, key)
			}
			if !objectKeyNoTraversal(key) {
				t.Errorf("objectKey(%q) = %q enables traversal", tc.filename, key)
			}
		})
	}
}

func TestObjectKeyTraversalFilenames(t *testing.T) {
	// These filenames contain traversal attempts or shell-special characters.
	// objectKey must produce a safe, confined key for all of them.
	malicious := []string{
		"../../etc/passwd",
		"../secret.key",
		"foo/../../bar",
		"a\\b\\c",
		"<script>alert(1)</script>.html",
		"file; rm -rf /",
		"file\x00null.txt",
		"%2e%2e%2fetc%2fpasswd",
		"....//....//etc/passwd",
		".",
		"..",
		"/",
		"",
	}
	for _, fn := range malicious {
		t.Run(fn, func(t *testing.T) {
			key := objectKey(fn)
			if !objectKeyNoTraversal(key) {
				t.Errorf("objectKey(%q) = %q traverses outside root", fn, key)
			}
			if !objectKeySafe(key) {
				t.Errorf("objectKey(%q) = %q contains unsafe characters", fn, key)
			}
			// Must never be empty.
			if key == "" {
				t.Errorf("objectKey(%q) returned empty string", fn)
			}
			// Must have three slash-separated segments.
			parts := strings.SplitN(key, "/", 3)
			if len(parts) != 3 {
				t.Errorf("objectKey(%q) = %q does not have three path components", fn, key)
			}
		})
	}
}

func TestObjectKeyUniqueness(t *testing.T) {
	// Two calls with the same filename must produce different keys (UUID prefix).
	k1 := objectKey("photo.jpg")
	k2 := objectKey("photo.jpg")
	if k1 == k2 {
		t.Errorf("objectKey returned identical keys on two calls: %q", k1)
	}
}

func TestObjectKeyNeverEmpty(t *testing.T) {
	for _, fn := range []string{"", ".", "..", "/"} {
		key := objectKey(fn)
		if key == "" {
			t.Errorf("objectKey(%q) returned empty string", fn)
		}
	}
}
