package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
)

// MinIOClient is a thin wrapper around the MinIO Go SDK.
// All methods are safe for concurrent use (the SDK client is too).
type MinIOClient struct {
	client *minio.Client
}

// Config holds MinIO client configuration.
//
// Endpoint must be host[:port] WITHOUT scheme (e.g. "minio:9000").
// UseSSL controls http vs https.
type Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Region    string // optional; "" lets the server decide
}

// NewClient creates and initializes a new MinIO client.
// It performs a health check (ListBuckets) to fail fast on bad credentials.
func NewClient(config Config, log *zap.Logger) (*MinIOClient, error) {
	if config.Endpoint == "" {
		return nil, errors.New("minio: endpoint is required")
	}
	if config.AccessKey == "" || config.SecretKey == "" {
		return nil, errors.New("minio: access key and secret key are required")
	}
	// Normalize accidental schemes — the SDK rejects them.
	endpoint := strings.TrimPrefix(config.Endpoint, "http://")
	endpoint = strings.TrimPrefix(endpoint, "https://")

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.UseSSL,
		Region: config.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}
	client.SetAppInfo("cdn-alpha-api", "1.0.0")

	// Fail fast on unreachable server / bad credentials.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if !client.IsOnline() {
		return nil, fmt.Errorf("minio: server at %s is not reachable", endpoint)
	}
	if _, err := client.ListBuckets(ctx); err != nil {
		return nil, fmt.Errorf("minio: health check failed: %w", err)
	}

	log.Info("MinIO connected",
		zap.String("endpoint", endpoint),
		zap.Bool("secure", config.UseSSL),
	)

	return &MinIOClient{client: client}, nil
}

// Client returns the underlying minio.Client.
func (c *MinIOClient) Client() *minio.Client {
	return c.client
}

// Close is a no-op kept for interface symmetry with NATS/Redis clients.
// The MinIO SDK uses a pooled net/http client whose connections are
// reclaimed by the runtime; there is nothing to drain.
func (c *MinIOClient) Close() {
	// intentionally empty
}

// Ping checks that MinIO is reachable and credentials are valid.
func (c *MinIOClient) Ping(ctx context.Context) error {
	if c.client == nil {
		return errors.New("minio client is nil")
	}
	if !c.client.IsOnline() {
		return errors.New("minio is not reachable")
	}
	_, err := c.client.ListBuckets(ctx)
	return err
}

// ----------------------------------------------------------------------------
// BUCKET OPERATIONS
// ----------------------------------------------------------------------------

// BucketExists reports whether the bucket exists and is accessible.
func (c *MinIOClient) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return c.client.BucketExists(ctx, bucket)
}

// MakeBucket creates a bucket. Idempotent via EnsureBucket.
func (c *MinIOClient) MakeBucket(ctx context.Context, bucket, region string) error {
	return c.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
}

// EnsureBucket creates the bucket if it does not exist. Safe under race:
// concurrent creation returning BucketAlreadyOwnedByYou is treated as success.
func (c *MinIOClient) EnsureBucket(ctx context.Context, bucket, region string) error {
	err := c.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region})
	if err == nil {
		return nil
	}
	var er minio.ErrorResponse
	if errors.As(err, &er) {
		switch er.Code {
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return nil
		}
	}
	return fmt.Errorf("minio.EnsureBucket(%s): %w", bucket, err)
}

// RemoveBucket deletes an empty bucket.
func (c *MinIOClient) RemoveBucket(ctx context.Context, bucket string) error {
	return c.client.RemoveBucket(ctx, bucket)
}

// ListBuckets returns all buckets the credentials can see.
func (c *MinIOClient) ListBuckets(ctx context.Context) ([]minio.BucketInfo, error) {
	return c.client.ListBuckets(ctx)
}

