package model

import (
	"time"

	"github.com/xraph/grove"
)

// CASEntry represents a CAS index entry in the database.
type CASEntry struct {
	grove.BaseModel `grove:"table:trove_cas_index"`

	Hash      string    `grove:"hash,pk"   json:"hash"      bson:"_id"`
	BucketID  string    `grove:"bucket_id" json:"bucket_id"  bson:"bucket_id"`
	Key       string    `grove:"key"       json:"key"        bson:"key"`
	Size      int64     `grove:"size"      json:"size"       bson:"size"`
	RefCount  int       `grove:"ref_count" json:"ref_count"   bson:"ref_count"`
	Pinned    bool      `grove:"pinned"    json:"pinned"     bson:"pinned"`
	CreatedAt time.Time `grove:"created_at" json:"created_at" bson:"created_at"`
}
