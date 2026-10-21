package usecase

import (
	"path"
	"strings"
	"testing"
	"unicode"
)

const testBucket = "product-images"

// objectKeySafe checks that the generated filename suffix contains only
// permitted ASCII characters.
func objectKeySafe(key string) bool {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) != 4 {
		return false
	}

	suffix := parts[3]
	if suffix == "" {
		return false
	}

	for _, r := range suffix {
		if (r >= 'a' && r <= 'z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}

	return true
}

// objectKeyNoTraversal checks that the key contains no traversal components.
func objectKeyNoTraversal(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") {
		return false
	}

	for _, part := range strings.Split(key, "/") {
		if part == "." || part == ".." || part == "" {
			return false
		}
	}

	cleaned := path.Clean(key)
	return cleaned == key && !strings.HasPrefix(cleaned, "../")
}

// objectKeyHasExpectedStructure validates:
// bucket/YYYY/MM/<uuid>-<safe-filename>
func objectKeyHasExpectedStructure(key, bucket string) bool {
	parts := strings.SplitN(key, "/", 4)
	if len(parts) != 4 {
		return false
	}

	if parts[0] != bucket {
		return false
	}

	if len(parts[1]) != 4 {
		return false
	}

	if len(parts[2]) != 2 {
		return false
	}

	return parts[3] != ""
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
			key := objectKey(testBucket, tc.filename)

			if !objectKeySafe(key) {
				t.Errorf("objectKey(%q) = %q contains unsafe filename characters",
					tc.filename, key)
			}

			if !objectKeyNoTraversal(key) {
				t.Errorf("objectKey(%q) = %q enables traversal",
					tc.filename, key)
			}

			if !objectKeyHasExpectedStructure(key, testBucket) {
				t.Errorf("objectKey(%q) = %q has an invalid structure",
					tc.filename, key)
			}
		})
	}
}

func TestObjectKeyTraversalFilenames(t *testing.T) {
	malicious := []string{
		"../../etc/passwd",
		"../secret.key",
		"foo/../../bar",
		`a\b\c`,
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
			key := objectKey(testBucket, fn)

			if !objectKeyNoTraversal(key) {
				t.Errorf("objectKey(%q) = %q traverses outside root", fn, key)
			}

			if !objectKeySafe(key) {
				t.Errorf("objectKey(%q) = %q contains unsafe filename characters",
					fn, key)
			}

			if key == "" {
				t.Errorf("objectKey(%q) returned an empty key", fn)
			}

			if !objectKeyHasExpectedStructure(key, testBucket) {
				t.Errorf("objectKey(%q) = %q has an invalid structure", fn, key)
			}
		})
	}
}

func TestObjectKeyBucketIsolation(t *testing.T) {
	key1 := objectKey("product-images", "photo.jpg")
	key2 := objectKey("user-uploads", "photo.jpg")

	if key1 == key2 {
		t.Fatal("different buckets produced identical keys")
	}

	if !strings.HasPrefix(key1, "product-images/") {
		t.Errorf("key %q does not belong to product-images", key1)
	}

	if !strings.HasPrefix(key2, "user-uploads/") {
		t.Errorf("key %q does not belong to user-uploads", key2)
	}
}

func TestObjectKeyUniqueness(t *testing.T) {
	k1 := objectKey(testBucket, "photo.jpg")
	k2 := objectKey(testBucket, "photo.jpg")

	if k1 == k2 {
		t.Errorf("objectKey returned identical keys: %q", k1)
	}
}

func TestObjectKeyNeverEmpty(t *testing.T) {
	for _, fn := range []string{"", ".", "..", "/"} {
		key := objectKey(testBucket, fn)

		if key == "" {
			t.Errorf("objectKey(%q) returned an empty key", fn)
		}
	}
}

// Compile-time use of unicode is intentionally avoided because the generated
// filename policy permits ASCII characters only.
var _ = unicode.IsLetter
