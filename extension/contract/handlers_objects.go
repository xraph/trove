package contract

import (
	"context"
	"strings"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
)

const (
	defaultListLimit = 100
	maxListLimit     = 1000
)

type objectKeyInput struct {
	Store  string `json:"store"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

type objectsListInput struct {
	Store  string `json:"store"`
	Bucket string `json:"bucket"`
	Prefix string `json:"prefix"`
	// Delimiter defaults to "/" when absent. An empty string asks for a
	// flat listing.
	Delimiter *string `json:"delimiter"`
	Cursor    string  `json:"cursor"`
	Limit     int     `json:"limit"`
}

type objectsListOutput struct {
	Objects []objectRow `json:"objects"`
	// Prefixes is null on a flat listing and a list otherwise. Objects and
	// prefixes are each sorted; the page merges them.
	Prefixes   []string `json:"prefixes"`
	NextCursor *string  `json:"nextCursor"`
	// FoldersSupported is true when this listing set a delimiter and the
	// driver folded keys into prefixes. It is false for a flat listing,
	// and false if a driver returned keys it should have folded.
	FoldersSupported bool `json:"foldersSupported"`
}

func objectsListHandler(deps Deps) func(context.Context, objectsListInput, contract.Principal) (objectsListOutput, error) {
	return func(ctx context.Context, in objectsListInput, _ contract.Principal) (objectsListOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return objectsListOutput{}, err
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return objectsListOutput{}, err
		}
		token, err := decodeCursor(in.Cursor)
		if err != nil {
			return objectsListOutput{}, err
		}
		delim := "/"
		if in.Delimiter != nil {
			delim = *in.Delimiter
		}
		limit := in.Limit
		if limit <= 0 {
			limit = defaultListLimit
		}
		if limit > maxListLimit {
			limit = maxListLimit
		}

		opts := []driver.ListOption{driver.WithMaxKeys(limit)}
		if in.Prefix != "" {
			opts = append(opts, driver.WithPrefix(in.Prefix))
		}
		if delim != "" {
			opts = append(opts, driver.WithDelimiter(delim))
		}
		if token != "" {
			opts = append(opts, driver.WithCursor(token))
		}

		it, err := st.Trove.List(ctx, in.Bucket, opts...)
		if err != nil {
			return objectsListOutput{}, deps.mapError("objects.list", err)
		}
		objects, err := it.All(ctx)
		if err != nil {
			return objectsListOutput{}, deps.mapError("objects.list", err)
		}

		out := objectsListOutput{
			Objects:    make([]objectRow, 0, len(objects)),
			NextCursor: encodeCursor(it.NextToken()),
		}
		folded := delim != ""
		for _, o := range objects {
			out.Objects = append(out.Objects, projectObjectRow(o))
			if delim != "" && strings.Contains(strings.TrimPrefix(o.Key, in.Prefix), delim) {
				folded = false
			}
		}
		if delim != "" {
			out.Prefixes = append([]string{}, it.CommonPrefixes()...)
		}
		out.FoldersSupported = folded
		return out, nil
	}
}

// presignStatus says whether a share link can be offered for a key, and why
// not when it cannot.
type presignStatus struct {
	Available bool    `json:"available"`
	Reason    *string `json:"reason"`
}

// presignAvailability offers a presigned link only when the driver can sign
// one and no middleware applies to the key. A presigned link talks to the
// backend directly, so it would skip encrypt, compress and scan on the way
// in and return stored bytes on the way out.
func presignAvailability(t *trove.Trove, rows []middlewareRow) presignStatus {
	if _, ok := t.Driver().(driver.PresignDriver); !ok {
		return presignStatus{Reason: optString("This driver cannot create presigned links.")}
	}
	if len(rows) > 0 {
		names := make([]string, 0, len(rows))
		for _, r := range rows {
			names = append(names, r.Name)
		}
		return presignStatus{Reason: optString("Middleware applies to this key (" + strings.Join(names, ", ") +
			"). A presigned link would skip it and return the stored bytes.")}
	}
	return presignStatus{Available: true}
}

// casBucket returns the CAS bucket when this Trove has CAS enabled.
func casBucket(t *trove.Trove) (string, bool) {
	c := t.CAS()
	if c == nil {
		return "", false
	}
	return c.Bucket(), true
}

type objectsHeadOutput struct {
	Object     objectDetail    `json:"object"`
	Middleware []middlewareRow `json:"middleware"`
	Presign    presignStatus   `json:"presign"`
}

func objectsHeadHandler(deps Deps) func(context.Context, objectKeyInput, contract.Principal) (objectsHeadOutput, error) {
	return func(ctx context.Context, in objectKeyInput, _ contract.Principal) (objectsHeadOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return objectsHeadOutput{}, err
		}
		if in.Key == "" {
			return objectsHeadOutput{}, badRequest("key is required")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return objectsHeadOutput{}, err
		}
		info, err := st.Trove.Head(ctx, in.Bucket, in.Key)
		if err != nil {
			return objectsHeadOutput{}, deps.mapError("objects.head", err)
		}
		rows := matchingAny(ctx, st.Trove, in.Bucket, in.Key)
		return objectsHeadOutput{
			Object:     projectObjectDetail(info),
			Middleware: rows,
			Presign:    presignAvailability(st.Trove, rows),
		}, nil
	}
}
