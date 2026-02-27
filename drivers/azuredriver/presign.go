package azuredriver

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/sas"
)

// PresignGet generates a pre-signed URL (SAS token) for downloading an object.
func (d *AzureDriver) PresignGet(_ context.Context, bucket, key string, expires time.Duration) (string, error) {
	client, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	expiry := time.Now().UTC().Add(expires)
	permissions := sas.BlobPermissions{Read: true}

	url, err := client.ServiceClient().NewContainerClient(bucket).NewBlobClient(key).GetSASURL(permissions, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("azuredriver: presign get %q: %w", key, err)
	}

	return url, nil
}

// PresignPut generates a pre-signed URL (SAS token) for uploading an object.
func (d *AzureDriver) PresignPut(_ context.Context, bucket, key string, expires time.Duration) (string, error) {
	client, _, err := d.getClient()
	if err != nil {
		return "", err
	}

	expiry := time.Now().UTC().Add(expires)
	permissions := sas.BlobPermissions{Write: true, Create: true}

	url, err := client.ServiceClient().NewContainerClient(bucket).NewBlobClient(key).GetSASURL(permissions, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("azuredriver: presign put %q: %w", key, err)
	}

	return url, nil
}
