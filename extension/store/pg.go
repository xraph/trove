package store

import (
	"context"
	"fmt"

	"github.com/xraph/grove"
	"github.com/xraph/grove/drivers/pgdriver"

	"github.com/xraph/trove/extension/model"
)

// tryUnwrapPg attempts to unwrap a PostgreSQL driver from a Grove DB.
func tryUnwrapPg(db *grove.DB) (*pgdriver.PgDB, bool) {
	defer func() { recover() }() // pgdriver.Unwrap panics if not pg
	pg := pgdriver.Unwrap(db)
	return pg, pg != nil
}

// --- Internal query helpers (PostgreSQL-optimized) ---

func (s *Store) insert(ctx context.Context, model any) error {
	if s.pg != nil {
		_, err := s.pg.NewInsert(model).Exec(ctx)
		return err
	}
	_, err := s.db.NewInsert(model).(interface {
		Exec(context.Context) (any, error)
	}).Exec(ctx)
	return err
}

func (s *Store) update(ctx context.Context, model any) error {
	if s.pg != nil {
		_, err := s.pg.NewUpdate(model).WherePK().Exec(ctx)
		return err
	}
	return fmt.Errorf("store: update not supported for this driver")
}

func (s *Store) upsert(ctx context.Context, model any, conflictColumn string) error {
	if s.pg != nil {
		_, err := s.pg.NewInsert(model).
			OnConflict("(" + conflictColumn + ") DO UPDATE").
			Exec(ctx)
		return err
	}
	return s.insert(ctx, model)
}

func (s *Store) findByPK(ctx context.Context, model any, pk any) error {
	if s.pg != nil {
		return s.pg.NewSelect(model).Where("id = ?", pk).Scan(ctx)
	}
	return fmt.Errorf("store: find not supported for this driver")
}

func (s *Store) findByField(ctx context.Context, model any, field string, value any) error {
	if s.pg != nil {
		return s.pg.NewSelect(model).Where(field+" = ?", value).Limit(1).Scan(ctx)
	}
	return fmt.Errorf("store: find not supported for this driver")
}

func (s *Store) findByFields(ctx context.Context, model any, fields map[string]any) error {
	if s.pg != nil {
		q := s.pg.NewSelect(model)
		for k, v := range fields {
			if v == nil {
				q = q.Where(k + " IS NULL")
			} else {
				q = q.Where(k+" = ?", v)
			}
		}
		return q.Limit(1).Scan(ctx)
	}
	return fmt.Errorf("store: find not supported for this driver")
}

func (s *Store) deleteByPK(ctx context.Context, model any, pk any) error {
	if s.pg != nil {
		_, err := s.pg.NewDelete(model).Where("id = ?", pk).Exec(ctx)
		return err
	}
	return fmt.Errorf("store: delete not supported for this driver")
}

func (s *Store) updateFields(ctx context.Context, model any, pk any, fields map[string]any) error {
	if s.pg != nil {
		q := s.pg.NewUpdate(model).Where("id = ?", pk)
		for k, v := range fields {
			q = q.Set(k+" = ?", v)
		}
		_, err := q.Exec(ctx)
		return err
	}
	return fmt.Errorf("store: update fields not supported for this driver")
}

func (s *Store) updateFieldsByColumn(ctx context.Context, model any, column string, value any, fields map[string]any) error {
	if s.pg != nil {
		q := s.pg.NewUpdate(model).Where(column+" = ?", value)
		for k, v := range fields {
			q = q.Set(k+" = ?", v)
		}
		_, err := q.Exec(ctx)
		return err
	}
	return fmt.Errorf("store: update fields by column not supported for this driver")
}

func (s *Store) deleteByColumn(ctx context.Context, model any, column string, value any) error {
	if s.pg != nil {
		_, err := s.pg.NewDelete(model).Where(column+" = ?", value).Exec(ctx)
		return err
	}
	return fmt.Errorf("store: delete by column not supported for this driver")
}

