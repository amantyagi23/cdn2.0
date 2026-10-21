package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	DefaultBasePath = "storage"
	metaDir         = ".meta"
	defaultPartSize = 16 << 20
)

var (
	ErrNotFound      = errors.New("storage: not found")
	ErrAlreadyExists = errors.New("storage: already exists")
	ErrAccessDenied  = errors.New("storage: access denied")
	ErrInvalidPath   = errors.New("storage: invalid path")
	ErrExpired       = errors.New("storage: url expired")
	ErrUnauthorized  = errors.New("storage: unauthorized")
)

// ----------------------------------------------------------------------------
// CONFIG / CONSTRUCTOR
// ----------------------------------------------------------------------------

// Config holds storage client configuration.
type Config struct {
	BasePath      string // where objects live (default: "storage"), created if missing
	SigningSecret string // HMAC secret for presigned URLs (required for presigning)
	PublicBaseURL string // public URL of the storage HTTP handler, e.g. "https://cdn.example.com"
}

type StorageClient struct {
	basePath      string
	signingSecret []byte
	publicBaseURL string
	log           *zap.Logger
}

// NewClient initializes the storage service and creates the base directory
// (plus metadata dir) at runtime if they do not already exist.
func NewClient(config Config, log *zap.Logger) (*StorageClient, error) {
	base := config.BasePath
	if base == "" {
		base = DefaultBasePath
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve base path: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage: create base dir %s: %w", abs, err)
	}
	if err := os.MkdirAll(filepath.Join(abs, metaDir), 0o755); err != nil {
		return nil, fmt.Errorf("storage: create meta dir: %w", err)
	}
	if log == nil {
		log = zap.NewNop()
	}
	log.Info("storage initialized",
		zap.String("base_path", abs),
		zap.Bool("presigned_enabled", config.SigningSecret != "" && config.PublicBaseURL != ""),
	)
	return &StorageClient{
		basePath:      abs,
		signingSecret: []byte(config.SigningSecret),
		publicBaseURL: strings.TrimRight(config.PublicBaseURL, "/"),
		log:           log,
	}, nil
}

func (c *StorageClient) BasePath() string { return c.basePath }
func (c *StorageClient) Close()           {} // no-op, symmetry with NATS/Redis clients

// Ping verifies the base directory is still accessible.
func (c *StorageClient) Ping(_ context.Context) error {
	fi, err := os.Stat(c.basePath)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("storage: %s is not a directory", c.basePath)
	}
	return nil
}

// ----------------------------------------------------------------------------
// TYPES (mirrors the previous MinIO API surface)
// ----------------------------------------------------------------------------

type BucketInfo struct {
	Name         string
	CreationDate time.Time
}

type UploadInfo struct {
	Bucket       string
	Key          string
	ETag         string
	Size         int64
	LastModified time.Time
}

type ObjectInfo struct {
	Bucket       string
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	UserMetadata map[string]string
}

type PutObjectOptions struct {
	ContentType        string
	CacheControl       string
	ContentEncoding    string
	ContentDisposition string
	UserMetadata       map[string]string
}

type BucketMeta struct {
	CreatedAt  time.Time `json:"created_at"`
	Region     string    `json:"region,omitempty"`
	Visibility string    `json:"visibility,omitempty"` // "public" | "private"
}

type ObjectMeta struct {
	ContentType        string            `json:"content_type,omitempty"`
	CacheControl       string            `json:"cache_control,omitempty"`
	ContentEncoding    string            `json:"content_encoding,omitempty"`
	ContentDisposition string            `json:"content_disposition,omitempty"`
	UserMetadata       map[string]string `json:"user_metadata,omitempty"`
	ETag               string            `json:"etag,omitempty"`
	CreatedAt          time.Time         `json:"created_at,omitempty"`
}

// ----------------------------------------------------------------------------
// PATH SAFETY
// ----------------------------------------------------------------------------

