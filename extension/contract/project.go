package contract

import (
	"time"

	"github.com/xraph/trove/driver"
)

// formatTime renders t as UTC RFC3339, and a zero time as null.
func formatTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

// optString is null for an empty string.
func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// objectRow is one object in a listing. storedSize is named for what it is:
// compress shrinks it and encrypt grows it, so it is not the size of what a
// download returns.
type objectRow struct {
	Key          string  `json:"key"`
	StoredSize   int64   `json:"storedSize"`
	ETag         *string `json:"etag"`
	LastModified *string `json:"lastModified"`
	ContentType  *string `json:"contentType"`
	StorageClass *string `json:"storageClass"`
}

func projectObjectRow(o driver.ObjectInfo) objectRow {
	return objectRow{
		Key:          o.Key,
		StoredSize:   o.Size,
		ETag:         optString(o.ETag),
		LastModified: formatTime(o.LastModified),
		ContentType:  optString(o.ContentType),
		StorageClass: optString(o.StorageClass),
	}
}

// objectDetail is what Head returns for one object.
type objectDetail struct {
	objectRow
	VersionID *string           `json:"versionId"`
	Metadata  map[string]string `json:"metadata"`
}

func projectObjectDetail(o *driver.ObjectInfo) objectDetail {
	var meta map[string]string
	if len(o.Metadata) > 0 {
		meta = make(map[string]string, len(o.Metadata))
		for k, v := range o.Metadata {
			meta[k] = v
		}
	}
	return objectDetail{
		objectRow: projectObjectRow(*o),
		VersionID: optString(o.VersionID),
		Metadata:  meta,
	}
}

// Facts about the six drivers, keyed by Driver.Name(). They come from
// reading each driver, recorded in the spec's findings.

// driverFolds reports whether a driver reports common prefixes when a
// listing sets a delimiter. All six do since trove slice 1.
func driverFolds(name string) bool {
	switch name {
	case "mem", "local", "sftp", "s3", "gcs", "azure":
		return true
	}
	return false
}

// etagIsContentHash reports whether a driver's ETag changes with content.
// mem's is the length in hex; local and sftp derive it from size and mtime.
func etagIsContentHash(name string) bool {
	switch name {
	case "s3", "gcs", "azure":
		return true
	}
	return false
}

// createdAtIsCreation reports whether ListBuckets returns a creation time.
// local and sftp return the directory's mtime, azure the container's
// last-modified time.
func createdAtIsCreation(name string) bool {
	switch name {
	case "mem", "s3", "gcs":
		return true
	}
	return false
}
