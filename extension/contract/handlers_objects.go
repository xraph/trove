package contract

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
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

// presignAvailability offers a presigned link only when the driver that
// serves the key can sign one and no middleware applies to it. The driver is
// the routed one, not the default, because a route can send this key to a
// backend with different abilities. A presigned link talks to the
// backend directly, so it would skip encrypt, compress and scan on the way
// in and return stored bytes on the way out.
func presignAvailability(t *trove.Trove, bucket, key string, rows []middlewareRow) presignStatus {
	if _, ok := t.DriverFor(bucket, key).(driver.PresignDriver); !ok {
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
			Presign:    presignAvailability(st.Trove, in.Bucket, in.Key, rows),
		}, nil
	}
}

// refuseCASBucket refuses a write or delete in the CAS bucket. CAS owns
// that bucket's keys and its index points at them: deleting one leaves the
// index pointing at nothing, and writing one plants content the index
// never heard of.
func refuseCASBucket(t *trove.Trove, bucket string) error {
	if b, ok := casBucket(t); ok && b == bucket {
		return conflict("CAS manages the " + b + " bucket. Changing its objects here would leave the CAS index pointing at the wrong content.")
	}
	return nil
}

type keyOutput struct {
	Key string `json:"key"`
}

func objectsDeleteHandler(deps Deps) func(context.Context, objectKeyInput, contract.Principal) (keyOutput, error) {
	return func(ctx context.Context, in objectKeyInput, _ contract.Principal) (keyOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return keyOutput{}, err
		}
		if in.Key == "" {
			return keyOutput{}, badRequest("key is required")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return keyOutput{}, err
		}
		if err := refuseCASBucket(st.Trove, in.Bucket); err != nil {
			return keyOutput{}, err
		}
		if err := st.Trove.Delete(ctx, in.Bucket, in.Key); err != nil {
			return keyOutput{}, deps.mapError("objects.delete", err)
		}
		return keyOutput{Key: in.Key}, nil
	}
}

type objectsCopyInput struct {
	Store     string `json:"store"`
	SrcBucket string `json:"srcBucket"`
	SrcKey    string `json:"srcKey"`
	DstBucket string `json:"dstBucket"`
	DstKey    string `json:"dstKey"`
	Overwrite bool   `json:"overwrite"`
}

// pipelineSignature describes the middleware that runs for a key in both
// directions, in order, so two keys can be compared.
func pipelineSignature(ctx context.Context, t *trove.Trove, bucket, key string) string {
	var b strings.Builder
	for _, dir := range []struct {
		label string
		rows  []middlewareRow
	}{
		{"write:", matching(ctx, t, bucket, key, middleware.DirectionWrite)},
		{"read:", matching(ctx, t, bucket, key, middleware.DirectionRead)},
	} {
		b.WriteString(dir.label)
		for _, r := range dir.rows {
			b.WriteString(r.Name)
			b.WriteByte(',')
		}
		b.WriteByte(';')
	}
	return b.String()
}

// objectsCopyHandler copies stored bytes. Copy runs no middleware, so it
// refuses when the destination would run different middleware from the
// source: compressed or encrypted bytes would land where nothing decodes
// them.
func objectsCopyHandler(deps Deps) func(context.Context, objectsCopyInput, contract.Principal) (objectRow, error) {
	return func(ctx context.Context, in objectsCopyInput, _ contract.Principal) (objectRow, error) {
		for field, v := range map[string]string{"srcBucket": in.SrcBucket, "dstBucket": in.DstBucket} {
			if err := requireName(field, v); err != nil {
				return objectRow{}, err
			}
		}
		if in.SrcKey == "" || in.DstKey == "" {
			return objectRow{}, badRequest("srcKey and dstKey are required")
		}
		if in.SrcBucket == in.DstBucket && in.SrcKey == in.DstKey {
			return objectRow{}, badRequest("the source and the destination are the same object")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return objectRow{}, err
		}
		t := st.Trove
		err = refuseCASBucket(t, in.DstBucket)
		if err != nil {
			return objectRow{}, err
		}
		if _, err = t.Head(ctx, in.SrcBucket, in.SrcKey); err != nil {
			return objectRow{}, deps.mapError("objects.copy", err)
		}
		if !in.Overwrite {
			_, err = t.Head(ctx, in.DstBucket, in.DstKey)
			if err == nil {
				return objectRow{}, &contract.Error{
					Code: contract.CodeConflict, Message: "an object with this key already exists",
					Details: map[string]any{"exists": true},
				}
			}
			if !errors.Is(err, driver.ErrObjectNotFound) {
				return objectRow{}, deps.mapError("objects.copy", err)
			}
		}
		if pipelineSignature(ctx, t, in.SrcBucket, in.SrcKey) != pipelineSignature(ctx, t, in.DstBucket, in.DstKey) {
			return objectRow{}, conflict("Different middleware applies to the destination. Copy moves stored bytes without running middleware, so the copy would not read back correctly.")
		}
		info, err := t.Copy(ctx, in.SrcBucket, in.SrcKey, in.DstBucket, in.DstKey)
		if err != nil {
			return objectRow{}, deps.mapError("objects.copy", err)
		}
		return projectObjectRow(*info), nil
	}
}

const (
	defaultPresignSeconds = 3600
	minPresignSeconds     = 60
	maxPresignSeconds     = 7 * 24 * 3600
)

type objectsPresignInput struct {
	Store          string `json:"store"`
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	ExpiresSeconds int    `json:"expiresSeconds"`
}

type linkOutput struct {
	URL       string `json:"url"`
	ExpiresAt string `json:"expiresAt"`
}

func objectsPresignHandler(deps Deps) func(context.Context, objectsPresignInput, contract.Principal) (linkOutput, error) {
	return func(ctx context.Context, in objectsPresignInput, _ contract.Principal) (linkOutput, error) {
		if err := requireName("bucket", in.Bucket); err != nil {
			return linkOutput{}, err
		}
		if in.Key == "" {
			return linkOutput{}, badRequest("key is required")
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return linkOutput{}, err
		}
		if _, err = st.Trove.Head(ctx, in.Bucket, in.Key); err != nil {
			return linkOutput{}, deps.mapError("objects.presign", err)
		}
		status := presignAvailability(st.Trove, in.Bucket, in.Key, matchingAny(ctx, st.Trove, in.Bucket, in.Key))
		if !status.Available {
			return linkOutput{}, unavailable(*status.Reason)
		}
		secs := in.ExpiresSeconds
		if secs == 0 {
			secs = defaultPresignSeconds
		}
		if secs < minPresignSeconds {
			secs = minPresignSeconds
		}
		if secs > maxPresignSeconds {
			secs = maxPresignSeconds
		}
		ttl := time.Duration(secs) * time.Second
		expires := time.Now().Add(ttl)
		// presignAvailability has just confirmed this driver can sign.
		signer, ok := st.Trove.DriverFor(in.Bucket, in.Key).(driver.PresignDriver)
		if !ok {
			return linkOutput{}, unavailable("This driver cannot create presigned links.")
		}
		link, err := signer.PresignGet(ctx, in.Bucket, in.Key, ttl)
		if err != nil {
			if deps.Logger != nil {
				deps.Logger.Error("trove/contract: presign failed", forge.F("store", st.Name), forge.F("error", err))
			}
			return linkOutput{}, unavailable("The driver could not sign a link. GCS needs a service account key and Azure a shared key.")
		}
		return linkOutput{URL: link, ExpiresAt: expires.UTC().Format(time.RFC3339)}, nil
	}
}
