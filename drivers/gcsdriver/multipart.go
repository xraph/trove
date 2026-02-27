package gcsdriver

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"cloud.google.com/go/storage"

	"github.com/xraph/trove/driver"
)

// uploadState tracks the parts for an in-progress compose-based multipart upload.
type uploadState struct {
	bucket string
	key    string
	parts  map[int]*driver.PartInfo
}

var (
	uploadsMu sync.Mutex
	uploads   = make(map[string]*uploadState)
)

// InitiateMultipart starts a multipart upload. GCS uses a compose pattern:
// parts are uploaded as temporary objects and composed into the final object.
func (d *GCSDriver) InitiateMultipart(_ context.Context, bucket, key string, _ ...driver.PutOption) (string, error) {
	if _, _, err := d.getClient(); err != nil {
		return "", err
	}

	// Generate a unique upload ID.
	uploadID := fmt.Sprintf("%s/%s/%d", bucket, key, uniqueID())

	uploadsMu.Lock()
	uploads[uploadID] = &uploadState{
		bucket: bucket,
		key:    key,
		parts:  make(map[int]*driver.PartInfo),
	}
	uploadsMu.Unlock()

	return uploadID, nil
}

// UploadPart uploads a single part as a temporary GCS object.
func (d *GCSDriver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader) (*driver.PartInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	uploadsMu.Lock()
	state, ok := uploads[uploadID]
	uploadsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("gcsdriver: upload %q not found", uploadID)
	}

	// Upload as temporary object.
	partKey := fmt.Sprintf("%s.part.%d", key, partNum)
	obj := client.Bucket(bucket).Object(partKey)
	w := obj.NewWriter(ctx)

	data, err := io.ReadAll(r)
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("gcsdriver: read part %d data: %w", partNum, err)
	}

	if _, err := io.Copy(w, strings.NewReader(string(data))); err != nil {
		w.Close()
		return nil, fmt.Errorf("gcsdriver: upload part %d: %w", partNum, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("gcsdriver: close part %d writer: %w", partNum, err)
	}

	partInfo := &driver.PartInfo{
		PartNumber: partNum,
		ETag:       partKey,
		Size:       int64(len(data)),
	}

	uploadsMu.Lock()
	state.parts[partNum] = partInfo
	uploadsMu.Unlock()

	return partInfo, nil
}

// CompleteMultipart composes all part objects into the final object and cleans up.
func (d *GCSDriver) CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []driver.PartInfo) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	uploadsMu.Lock()
	state, ok := uploads[uploadID]
	if ok {
		delete(uploads, uploadID)
	}
	uploadsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("gcsdriver: upload %q not found", uploadID)
	}

	// Build source objects for compose.
	var sources []*storage.ObjectHandle
	for _, p := range parts {
		partKey := fmt.Sprintf("%s.part.%d", key, p.PartNumber)
		sources = append(sources, client.Bucket(bucket).Object(partKey))
	}

	// Compose into the final object.
	dst := client.Bucket(bucket).Object(key)
	composer := dst.ComposerFrom(sources...)

	attrs, err := composer.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcsdriver: compose %q: %w", key, err)
	}

	// Clean up temporary part objects.
	for _, p := range parts {
		partKey := fmt.Sprintf("%s.part.%d", key, p.PartNumber)
		client.Bucket(bucket).Object(partKey).Delete(ctx) //nolint:errcheck
	}

	// Calculate total size.
	var totalSize int64
	for _, p := range state.parts {
		totalSize += p.Size
	}

	return &driver.ObjectInfo{
		Key:          key,
		Size:         totalSize,
		ContentType:  attrs.ContentType,
		ETag:         attrs.Etag,
		LastModified: attrs.Updated,
		StorageClass: attrs.StorageClass,
	}, nil
}

// AbortMultipart cancels a multipart upload and cleans up temporary objects.
func (d *GCSDriver) AbortMultipart(ctx context.Context, bucket, key, uploadID string) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	uploadsMu.Lock()
	state, ok := uploads[uploadID]
	if ok {
		delete(uploads, uploadID)
	}
	uploadsMu.Unlock()
	if !ok {
		return fmt.Errorf("gcsdriver: upload %q not found", uploadID)
	}

	// Clean up temporary part objects.
	for _, p := range state.parts {
		partKey := fmt.Sprintf("%s.part.%d", key, p.PartNumber)
		client.Bucket(bucket).Object(partKey).Delete(ctx) //nolint:errcheck
	}

	return nil
}

// --- Upload ID generation ---

var (
	idMu      sync.Mutex
	idCounter int64
)

func uniqueID() int64 {
	idMu.Lock()
	defer idMu.Unlock()
	idCounter++
	return idCounter
}
