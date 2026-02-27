package model

import (
	"encoding/json"
	"time"

	"github.com/xraph/grove"
)

// UploadSession represents a multipart upload session.
type UploadSession struct {
	grove.BaseModel `grove:"table:trove_upload_sessions"`

	ID            string            `grove:"id,pk"              json:"id"               bson:"_id"`
	BucketID      string            `grove:"bucket_id"          json:"bucket_id"        bson:"bucket_id"`
	ObjectKey     string            `grove:"object_key"         json:"object_key"       bson:"object_key"`
	ContentType   string            `grove:"content_type"       json:"content_type"     bson:"content_type"`
	Status        UploadStatus      `grove:"status"             json:"status"           bson:"status"`
	TotalParts    int               `grove:"total_parts"        json:"total_parts"      bson:"total_parts"`
	UploadedParts int               `grove:"uploaded_parts"     json:"uploaded_parts"   bson:"uploaded_parts"`
	TotalSize     int64             `grove:"total_size"         json:"total_size"       bson:"total_size"`
	Chunks        json.RawMessage   `grove:"chunks,type:jsonb"  json:"chunks,omitempty" bson:"chunks,omitempty"`
	Metadata      map[string]string `grove:"metadata,type:jsonb" json:"metadata,omitempty" bson:"metadata,omitempty"`
	TenantKey     string            `grove:"tenant_key"         json:"tenant_key,omitempty" bson:"tenant_key,omitempty"`
	ExpiresAt     time.Time         `grove:"expires_at"         json:"expires_at"       bson:"expires_at"`
	CreatedAt     time.Time         `grove:"created_at"         json:"created_at"       bson:"created_at"`
	UpdatedAt     time.Time         `grove:"updated_at"         json:"updated_at"       bson:"updated_at"`
}
