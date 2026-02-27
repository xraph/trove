package model

import (
	"time"

	"github.com/xraph/grove"
)

// Object represents a stored object with metadata.
type Object struct {
	grove.BaseModel `grove:"table:trove_objects"`

	ID           string            `grove:"id,pk"              json:"id"                bson:"_id"`
	BucketID     string            `grove:"bucket_id"          json:"bucket_id"         bson:"bucket_id"`
	Key          string            `grove:"key"                json:"key"               bson:"key"`
	Size         int64             `grove:"size"               json:"size"              bson:"size"`
	ContentType  string            `grove:"content_type"       json:"content_type"      bson:"content_type"`
	ETag         string            `grove:"etag"               json:"etag"              bson:"etag"`
	ChecksumAlg  string            `grove:"checksum_alg"       json:"checksum_alg,omitempty"  bson:"checksum_alg,omitempty"`
	ChecksumVal  string            `grove:"checksum_val"       json:"checksum_val,omitempty"  bson:"checksum_val,omitempty"`
	Metadata     map[string]string `grove:"metadata,type:jsonb" json:"metadata,omitempty" bson:"metadata,omitempty"`
	Tags         map[string]string `grove:"tags,type:jsonb"    json:"tags,omitempty"    bson:"tags,omitempty"`
	Driver       string            `grove:"driver"             json:"driver"            bson:"driver"`
	StorageClass string            `grove:"storage_class"      json:"storage_class,omitempty" bson:"storage_class,omitempty"`
	VersionID    string            `grove:"version_id"         json:"version_id,omitempty" bson:"version_id,omitempty"`
	TenantKey    string            `grove:"tenant_key"         json:"tenant_key,omitempty" bson:"tenant_key,omitempty"`
	DeletedAt    *time.Time        `grove:"deleted_at"         json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`
	CreatedAt    time.Time         `grove:"created_at"         json:"created_at"        bson:"created_at"`
	UpdatedAt    time.Time         `grove:"updated_at"         json:"updated_at"        bson:"updated_at"`
}
