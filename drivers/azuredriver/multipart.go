package azuredriver

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/streaming"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"

	"github.com/xraph/trove/driver"
)

// blockUploadState tracks the blocks for an in-progress block blob upload.
type blockUploadState struct {
	bucket   string
	key      string
	blockIDs []string
	parts    map[int]*driver.PartInfo
}

var (
	blockUploadsMu sync.Mutex
	blockUploads   = make(map[string]*blockUploadState)
)

// InitiateMultipart starts a block blob upload. Azure uses a stage-then-commit
// pattern: blocks are staged individually, then committed as a block list.
func (d *AzureDriver) InitiateMultipart(_ context.Context, bucket, key string, _ ...driver.PutOption) (string, error) {
	if _, _, err := d.getClient(); err != nil {
		return "", err
	}

	uploadID := fmt.Sprintf("%s/%s/%d", bucket, key, blockUniqueID())

	blockUploadsMu.Lock()
	blockUploads[uploadID] = &blockUploadState{
		bucket: bucket,
		key:    key,
		parts:  make(map[int]*driver.PartInfo),
	}
	blockUploadsMu.Unlock()

	return uploadID, nil
}

// UploadPart stages a block for the block blob.
func (d *AzureDriver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader) (*driver.PartInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	blockUploadsMu.Lock()
	state, ok := blockUploads[uploadID]
	blockUploadsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("azuredriver: upload %q not found", uploadID)
	}

	// Block IDs must be base64-encoded and all the same length.
	blockID := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("block-%08d", partNum)))

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: read part %d data: %w", partNum, err)
	}

	bbClient := client.ServiceClient().NewContainerClient(bucket).NewBlockBlobClient(key)

	_, err = bbClient.StageBlock(ctx, blockID, streaming.NopCloser(bytes.NewReader(data)), nil)
	if err != nil {
		return nil, fmt.Errorf("azuredriver: stage block %d: %w", partNum, err)
	}

	partInfo := &driver.PartInfo{
		PartNumber: partNum,
		ETag:       blockID,
		Size:       int64(len(data)),
	}

	blockUploadsMu.Lock()
	state.blockIDs = append(state.blockIDs, blockID)
	state.parts[partNum] = partInfo
	blockUploadsMu.Unlock()

	return partInfo, nil
}

// CompleteMultipart commits the block list to form the final blob.
func (d *AzureDriver) CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []driver.PartInfo) (*driver.ObjectInfo, error) {
	client, _, err := d.getClient()
	if err != nil {
		return nil, err
	}

	blockUploadsMu.Lock()
	state, ok := blockUploads[uploadID]
	if ok {
		delete(blockUploads, uploadID)
	}
	blockUploadsMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("azuredriver: upload %q not found", uploadID)
	}

	// Build block ID list in order.
	blockIDs := make([]string, len(parts))
	for i, p := range parts {
		blockIDs[i] = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("block-%08d", p.PartNumber)))
	}

	bbClient := client.ServiceClient().NewContainerClient(bucket).NewBlockBlobClient(key)

	_, err = bbClient.CommitBlockList(ctx, blockIDs, &blockblob.CommitBlockListOptions{})
	if err != nil {
		return nil, fmt.Errorf("azuredriver: commit block list %q: %w", key, err)
	}

	var totalSize int64
	for _, p := range state.parts {
		totalSize += p.Size
	}

	return &driver.ObjectInfo{
		Key:  key,
		Size: totalSize,
	}, nil
}

// AbortMultipart cancels a block blob upload. Uncommitted blocks are
// automatically cleaned up by Azure after a retention period.
func (d *AzureDriver) AbortMultipart(_ context.Context, _, _, uploadID string) error {
	if _, _, err := d.getClient(); err != nil {
		return err
	}

	blockUploadsMu.Lock()
	delete(blockUploads, uploadID)
	blockUploadsMu.Unlock()

	return nil
}

// --- Block upload ID generation ---

var (
	blockIDMu      sync.Mutex
	blockIDCounter int64
)

func blockUniqueID() int64 {
	blockIDMu.Lock()
	defer blockIDMu.Unlock()
	blockIDCounter++
	return blockIDCounter
}
