package hooks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/xraph/forge"
	vaultstore "github.com/xraph/vault/store"
	"github.com/xraph/vessel"

	"github.com/xraph/trove/middleware/encrypt"
)

// VaultHook auto-configures encryption middleware with Vault-managed keys.
type VaultHook struct {
	secrets vaultstore.Store
	logger  *slog.Logger
}

// NewVaultHook creates a Vault hook, auto-discovering Vault from DI.
// Returns nil if Vault is not available.
func NewVaultHook(fapp forge.App, logger *slog.Logger) *VaultHook {
	s, err := vessel.Inject[vaultstore.Store](fapp.Container())
	if err != nil {
		if logger != nil {
			logger.Debug("vault not available, skipping encryption auto-config")
		}
		return nil
	}

	return &VaultHook{
		secrets: s,
		logger:  logger,
	}
}

// KeyProvider returns a KeyProvider backed by Vault secrets.
func (h *VaultHook) KeyProvider(appID string) encrypt.KeyProvider {
	if h == nil {
		return nil
	}
	return &vaultKeyProvider{
		store: h.secrets,
		appID: appID,
		key:   "trove/data-encryption-key",
	}
}

// vaultKeyProvider retrieves encryption keys from Vault.
type vaultKeyProvider struct {
	store vaultstore.Store
	appID string
	key   string
}

// Key retrieves the encryption key from Vault.
func (p *vaultKeyProvider) Key(ctx context.Context) ([]byte, error) {
	secret, err := p.store.GetSecret(ctx, p.key, p.appID)
	if err != nil {
		return nil, fmt.Errorf("vault: get encryption key: %w", err)
	}

	// EncryptedValue holds the persisted bytes. When vault-level encryption
	// is disabled, this is the plaintext key material.
	return secret.EncryptedValue, nil
}
