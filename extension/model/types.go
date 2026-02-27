package model

import (
	"encoding/json"
	"time"
)

// UploadStatus represents the state of a multipart upload session.
type UploadStatus string

const (
	UploadStatusPending   UploadStatus = "pending"
	UploadStatusActive    UploadStatus = "active"
	UploadStatusCompleted UploadStatus = "completed"
	UploadStatusAborted   UploadStatus = "aborted"
	UploadStatusExpired   UploadStatus = "expired"
)

// LifecycleRule defines an automatic lifecycle transition or expiration.
type LifecycleRule struct {
	ID                  string `json:"id"`
	Prefix              string `json:"prefix,omitempty"`
	ExpirationDays      int    `json:"expiration_days,omitempty"`
	TransitionDays      int    `json:"transition_days,omitempty"`
	TransitionClass     string `json:"transition_class,omitempty"`
	NoncurrentDays      int    `json:"noncurrent_days,omitempty"`
	AbortIncompleteDays int    `json:"abort_incomplete_days,omitempty"`
}

// Checksum holds integrity verification data.
type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

// ChunkInfo tracks a single chunk in a multipart upload.
type ChunkInfo struct {
	Number    int       `json:"number"`
	Size      int64     `json:"size"`
	ETag      string    `json:"etag,omitempty"`
	Uploaded  bool      `json:"uploaded"`
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// MarshalJSON is a helper for LifecycleRule slices.
func MarshalLifecycleRules(rules []LifecycleRule) json.RawMessage {
	if len(rules) == 0 {
		return nil
	}
	data, _ := json.Marshal(rules)
	return data
}

// UnmarshalLifecycleRules parses lifecycle rules from JSON.
func UnmarshalLifecycleRules(data json.RawMessage) []LifecycleRule {
	if len(data) == 0 {
		return nil
	}
	var rules []LifecycleRule
	_ = json.Unmarshal(data, &rules)
	return rules
}
