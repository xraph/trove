package model

import (
	"time"

	"github.com/xraph/grove"
)

// Quota tracks storage usage per tenant.
type Quota struct {
	grove.BaseModel `grove:"table:trove_quotas"`

	TenantKey    string    `grove:"tenant_key,pk"  json:"tenant_key"    bson:"_id"`
	UsedBytes    int64     `grove:"used_bytes"     json:"used_bytes"    bson:"used_bytes"`
	LimitBytes   int64     `grove:"limit_bytes"    json:"limit_bytes"   bson:"limit_bytes"`
	ObjectCount  int64     `grove:"object_count"   json:"object_count"  bson:"object_count"`
	LimitObjects int64     `grove:"limit_objects"  json:"limit_objects" bson:"limit_objects"`
	UpdatedAt    time.Time `grove:"updated_at"  json:"updated_at"    bson:"updated_at"`
}