func (s *Store) incrementField(ctx context.Context, model any, pk any, field string) error {
	if s.pg != nil {
		_, err := s.pg.NewUpdate(model).
			Where("hash = ?", pk).
			Set(field + " = " + field + " + 1").
			Exec(ctx)
		return err
	}
	return fmt.Errorf("store: increment not supported for this driver")
}

func (s *Store) decrementField(ctx context.Context, model any, pk any, field string) error {
	if s.pg != nil {
		_, err := s.pg.NewUpdate(model).
			Where("hash = ?", pk).
			Set(field + " = GREATEST(" + field + " - 1, 0)").
			Exec(ctx)
		return err
	}
	return fmt.Errorf("store: decrement not supported for this driver")
}

func (s *Store) listByTenant(ctx context.Context, dest any, table, tenantKey string) error {
	if s.pg != nil {
		q := s.pg.NewSelect(dest)
		if tenantKey != "" {
			q = q.Where("tenant_key = ?", tenantKey)
		}
		q = q.OrderExpr("created_at DESC")
		return q.Scan(ctx)
	}
	return fmt.Errorf("store: list not supported for this driver")
}

func (s *Store) listObjects(ctx context.Context, dest *[]*model.Object, bucketID string, cfg listConfig) error {
	if s.pg != nil {
		q := s.pg.NewSelect(dest).
			Where("bucket_id = ?", bucketID).
			Where("deleted_at IS NULL")
		if cfg.prefix != "" {
			q = q.Where("key LIKE ?", cfg.prefix+"%")
		}
		if cfg.tenantKey != "" {
			q = q.Where("tenant_key = ?", cfg.tenantKey)
		}
		q = q.OrderExpr("key ASC").Limit(cfg.limit).Offset(cfg.offset)
		return q.Scan(ctx)
	}
	return fmt.Errorf("store: list objects not supported for this driver")
}

func (s *Store) listExpired(ctx context.Context, dest any, table string, now interface{}) error {
	if s.pg != nil {
		return s.pg.NewSelect(dest).
			Where("expires_at < ?", now).
			Where("status IN (?, ?)", model.UploadStatusPending, model.UploadStatusActive).
			Scan(ctx)
	}
	return fmt.Errorf("store: list expired not supported for this driver")
}

func (s *Store) listUnpinned(ctx context.Context, dest *[]*model.CASEntry) error {
	if s.pg != nil {
		return s.pg.NewSelect(dest).
			Where("pinned = ?", false).
			Where("ref_count = ?", 0).
			Scan(ctx)
	}
	return fmt.Errorf("store: list unpinned not supported for this driver")
}

func (s *Store) listAll(ctx context.Context, dest any) error {
	if s.pg != nil {
		return s.pg.NewSelect(dest).
			OrderExpr("created_at DESC").
			Scan(ctx)
	}
	return fmt.Errorf("store: list all not supported for this driver")
}

func (s *Store) listAllWithLimit(ctx context.Context, dest any, limit int) error {
	if s.pg != nil {
		return s.pg.NewSelect(dest).
			Where("deleted_at IS NULL").
			OrderExpr("created_at DESC").
			Limit(limit).
			Scan(ctx)
	}
	return fmt.Errorf("store: list all with limit not supported for this driver")
}

func (s *Store) updateQuotaCounters(ctx context.Context, tenantKey string, deltaBytes, deltaObjects int64) error {
	if s.pg != nil {
		_, err := s.pg.NewUpdate((*model.Quota)(nil)).
			Where("tenant_key = ?", tenantKey).
			Set("used_bytes = used_bytes + ?", deltaBytes).
			Set("object_count = object_count + ?", deltaObjects).
			Set("updated_at = NOW()").
			Exec(ctx)
		return err
	}
	return fmt.Errorf("store: update quota not supported for this driver")
}