// SetPublicRead applies an anonymous GetObject policy to the bucket.
// Use for bucket.Visibility == "public".
func (c *MinIOClient) SetPublicRead(ctx context.Context, bucket string) error {
	policy := fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"AWS": ["*"]},
    "Action": ["s3:GetObject"],
    "Resource": ["arn:aws:s3:::%s/*"]
  }]
}`, bucket)
	return c.client.SetBucketPolicy(ctx, bucket, policy)
}

// SetPrivate removes any existing bucket policy.
// Use for bucket.Visibility == "private".
func (c *MinIOClient) SetPrivate(ctx context.Context, bucket string) error {
	return c.client.SetBucketPolicy(ctx, bucket, "")
}

// ----------------------------------------------------------------------------
// OBJECT OPERATIONS
// ----------------------------------------------------------------------------

// PutObjectOptions is a friendly subset of minio.PutObjectOptions that maps
// 1:1 to columns on the `files` table.
type PutObjectOptions struct {
	ContentType        string
	CacheControl       string
	ContentEncoding    string
	ContentDisposition string
	UserMetadata       map[string]string // e.g. {"original-filename": "photo.jpg"}
}

// PutObject uploads an object from reader. `size` must be exact (use
// PutObjectStreaming for unknown sizes). Returns the resulting UploadInfo,
// whose ETag should be persisted on the `files` row.
func (c *MinIOClient) PutObject(
	ctx context.Context,
	bucket, key string,
	reader io.Reader,
	size int64,
	opts PutObjectOptions,
) (minio.UploadInfo, error) {
	return c.client.PutObject(ctx, bucket, key, reader, size, minio.PutObjectOptions{
		ContentType:        opts.ContentType,
		CacheControl:       opts.CacheControl,
		ContentEncoding:    opts.ContentEncoding,
		ContentDisposition: opts.ContentDisposition,
		UserMetadata:       opts.UserMetadata,
	})
}

// PutObjectStreaming uploads without knowing the size upfront (slower — two
// passes over the stream). Only use when the size truly isn't known.
func (c *MinIOClient) PutObjectStreaming(
	ctx context.Context,
	bucket, key string,
	reader io.Reader,
	opts PutObjectOptions,
) (minio.UploadInfo, error) {
	return c.client.PutObject(ctx, bucket, key, reader, -1, minio.PutObjectOptions{
		ContentType:        opts.ContentType,
		CacheControl:       opts.CacheControl,
		ContentEncoding:    opts.ContentEncoding,
		ContentDisposition: opts.ContentDisposition,
		UserMetadata:       opts.UserMetadata,
		PartSize:           16 << 20, // 16 MiB
	})
}

// GetObject returns the raw object. The caller MUST close it.
// It does NOT stat first — use ObjectExists or StatObject if you need to
// guarantee existence before returning.
func (c *MinIOClient) GetObject(ctx context.Context, bucket, key string) (*minio.Object, error) {
	return c.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
}

// DownloadTo streams an object into w, verifying existence first.
func (c *MinIOClient) DownloadTo(
	ctx context.Context,
	bucket, key string,
	w io.Writer,
) (minio.ObjectInfo, error) {
	info, err := c.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return info, err
	}
	obj, err := c.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return info, err
	}
	defer obj.Close()

	if _, err := io.Copy(w, obj); err != nil {
		return info, fmt.Errorf("minio.DownloadTo(%s/%s): %w", bucket, key, err)
	}
	return info, nil
}

// StatObject returns metadata for an object.
func (c *MinIOClient) StatObject(ctx context.Context, bucket, key string) (minio.ObjectInfo, error) {
	return c.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
}

// ObjectExists reports whether the object exists.
func (c *MinIOClient) ObjectExists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// RemoveObject deletes a single object.
func (c *MinIOClient) RemoveObject(ctx context.Context, bucket, key string) error {
	return c.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

// RemoveObjects batch-deletes objects. Returns the first error encountered.
func (c *MinIOClient) RemoveObjects(ctx context.Context, bucket string, keys []string) error {
	infoCh := make(chan minio.ObjectInfo, len(keys))
	go func() {
		defer close(infoCh)
		for _, k := range keys {
			infoCh <- minio.ObjectInfo{Key: k}
		}
	}()
	for rmErr := range c.client.RemoveObjects(ctx, bucket, infoCh, minio.RemoveObjectsOptions{}) {
		if rmErr.Err != nil {
			return rmErr.Err
		}
	}
	return nil
}

// ListObjects returns objects in a bucket. prefix may be empty.
// Recursive=false treats "/" as a delimiter (S3-style directory listing).
func (c *MinIOClient) ListObjects(
	ctx context.Context,
	bucket, prefix string,
	recursive bool,
) ([]minio.ObjectInfo, error) {
	ch := c.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: recursive,
	})
	var out []minio.ObjectInfo
	for obj := range ch {
		if obj.Err != nil {
			return nil, obj.Err
		}
		out = append(out, obj)
	}
	return out, nil
}

// CopyObject copies an object within MinIO (server-side, no egress).
// Useful for bucket-to-bucket moves.
func (c *MinIOClient) CopyObject(
	ctx context.Context,
	dstBucket, dstKey, srcBucket, srcKey string,
) (minio.UploadInfo, error) {
	src := minio.CopySrcOptions{Bucket: srcBucket, Object: srcKey}
	dst := minio.CopyDestOptions{Bucket: dstBucket, Object: dstKey}
	return c.client.CopyObject(ctx, dst, src)
}

// ----------------------------------------------------------------------------
// PRESIGNED URLS
// ----------------------------------------------------------------------------

// PresignedGetObject returns a time-limited GET URL.
// reqParams may be nil; common keys are
// "response-content-type", "response-content-disposition", "response-cache-control".
func (c *MinIOClient) PresignedGetObject(
	ctx context.Context,
	bucket, key string,
	expiry time.Duration,
	reqParams url.Values,
) (*url.URL, error) {
	return c.client.PresignedGetObject(ctx, bucket, key, expiry, reqParams)
}

// PresignedGetAttachment builds a GET URL that forces the browser to download
// the object with the given filename.
func (c *MinIOClient) PresignedGetAttachment(
	ctx context.Context,
	bucket, key string,
	expiry time.Duration,
	filename string,
) (*url.URL, error) {
	params := url.Values{}
	params.Set("response-content-disposition",
		fmt.Sprintf(`attachment; filename=%q`, filename))
	return c.client.PresignedGetObject(ctx, bucket, key, expiry, params)
}

// PresignedPutObject returns a time-limited PUT URL for direct client uploads.
// The client must set Content-Type itself; to enforce content-type or size,
// use PresignedPostPolicy instead.
func (c *MinIOClient) PresignedPutObject(
	ctx context.Context,
	bucket, key string,
	expiry time.Duration,
) (*url.URL, error) {
	return c.client.PresignedPutObject(ctx, bucket, key, expiry)
}

// PresignedHeadObject returns a time-limited HEAD URL.
func (c *MinIOClient) PresignedHeadObject(
	ctx context.Context,
	bucket, key string,
	expiry time.Duration,
	reqParams url.Values,
) (*url.URL, error) {
	return c.client.PresignedHeadObject(ctx, bucket, key, expiry, reqParams)
}

// ----------------------------------------------------------------------------
// ERROR HELPERS
// ----------------------------------------------------------------------------

// IsNotFound reports whether err is a missing key/bucket.
func IsNotFound(err error) bool {
	var er minio.ErrorResponse
	if !errors.As(err, &er) {
		return false
	}
	switch er.Code {
	case "NoSuchKey", "NoSuchBucket", "NotFound", "NoSuchObject":
		return true
	}
	return er.StatusCode == 404
}

// IsAlreadyExists reports whether err is a duplicate-create conflict.
func IsAlreadyExists(err error) bool {
	var er minio.ErrorResponse
	if !errors.As(err, &er) {
		return false
	}
	switch er.Code {
	case "BucketAlreadyOwnedByYou", "BucketAlreadyExists", "PreconditionFailed":
		return true
	}
	return er.StatusCode == 409
}

// IsAccessDenied reports whether err is an authorization failure.
func IsAccessDenied(err error) bool {
	var er minio.ErrorResponse
	if !errors.As(err, &er) {
		return false
	}
	return er.Code == "AccessDenied" || er.StatusCode == 403
}
