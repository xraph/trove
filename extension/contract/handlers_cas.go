package contract

import (
	"context"
	"errors"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
)

type casStatusOutput struct {
	Enabled   bool    `json:"enabled"`
	Algorithm *string `json:"algorithm"`
	Bucket    *string `json:"bucket"`
	// Index is "memory": the extension never gives CAS a persistent index.
	Index *string `json:"index"`
	// ResetsOnRestart is true for the memory index: every refcount and pin
	// is lost, and existing blobs stop being found.
	ResetsOnRestart bool `json:"resetsOnRestart"`
	// ReleaseSupported is false: nothing in CAS lowers a refcount, so GC
	// never finds anything to collect.
	ReleaseSupported bool `json:"releaseSupported"`
}

func casStatusHandler(deps Deps) func(context.Context, storeInput, contract.Principal) (casStatusOutput, error) {
	return func(_ context.Context, in storeInput, _ contract.Principal) (casStatusOutput, error) {
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return casStatusOutput{}, err
		}
		c := st.Trove.CAS()
		if c == nil {
			return casStatusOutput{}, nil
		}
		return casStatusOutput{
			Enabled:          true,
			Algorithm:        optString(c.Algorithm().String()),
			Bucket:           optString(c.Bucket()),
			Index:            optString("memory"),
			ResetsOnRestart:  true,
			ReleaseSupported: false,
		}, nil
	}
}

func requireCAS(deps Deps, store string) (*cas.CAS, *Store, error) {
	st, err := deps.Stores.Resolve(store)
	if err != nil {
		return nil, nil, err
	}
	c := st.Trove.CAS()
	if c == nil {
		return nil, nil, unavailable("CAS is not enabled on this store.")
	}
	return c, st, nil
}

type casListInput struct {
	Store  string `json:"store"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type casEntryRow struct {
	Hash         string  `json:"hash"`
	StoredSize   int64   `json:"storedSize"`
	LastModified *string `json:"lastModified"`
	Indexed      bool    `json:"indexed"`
	RefCount     *int    `json:"refCount"`
	Pinned       *bool   `json:"pinned"`
}

type casListOutput struct {
	Entries    []casEntryRow `json:"entries"`
	NextCursor *string       `json:"nextCursor"`
}

// casListHandler lists the CAS bucket on the default driver and joins each
// blob to the index. CAS writes through the default driver whatever the
// routes say, so Trove.List, which routes by bucket, could read a backend
// that holds none of its blobs. A blob the index does not know is
// `indexed: false`, which is how a restart shows up.
func casListHandler(deps Deps) func(context.Context, casListInput, contract.Principal) (casListOutput, error) {
	return func(ctx context.Context, in casListInput, _ contract.Principal) (casListOutput, error) {
		c, st, err := requireCAS(deps, in.Store)
		if err != nil {
			return casListOutput{}, err
		}
		token, err := decodeCursor(in.Cursor)
		if err != nil {
			return casListOutput{}, err
		}
		limit := in.Limit
		if limit <= 0 {
			limit = defaultListLimit
		}
		if limit > maxListLimit {
			limit = maxListLimit
		}
		opts := []driver.ListOption{driver.WithMaxKeys(limit)}
		if token != "" {
			opts = append(opts, driver.WithCursor(token))
		}
		out := casListOutput{Entries: []casEntryRow{}}
		it, err := st.Trove.Driver().List(ctx, c.Bucket(), opts...)
		if errors.Is(err, driver.ErrBucketNotFound) {
			// The extension never creates the CAS bucket; until something
			// is stored there is nothing to list.
			return out, nil
		}
		if err != nil {
			return casListOutput{}, deps.mapError("cas.list", err)
		}
		objects, err := it.All(ctx)
		if err != nil {
			return casListOutput{}, deps.mapError("cas.list", err)
		}
		for _, o := range objects {
			row := casEntryRow{Hash: o.Key, StoredSize: o.Size, LastModified: formatTime(o.LastModified)}
			entry, statErr := c.Stat(ctx, o.Key)
			switch {
			case statErr == nil:
				refs, pinned := entry.RefCount, entry.Pinned
				row.Indexed, row.RefCount, row.Pinned = true, &refs, &pinned
			case errors.Is(statErr, cas.ErrNotFound):
			default:
				return casListOutput{}, deps.mapError("cas.list", statErr)
			}
			out.Entries = append(out.Entries, row)
		}
		out.NextCursor = encodeCursor(it.NextToken())
		return out, nil
	}
}

type casHashInput struct {
	Store string `json:"store"`
	Hash  string `json:"hash"`
}

func casPinHandlerFor(deps Deps, intent string, pin bool) func(context.Context, casHashInput, contract.Principal) (casEntryRow, error) {
	return func(ctx context.Context, in casHashInput, _ contract.Principal) (casEntryRow, error) {
		if in.Hash == "" {
			return casEntryRow{}, badRequest("hash is required")
		}
		c, _, err := requireCAS(deps, in.Store)
		if err != nil {
			return casEntryRow{}, err
		}
		if pin {
			err = c.Pin(ctx, in.Hash)
		} else {
			err = c.Unpin(ctx, in.Hash)
		}
		if err != nil {
			return casEntryRow{}, deps.mapError(intent, err)
		}
		entry, err := c.Stat(ctx, in.Hash)
		if err != nil {
			return casEntryRow{}, deps.mapError(intent, err)
		}
		refs, pinned := entry.RefCount, entry.Pinned
		return casEntryRow{Hash: entry.Hash, StoredSize: entry.Size, Indexed: true, RefCount: &refs, Pinned: &pinned}, nil
	}
}

func casPinHandler(deps Deps) func(context.Context, casHashInput, contract.Principal) (casEntryRow, error) {
	return casPinHandlerFor(deps, "cas.pin", true)
}

func casUnpinHandler(deps Deps) func(context.Context, casHashInput, contract.Principal) (casEntryRow, error) {
	return casPinHandlerFor(deps, "cas.unpin", false)
}

type casGCOutput struct {
	Scanned    int   `json:"scanned"`
	Deleted    int   `json:"deleted"`
	FreedBytes int64 `json:"freedBytes"`
	Errors     int   `json:"errors"`
}

func casGCHandler(deps Deps) func(context.Context, storeInput, contract.Principal) (casGCOutput, error) {
	return func(ctx context.Context, in storeInput, _ contract.Principal) (casGCOutput, error) {
		c, _, err := requireCAS(deps, in.Store)
		if err != nil {
			return casGCOutput{}, err
		}
		res, err := c.GC(ctx)
		if err != nil {
			return casGCOutput{}, deps.mapError("cas.gc", err)
		}
		return casGCOutput{Scanned: res.Scanned, Deleted: res.Deleted, FreedBytes: res.FreedBytes, Errors: res.Errors}, nil
	}
}