func validateBucket(bucket string) error {
	if bucket == "" || bucket == "." || bucket == ".." || bucket == metaDir {
		return ErrInvalidPath
	}
	if strings.ContainsAny(bucket, `/\`) || strings.Contains(bucket, "..") {
		return ErrInvalidPath
	}
	return nil
}

func (c *StorageClient) resolvePath(bucket, key string) (string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", err
	}
	bucketRoot := filepath.Join(c.basePath, bucket)
	if key == "" {
		return bucketRoot, nil
	}
	clean := strings.TrimPrefix(path.Clean("/"+key), "/")
	if clean == "" || clean == "." {
		return bucketRoot, nil
	}
	full := filepath.Join(bucketRoot, filepath.FromSlash(clean))
	rel, err := filepath.Rel(bucketRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return full, nil
}

func (c *StorageClient) metaPath(bucket, key string) (string, error) {
	if err := validateBucket(bucket); err != nil {
		return "", err
	}
	metaRoot := filepath.Join(c.basePath, metaDir, bucket)
	if key == "" {
		return filepath.Join(metaRoot, "bucket.json"), nil
	}
	clean := strings.TrimPrefix(path.Clean("/"+key), "/")
	if clean == "" {
		return "", ErrInvalidPath
	}
	full := filepath.Join(metaRoot, filepath.FromSlash(clean)+".json")
	rel, err := filepath.Rel(metaRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrInvalidPath
	}
	return full, nil
}

// ----------------------------------------------------------------------------
// BUCKET OPERATIONS
// ----------------------------------------------------------------------------

func (c *StorageClient) BucketExists(_ context.Context, bucket string) (bool, error) {
	if err := validateBucket(bucket); err != nil {
		return false, err
	}
	fi, err := os.Stat(filepath.Join(c.basePath, bucket))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return fi.IsDir(), nil
}

func (c *StorageClient) MakeBucket(_ context.Context, bucket, region string) error {
	if err := validateBucket(bucket); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(c.basePath, bucket), 0o755); err != nil {
		return err
	}
	return c.writeBucketMeta(bucket, BucketMeta{CreatedAt: time.Now().UTC(), Region: region})
}

func (c *StorageClient) EnsureBucket(ctx context.Context, bucket, region string) error {
	ok, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	return c.MakeBucket(ctx, bucket, region)
}

func (c *StorageClient) RemoveBucket(_ context.Context, bucket string) error {
	if err := validateBucket(bucket); err != nil {
		return err
	}
	p := filepath.Join(c.basePath, bucket)
	entries, err := os.ReadDir(p)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("storage: bucket %q is not empty", bucket)
	}
	if err := os.Remove(p); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(c.basePath, metaDir, bucket))
	return nil
}

func (c *StorageClient) ListBuckets(_ context.Context) ([]BucketInfo, error) {
	entries, err := os.ReadDir(c.basePath)
	if err != nil {
		return nil, err
	}
	var out []BucketInfo
	for _, e := range entries {
		if !e.IsDir() || e.Name() == metaDir {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, BucketInfo{Name: e.Name(), CreationDate: fi.ModTime()})
	}
	return out, nil
}

func (c *StorageClient) SetPublicRead(_ context.Context, bucket string) error {
	m, err := c.readBucketMeta(bucket)
	if err != nil {
		return err
	}
	m.Visibility = "public"
	return c.writeBucketMeta(bucket, m)
}

func (c *StorageClient) SetPrivate(_ context.Context, bucket string) error {
	m, err := c.readBucketMeta(bucket)
	if err != nil {
		return err
	}
	m.Visibility = "private"
	return c.writeBucketMeta(bucket, m)
}

// ----------------------------------------------------------------------------
// OBJECT OPERATIONS
// ----------------------------------------------------------------------------

func (c *StorageClient) PutObject(_ context.Context, bucket, key string, r io.Reader, _ int64, opts PutObjectOptions) (UploadInfo, error) {
	return c.put(bucket, key, r, opts)
}

func (c *StorageClient) PutObjectStreaming(_ context.Context, bucket, key string, r io.Reader, opts PutObjectOptions) (UploadInfo, error) {
	return c.put(bucket, key, r, opts)
}

func (c *StorageClient) put(bucket, key string, r io.Reader, opts PutObjectOptions) (UploadInfo, error) {
	full, err := c.resolvePath(bucket, key)
	if err != nil {
		return UploadInfo{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return UploadInfo{}, err
	}

	// Write to a sibling temp file so that a crash or interrupted upload
	// never leaves a partially-written object visible at the real key.
	tmp, err := os.CreateTemp(filepath.Dir(full), ".upload-*")
	if err != nil {
		return UploadInfo{}, err
	}
	tmpName := tmp.Name()

	h := sha256.New()
	n, copyErr := io.Copy(tmp, io.TeeReader(r, h))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return UploadInfo{}, copyErr
		}
		return UploadInfo{}, closeErr
	}

	// Atomic rename: the object becomes visible only after a successful write.
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return UploadInfo{}, err
	}

	etag := hex.EncodeToString(h.Sum(nil))
	meta := ObjectMeta{
		ContentType:        opts.ContentType,
		CacheControl:       opts.CacheControl,
		ContentEncoding:    opts.ContentEncoding,
		ContentDisposition: opts.ContentDisposition,
		UserMetadata:       opts.UserMetadata,
		ETag:               etag,
		CreatedAt:          time.Now().UTC(),
	}
	if err := c.writeObjectMeta(bucket, key, meta); err != nil {
		c.log.Warn("storage: failed to write object metadata",
			zap.String("bucket", bucket), zap.String("key", key), zap.Error(err))
	}

	return UploadInfo{
		Bucket:       bucket,
		Key:          key,
		ETag:         etag,
		Size:         n,
		LastModified: meta.CreatedAt,
	}, nil
}

// GetObject returns the raw reader. Caller MUST close it.
func (c *StorageClient) GetObject(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	full, err := c.resolvePath(bucket, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (c *StorageClient) DownloadTo(ctx context.Context, bucket, key string, w io.Writer) (ObjectInfo, error) {
	info, err := c.StatObject(ctx, bucket, key)
	if err != nil {
		return info, err
	}
	rc, err := c.GetObject(ctx, bucket, key)
	if err != nil {
		return info, err
	}
	defer rc.Close()
	if _, err := io.Copy(w, rc); err != nil {
		return info, fmt.Errorf("storage.DownloadTo(%s/%s): %w", bucket, key, err)
	}
	return info, nil
}

func (c *StorageClient) StatObject(_ context.Context, bucket, key string) (ObjectInfo, error) {
	full, err := c.resolvePath(bucket, key)
	if err != nil {
		return ObjectInfo{}, err
	}
	fi, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, err
	}
	if fi.IsDir() {
		return ObjectInfo{}, ErrNotFound
	}
	meta, _ := c.readObjectMeta(bucket, key)
	return ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         fi.Size(),
		ETag:         meta.ETag,
		ContentType:  meta.ContentType,
		LastModified: fi.ModTime(),
		UserMetadata: meta.UserMetadata,
	}, nil
}

func (c *StorageClient) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.StatObject(ctx, bucket, key)
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}

func (c *StorageClient) RemoveObject(_ context.Context, bucket, key string) error {
	full, err := c.resolvePath(bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = c.removeObjectMeta(bucket, key)
	return nil
}

func (c *StorageClient) RemoveObjects(ctx context.Context, bucket string, keys []string) error {
	for _, k := range keys {
		if err := c.RemoveObject(ctx, bucket, k); err != nil {
			return err
		}
	}
	return nil
}

func (c *StorageClient) ListObjects(_ context.Context, bucket, prefix string, recursive bool) ([]ObjectInfo, error) {
	root, err := c.resolvePath(bucket, "")
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var out []ObjectInfo
	err = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			if !recursive && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		key := filepath.ToSlash(rel)
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}
		meta, _ := c.readObjectMeta(bucket, key)
		out = append(out, ObjectInfo{
			Bucket:       bucket,
			Key:          key,
			Size:         fi.Size(),
			ETag:         meta.ETag,
			ContentType:  meta.ContentType,
			LastModified: fi.ModTime(),
			UserMetadata: meta.UserMetadata,
		})
		return nil
	})
	return out, err
}

func (c *StorageClient) CopyObject(ctx context.Context, dstBucket, dstKey, srcBucket, srcKey string) (UploadInfo, error) {
	srcFull, err := c.resolvePath(srcBucket, srcKey)
	if err != nil {
		return UploadInfo{}, err
	}
	f, err := os.Open(srcFull)
	if err != nil {
		if os.IsNotExist(err) {
			return UploadInfo{}, ErrNotFound
		}
		return UploadInfo{}, err
	}
	defer f.Close()

	srcMeta, _ := c.readObjectMeta(srcBucket, srcKey)
	return c.put(dstBucket, dstKey, f, PutObjectOptions{
		ContentType:        srcMeta.ContentType,
		CacheControl:       srcMeta.CacheControl,
		ContentEncoding:    srcMeta.ContentEncoding,
		ContentDisposition: srcMeta.ContentDisposition,
		UserMetadata:       srcMeta.UserMetadata,
	})
}

// ----------------------------------------------------------------------------
// PRESIGNED URLS
// ----------------------------------------------------------------------------

func (c *StorageClient) presign(method, bucket, key string, expiry time.Duration, extra url.Values) (*url.URL, error) {
	if len(c.signingSecret) == 0 {
		return nil, errors.New("storage: signing secret not configured")
	}
	if c.publicBaseURL == "" {
		return nil, errors.New("storage: public base URL not configured")
	}
	if err := validateBucket(bucket); err != nil {
		return nil, err
	}
	exp := time.Now().Add(expiry).Unix()
	sig := signURL(c.signingSecret, method, bucket, key, exp)

	u, err := url.Parse(c.publicBaseURL + "/" + url.PathEscape(bucket) + "/" + escapeKey(key))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("expires", strconv.FormatInt(exp, 10))
	q.Set("sig", sig)
	for k, vs := range extra {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u, nil
}

func (c *StorageClient) PresignedGetObject(_ context.Context, bucket, key string, expiry time.Duration, reqParams url.Values) (*url.URL, error) {
	return c.presign(http.MethodGet, bucket, key, expiry, reqParams)
}

func (c *StorageClient) PresignedGetAttachment(_ context.Context, bucket, key string, expiry time.Duration, filename string) (*url.URL, error) {
	params := url.Values{}
	params.Set("response-content-disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	return c.presign(http.MethodGet, bucket, key, expiry, params)
}

func (c *StorageClient) PresignedPutObject(_ context.Context, bucket, key string, expiry time.Duration) (*url.URL, error) {
	return c.presign(http.MethodPut, bucket, key, expiry, nil)
}

func (c *StorageClient) PresignedHeadObject(_ context.Context, bucket, key string, expiry time.Duration, reqParams url.Values) (*url.URL, error) {
	return c.presign(http.MethodHead, bucket, key, expiry, reqParams)
}

func signURL(secret []byte, method, bucket, key string, expires int64) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%s\n%s\n%s\n%d", method, bucket, key, expires)
	return hex.EncodeToString(mac.Sum(nil))
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ----------------------------------------------------------------------------
// HTTP HANDLER (serves presigned URLs)
// ----------------------------------------------------------------------------

// Handler returns an http.Handler that serves objects via signed URLs.
// Mount it, e.g.:
//
//	http.Handle("/files/", http.StripPrefix("/files", client.Handler()))
//
// Make sure PublicBaseURL + "/<bucket>/<key>" resolves to this handler.
func (c *StorageClient) Handler() http.Handler {
	return http.HandlerFunc(c.serveHTTP)
}

func (c *StorageClient) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	parts := strings.SplitN(p, "/", 2)
	bucket := parts[0]
	key := ""
	if len(parts) > 1 {
		key = parts[1]
	}
	if err := validateBucket(bucket); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if ok, _ := c.BucketExists(r.Context(), bucket); !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Public buckets bypass signing for GET/HEAD.
	meta, _ := c.readBucketMeta(bucket)
	isPublicRead := meta.Visibility == "public" &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead)

	if !isPublicRead {
		q := r.URL.Query()
		expiresStr, sig := q.Get("expires"), q.Get("sig")
		if expiresStr == "" || sig == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		expires, err := strconv.ParseInt(expiresStr, 10, 64)
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if time.Now().Unix() > expires {
			http.Error(w, "expired", http.StatusForbidden)
			return
		}
		// Accept either the exact method or GET for HEAD (common browser behavior).
		valid := hmac.Equal([]byte(signURL(c.signingSecret, r.Method, bucket, key, expires)), []byte(sig))
		if !valid && r.Method == http.MethodHead {
			valid = hmac.Equal([]byte(signURL(c.signingSecret, http.MethodGet, bucket, key, expires)), []byte(sig))
		}
		if !valid {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		c.serveObject(w, r, bucket, key)
	case http.MethodPut:
		c.servePut(w, r, bucket, key)
	case http.MethodDelete:
		if err := c.RemoveObject(r.Context(), bucket, key); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (c *StorageClient) serveObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	full, err := c.resolvePath(bucket, key)
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	meta, _ := c.readObjectMeta(bucket, key)
	q := r.URL.Query()

	// response-content-* overrides are only valid when the request was
	// signed (the signature already validated above for private buckets).
	// For public buckets the override is still accepted so callers can
	// control the Content-Disposition / Content-Type on public downloads.
	if v := q.Get("response-content-type"); v != "" {
		w.Header().Set("Content-Type", v)
	} else if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	}
	if v := q.Get("response-content-disposition"); v != "" {
		w.Header().Set("Content-Disposition", v)
	} else if meta.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", meta.ContentDisposition)
	}
	if v := q.Get("response-cache-control"); v != "" {
		w.Header().Set("Cache-Control", v)
	} else if meta.CacheControl != "" {
		w.Header().Set("Cache-Control", meta.CacheControl)
	}
	if meta.ContentEncoding != "" {
		w.Header().Set("Content-Encoding", meta.ContentEncoding)
	}
	if meta.ETag != "" {
		w.Header().Set("ETag", `"`+meta.ETag+`"`)
	}
	// Prevent browsers from sniffing a different content type.
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// maxPresignedPutBytes is the maximum body size accepted by servePut.
// It is intentionally large (5 GiB) but provides a hard ceiling to
// prevent unbounded writes.  Lower this for your workload.
const maxPresignedPutBytes = 5 << 30 // 5 GiB

func (c *StorageClient) servePut(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if key == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Reject requests that claim a body larger than the ceiling.
	if r.ContentLength > maxPresignedPutBytes {
		http.Error(w, "request entity too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Limit the actual bytes read so a lying Content-Length cannot cause
	// unbounded storage consumption.
	body := io.LimitReader(r.Body, maxPresignedPutBytes+1)

	opts := PutObjectOptions{
		ContentType:        r.Header.Get("Content-Type"),
		CacheControl:       r.Header.Get("Cache-Control"),
		ContentEncoding:    r.Header.Get("Content-Encoding"),
		ContentDisposition: r.Header.Get("Content-Disposition"),
	}
	info, err := c.PutObjectStreaming(r.Context(), bucket, key, body, opts)
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("ETag", `"`+info.ETag+`"`)
	w.WriteHeader(http.StatusOK)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case IsNotFound(err):
		http.Error(w, "not found", http.StatusNotFound)
	case IsAccessDenied(err):
		http.Error(w, "forbidden", http.StatusForbidden)
	default:
		// Do not leak internal error details to the client.
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}

// ----------------------------------------------------------------------------
// METADATA HELPERS
// ----------------------------------------------------------------------------

func (c *StorageClient) readBucketMeta(bucket string) (BucketMeta, error) {
	p, err := c.metaPath(bucket, "")
	if err != nil {
		return BucketMeta{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return BucketMeta{}, nil
		}
		return BucketMeta{}, err
	}
	var m BucketMeta
	_ = json.Unmarshal(data, &m)
	return m, nil
}

func (c *StorageClient) writeBucketMeta(bucket string, m BucketMeta) error {
	p, err := c.metaPath(bucket, "")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (c *StorageClient) readObjectMeta(bucket, key string) (ObjectMeta, error) {
	p, err := c.metaPath(bucket, key)
	if err != nil {
		return ObjectMeta{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectMeta{}, nil
		}
		return ObjectMeta{}, err
	}
	var m ObjectMeta
	_ = json.Unmarshal(data, &m)
	return m, nil
}

func (c *StorageClient) writeObjectMeta(bucket, key string, m ObjectMeta) error {
	p, err := c.metaPath(bucket, key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func (c *StorageClient) removeObjectMeta(bucket, key string) error {
	p, err := c.metaPath(bucket, key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ----------------------------------------------------------------------------
// ERROR HELPERS
// ----------------------------------------------------------------------------

func IsNotFound(err error) bool      { return errors.Is(err, ErrNotFound) }
func IsAlreadyExists(err error) bool { return errors.Is(err, ErrAlreadyExists) }
func IsAccessDenied(err error) bool  { return errors.Is(err, ErrAccessDenied) }
