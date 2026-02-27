// Package migrate provides Grove modular migrations for Trove tables.
package migrate

import (
	"context"

	"github.com/xraph/grove/migrate"
)

// Migrations is the Trove migration group.
var Migrations = migrate.NewGroup("trove")

func init() {
	Migrations.MustRegister(
		&migrate.Migration{
			Name:    "create_trove_buckets",
			Version: "20240101000001",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trove_buckets (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE,
    driver          TEXT NOT NULL DEFAULT '',
    region          TEXT NOT NULL DEFAULT '',
    versioning      BOOLEAN NOT NULL DEFAULT FALSE,
    cas_enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    lifecycle       JSONB,
    quota_bytes     BIGINT NOT NULL DEFAULT 0,
    quota_objects   BIGINT NOT NULL DEFAULT 0,
    default_meta    JSONB,
    tenant_key      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trove_buckets_tenant ON trove_buckets (tenant_key);
CREATE INDEX IF NOT EXISTS idx_trove_buckets_name ON trove_buckets (name);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS trove_buckets CASCADE`)
				return err
			},
		},

		&migrate.Migration{
			Name:    "create_trove_objects",
			Version: "20240101000002",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trove_objects (
    id              TEXT PRIMARY KEY,
    bucket_id       TEXT NOT NULL REFERENCES trove_buckets(id) ON DELETE CASCADE,
    key             TEXT NOT NULL,
    size            BIGINT NOT NULL DEFAULT 0,
    content_type    TEXT NOT NULL DEFAULT 'application/octet-stream',
    etag            TEXT NOT NULL DEFAULT '',
    checksum_alg    TEXT NOT NULL DEFAULT '',
    checksum_val    TEXT NOT NULL DEFAULT '',
    metadata        JSONB,
    tags            JSONB,
    driver          TEXT NOT NULL DEFAULT '',
    storage_class   TEXT NOT NULL DEFAULT '',
    version_id      TEXT NOT NULL DEFAULT '',
    tenant_key      TEXT NOT NULL DEFAULT '',
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_trove_objects_bucket_key ON trove_objects (bucket_id, key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_trove_objects_tenant ON trove_objects (tenant_key);
CREATE INDEX IF NOT EXISTS idx_trove_objects_deleted ON trove_objects (deleted_at) WHERE deleted_at IS NOT NULL;
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS trove_objects CASCADE`)
				return err
			},
		},

		&migrate.Migration{
			Name:    "create_trove_upload_sessions",
			Version: "20240101000003",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trove_upload_sessions (
    id              TEXT PRIMARY KEY,
    bucket_id       TEXT NOT NULL REFERENCES trove_buckets(id) ON DELETE CASCADE,
    object_key      TEXT NOT NULL,
    content_type    TEXT NOT NULL DEFAULT 'application/octet-stream',
    status          TEXT NOT NULL DEFAULT 'pending',
    total_parts     INT NOT NULL DEFAULT 0,
    uploaded_parts  INT NOT NULL DEFAULT 0,
    total_size      BIGINT NOT NULL DEFAULT 0,
    chunks          JSONB,
    metadata        JSONB,
    tenant_key      TEXT NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trove_uploads_expires ON trove_upload_sessions (expires_at) WHERE status IN ('pending', 'active');
CREATE INDEX IF NOT EXISTS idx_trove_uploads_bucket ON trove_upload_sessions (bucket_id);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS trove_upload_sessions CASCADE`)
				return err
			},
		},

		&migrate.Migration{
			Name:    "create_trove_cas_index",
			Version: "20240101000004",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trove_cas_index (
    hash        TEXT PRIMARY KEY,
    bucket_id   TEXT NOT NULL DEFAULT '',
    key         TEXT NOT NULL DEFAULT '',
    size        BIGINT NOT NULL DEFAULT 0,
    ref_count   INT NOT NULL DEFAULT 0,
    pinned      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trove_cas_gc ON trove_cas_index (pinned, ref_count) WHERE pinned = FALSE AND ref_count = 0;
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS trove_cas_index CASCADE`)
				return err
			},
		},

		&migrate.Migration{
			Name:    "create_trove_quotas",
			Version: "20240101000005",
			Up: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `
CREATE TABLE IF NOT EXISTS trove_quotas (
    tenant_key      TEXT PRIMARY KEY,
    used_bytes      BIGINT NOT NULL DEFAULT 0,
    limit_bytes     BIGINT NOT NULL DEFAULT 0,
    object_count    BIGINT NOT NULL DEFAULT 0,
    limit_objects   BIGINT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`)
				return err
			},
			Down: func(ctx context.Context, exec migrate.Executor) error {
				_, err := exec.Exec(ctx, `DROP TABLE IF EXISTS trove_quotas CASCADE`)
				return err
			},
		},
	)
}
