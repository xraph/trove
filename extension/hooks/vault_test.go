package hooks

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/xraph/vault"
	"github.com/xraph/vault/secret"
	"github.com/xraph/vault/store/memory"
)

// TestVaultKeyProviderReadsTheAppLevelSecret pins what vault v1.8.0's
// tenant-scoped GetSecret means for the encryption key: the provider reads
// the app-level secret (empty tenant) and nothing else. A key stored only
// under a tenant is not found.
func TestVaultKeyProviderReadsTheAppLevelSecret(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	provider := (&VaultHook{secrets: store}).KeyProvider("app")

	tenantKey := bytes.Repeat([]byte("t"), 32)
	if err := store.SetSecret(ctx, &secret.Secret{
		Key: "trove/data-encryption-key", AppID: "app", TenantID: "acme", EncryptedValue: tenantKey,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Key(ctx); !errors.Is(err, vault.ErrSecretNotFound) {
		t.Fatalf("Key with only a tenant-scoped secret = %v, want ErrSecretNotFound", err)
	}

	appKey := bytes.Repeat([]byte("a"), 32)
	if err := store.SetSecret(ctx, &secret.Secret{
		Key: "trove/data-encryption-key", AppID: "app", EncryptedValue: appKey,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := provider.Key(ctx)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	if !bytes.Equal(got, appKey) {
		t.Fatalf("Key returned the wrong secret")
	}
}
