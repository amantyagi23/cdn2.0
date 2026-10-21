// Package workers contains background NATS JetStream consumers.
//
// # File cleanup worker
//
// Listens on subject "file.delete" (stream CDN_FILES).
// When a file is marked `deleting` by the API, the handler publishes a message
// to this subject.  The worker removes the object from MinIO and then marks the
// DB record as `deleted`.
package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	natsclient "github.com/amantyagi23/cdn-network/internal/infrastructure/nats"
	storageClient "github.com/amantyagi23/cdn-network/internal/infrastructure/storage"
	"github.com/amantyagi23/cdn-network/internal/repos"
	"github.com/google/uuid"
	natsgo "github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	fileDeleteStream   = "CDN_FILES"
	fileDeleteSubject  = "file.delete"
	fileDeleteConsumer = "file-cleanup"
)

// FileDeleteMsg is the payload published when a file is marked for deletion.
type FileDeleteMsg struct {
	FileID    string `json:"fileId"`
	ObjectKey string `json:"objectKey"`
}

// FileCleanupWorker consumes file.delete events and purges MinIO objects.
type FileCleanupWorker struct {
	nats       *natsclient.NATSClient
	storage    *storageClient.StorageClient
	fileRepo   *repos.FileRepository
	bucketRepo *repos.BucketRepository
	log        *zap.Logger
	sub        *natsgo.Subscription
}

func NewFileCleanupWorker(
	nats *natsclient.NATSClient,
	storage *storageClient.StorageClient,
	fileRepo *repos.FileRepository,
	bucketRepo *repos.BucketRepository,
	log *zap.Logger,
) *FileCleanupWorker {
	return &FileCleanupWorker{
		nats:       nats,
		storage:    storage,
		fileRepo:   fileRepo,
		bucketRepo: bucketRepo,
		log:        log,
	}
}

// Start creates the JetStream stream (idempotent) and begins consuming.
func (w *FileCleanupWorker) Start(ctx context.Context) error {
	js := w.nats.JetStream()

	// Ensure the stream exists (idempotent — returns existing info if already there).
	_, err := js.AddStream(&natsgo.StreamConfig{
		Name:      fileDeleteStream,
		Subjects:  []string{fileDeleteSubject},
		Retention: natsgo.WorkQueuePolicy,
		Storage:   natsgo.FileStorage,
		MaxAge:    24 * time.Hour,
	})
	if err != nil {
		return fmt.Errorf("FileCleanupWorker: ensure stream: %w", err)
	}

	sub, err := js.QueueSubscribe(fileDeleteSubject, fileDeleteConsumer, func(msg *natsgo.Msg) {
		if err := w.handle(ctx, msg); err != nil {
			w.log.Error("FileCleanupWorker: handle error", zap.Error(err))
			// Nak so NATS retries the message.
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, natsgo.Durable(fileDeleteConsumer), natsgo.AckExplicit())
	if err != nil {
		return fmt.Errorf("FileCleanupWorker: subscribe: %w", err)
	}

	w.sub = sub
	w.log.Info("FileCleanupWorker started", zap.String("subject", fileDeleteSubject))

	// Block until context is cancelled.
	<-ctx.Done()
	w.log.Info("FileCleanupWorker stopping")
	return sub.Unsubscribe()
}

func (w *FileCleanupWorker) handle(ctx context.Context, msg *natsgo.Msg) error {
	var payload FileDeleteMsg
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	fileID, err := uuid.Parse(payload.FileID)
	if err != nil {
		return fmt.Errorf("invalid file id %q: %w", payload.FileID, err)
	}

	// Fetch the file to get the canonical BucketID, then resolve MinIO bucket name.
	f, err := w.fileRepo.GetByID(ctx, fileID)
	if err != nil {
		return fmt.Errorf("GetByID(%s): %w", fileID, err)
	}
	b, err := w.bucketRepo.GetByID(ctx, f.BucketID)
	if err != nil {
		return fmt.Errorf("bucketRepo.GetByID(%s): %w", f.BucketID, err)
	}

	// Remove object from storage using the real bucket name.
	if err := w.storage.RemoveObject(ctx, b.StorageName, f.ObjectKey); err != nil {
		if !storageClient.IsNotFound(err) {
			return fmt.Errorf("RemoveObject(%s/%s): %w", b.StorageName, f.ObjectKey, err)
		}
		// Already gone — proceed to mark DB record deleted.
	}

	// Mark the DB record as deleted.
	if err := f.MarkDeleted(); err != nil {
		return fmt.Errorf("MarkDeleted: %w", err)
	}
	if err := w.fileRepo.Update(ctx, f); err != nil {
		return fmt.Errorf("Update: %w", err)
	}

	w.log.Info("file purged from MinIO and marked deleted",
		zap.String("fileId", payload.FileID),
		zap.String("objectKey", f.ObjectKey),
	)
	return nil
}

// PublishFileDelete publishes a deletion event so the worker picks it up.
// Call this from the file handler after marking a file as `deleting`.
func PublishFileDelete(ctx context.Context, nats *natsclient.NATSClient, fileID, objectKey string) error {
	payload := FileDeleteMsg{FileID: fileID, ObjectKey: objectKey}
	_, err := nats.PublishPersistentJSON(ctx, fileDeleteSubject, payload)
	return err
}
