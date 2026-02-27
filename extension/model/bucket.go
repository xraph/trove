package model

import (
	"encoding/json"
	"time"

	"github.com/xraph/grove"
)

// Bucket represents a storage bucket with metadata.
type Bucket struct {
	grove.BaseModel `grove:"table:trove_buckets"`

	ID           string            `grove:"id,pk"              json:"id"               bson:"_id"`
	Name         string            `grove:"name,unique"        json:"name"             bson:"name"`
	Driver       string            `grove:"driver"             json:"driver"           bson:"driver"`
	Region       string            `grove:"region"             json:"region,omitempty" bson:"region,omitempty"`
	Versioning   bool              `grove:"versioning"         json:"versioning"       bson:"versioning"`
	CASEnabled   bool              `grove:"cas_enabled"        json:"cas_enabled"      bson:"cas_enabled"`
	Lifecycle    json.RawMessage   `grove:"lifecycle,type:jsonb" json:"lifecycle,omitempty" bson:"lifecycle,omitempty"`
	QuotaBytes   int64             `grove:"quota_bytes"        json:"quota_bytes"      bson:"quota_bytes"`
	QuotaObjects int64             `grove:"quota_objects"      json:"quota_objects"    bson:"quota_objects"`
	DefaultMeta  map[string]string `grove:"default_meta,type:jsonb" json:"default_meta,omitempty" bson:"default_meta,omitempty"`
	TenantKey    string            `grove:"tenant_key"         json:"tenant_key,omitempty" bson:"tenant_key,omitempty"`
	CreatedAt    time.Time         `grove:"created_at"         json:"created_at"       bson:"created_at"`
	UpdatedAt    time.Time         `grove:"updated_at"         json:"updated_at"       bson:"updated_at"`
}
