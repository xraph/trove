package driver

import (
	"context"
	"io"
	"time"
)

// MultipartDriver extends Driver with multipart upload support.
type MultipartDriver interface {
	Driver
	InitiateMultipart(ctx context.Context, bucket, key string, opts ...PutOption) (uploadID string, err error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader) (*PartInfo, error)
	CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []PartInfo) (*ObjectInfo, error)
	AbortMultipart(ctx context.Context, bucket, key, uploadID string) error
}

// PartInfo holds metadata about an uploaded part.
type PartInfo struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// PresignDriver extends Driver with pre-signed URL generation.
type PresignDriver interface {
	Driver
	PresignGet(ctx context.Context, bucket, key string, expires time.Duration) (string, error)
	PresignPut(ctx context.Context, bucket, key string, expires time.Duration) (string, error)
}

// ServerCopyDriver extends Driver with server-side copy (avoids download+reupload).
type ServerCopyDriver interface {
	Driver
	ServerCopy(ctx context.Context, src, dst ObjectRef) (*ObjectInfo, error)
}

// ObjectRef identifies an object by bucket and key.
type ObjectRef struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

// VersioningDriver extends Driver with version management.
type VersioningDriver interface {
	Driver
	ListVersions(ctx context.Context, bucket, key string) ([]VersionInfo, error)
	GetVersion(ctx context.Context, bucket, key, versionID string) (*ObjectReader, error)
	DeleteVersion(ctx context.Context, bucket, key, versionID string) error
	RestoreVersion(ctx context.Context, bucket, key, versionID string) (*ObjectInfo, error)
}

// VersionInfo holds metadata about an object version.
type VersionInfo struct {
	VersionID    string    `json:"version_id"`
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
	IsLatest     bool      `json:"is_latest"`
}

// NotificationDriver extends Driver with change notification support.
type NotificationDriver interface {
	Driver
	Watch(ctx context.Context, bucket string, opts ...WatchOption) (<-chan ObjectEvent, error)
}

// WatchOption configures a Watch operation.
type WatchOption func(*WatchConfig)

// WatchConfig holds options for a Watch operation.
type WatchConfig struct {
	Prefix     string
	EventTypes []ObjectEventType
}

// ObjectEventType identifies the type of object change event.
type ObjectEventType string

const (
	EventCreated ObjectEventType = "object.created"
	EventDeleted ObjectEventType = "object.deleted"
	EventUpdated ObjectEventType = "object.updated"
)

// ObjectEvent represents a change notification.
type ObjectEvent struct {
	Type   ObjectEventType `json:"type"`
	Bucket string          `json:"bucket"`
	Key    string          `json:"key"`
	Info   *ObjectInfo     `json:"info,omitempty"`
}

// LifecycleDriver extends Driver with lifecycle management.
type LifecycleDriver interface {
	Driver
	SetLifecycle(ctx context.Context, bucket string, rules []LifecycleRule) error
	GetLifecycle(ctx context.Context, bucket string) ([]LifecycleRule, error)
}

// LifecycleRule defines a storage lifecycle transition or expiration rule.
type LifecycleRule struct {
	ID                string `json:"id"`
	Prefix            string `json:"prefix"`
	ExpirationDays    int    `json:"expiration_days,omitempty"`
	TransitionDays    int    `json:"transition_days,omitempty"`
	TransitionStorage string `json:"transition_storage,omitempty"`
	Enabled           bool   `json:"enabled"`
}

// RangeDriver extends Driver with byte-range read support. This enables
// resumable downloads and partial content retrieval.
type RangeDriver interface {
	Driver
	// GetRange retrieves a byte range of an object. The offset is the
	// starting byte position and length is the number of bytes to read.
	// A length of -1 reads from offset to end.
	GetRange(ctx context.Context, bucket, key string, offset, length int64) (*ObjectReader, error)
}
