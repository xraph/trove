package s3driver

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/xraph/trove/driver"
)

// InitiateMultipart starts a multipart upload and returns the upload ID.
func (d *S3Driver) InitiateMultipart(ctx context.Context, bucket, key string, opts ...driver.PutOption) (string, error) {
	cfg := driver.ApplyPutOptions(opts...)
	client, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	ct := cfg.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	input := &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(ct),
	}

	if cfg.StorageClass != "" {
		input.StorageClass = types.StorageClass(cfg.StorageClass)
	}

	if len(cfg.Metadata) > 0 {
		input.Metadata = cfg.Metadata
	}

	result, err := client.CreateMultipartUpload(ctx, input)
	if err != nil {
		return "", fmt.Errorf("s3driver: initiate multipart %q: %w", key, err)
	}

	if result.UploadId == nil {
		return "", fmt.Errorf("s3driver: initiate multipart %q: nil upload ID", key)
	}

	return *result.UploadId, nil
}

// UploadPart uploads a single part of a multipart upload.
func (d *S3Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader) (*driver.PartInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	// Read part data to determine size.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("s3driver: read part data: %w", err)
	}

	result, err := client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(key),
		UploadId:   aws.String(uploadID),
		PartNumber: aws.Int32(int32(partNum)),
		Body:       strings.NewReader(string(data)),
	})
	if err != nil {
		return nil, fmt.Errorf("s3driver: upload part %d for %q: %w", partNum, key, err)
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	return &driver.PartInfo{
		PartNumber: partNum,
		ETag:       etag,
		Size:       int64(len(data)),
	}, nil
}

// CompleteMultipart completes a multipart upload.
func (d *S3Driver) CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []driver.PartInfo) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	s3Parts := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		s3Parts[i] = types.CompletedPart{
			PartNumber: aws.Int32(int32(p.PartNumber)),
			ETag:       aws.String(p.ETag),
		}
	}

	result, err := client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: s3Parts,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("s3driver: complete multipart %q: %w", key, err)
	}

	etag := ""
	if result.ETag != nil {
		etag = strings.Trim(*result.ETag, "\"")
	}

	// Calculate total size from parts.
	var totalSize int64
	for _, p := range parts {
		totalSize += p.Size
	}

	versionID := ""
	if result.VersionId != nil {
		versionID = *result.VersionId
	}

	return &driver.ObjectInfo{
		Key:       key,
		Size:      totalSize,
		ETag:      etag,
		VersionID: versionID,
	}, nil
}

// AbortMultipart cancels a multipart upload.
func (d *S3Driver) AbortMultipart(ctx context.Context, bucket, key, uploadID string) error {
	client, _, err := d.getClient()
	if err != nil {
		return err
	}

	_, err = client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("s3driver: abort multipart %q: %w", key, err)
	}
	return nil
}
