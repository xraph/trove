package contract

import (
	"context"
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove/driver"
)

type bucketInput struct {
	Store string `json:"store"`
	Name  string `json:"name"`
}

type bucketRow struct {
	Name      string  `json:"name"`
	CreatedAt *string `json:"createdAt"`
}

type bucketsListOutput struct {
	Buckets []bucketRow `json:"buckets"`
	// CreatedAtMeaning is "created" where the driver returns a creation
	// time and "modified" where it returns a last-modified time (local,
	// sftp and azure), so the column header can say which.
	CreatedAtMeaning string `json:"createdAtMeaning"`
}

type bucketNameOutput struct {
	Name string `json:"name"`
}

func bucketsListHandler(deps Deps) func(context.Context, storeInput, contract.Principal) (bucketsListOutput, error) {
	return func(ctx context.Context, in storeInput, _ contract.Principal) (bucketsListOutput, error) {
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return bucketsListOutput{}, err
		}
		list, err := st.Trove.ListBuckets(ctx)
		if err != nil {
			return bucketsListOutput{}, deps.mapError("buckets.list", err)
		}
		out := bucketsListOutput{Buckets: make([]bucketRow, 0, len(list)), CreatedAtMeaning: "modified"}
		if createdAtIsCreation(st.Trove.Driver().Name()) {
			out.CreatedAtMeaning = "created"
		}
		for _, b := range list {
			out.Buckets = append(out.Buckets, bucketRow{Name: b.Name, CreatedAt: formatTime(b.CreatedAt)})
		}
		sort.Slice(out.Buckets, func(i, j int) bool { return out.Buckets[i].Name < out.Buckets[j].Name })
		return out, nil
	}
}

func bucketsCreateHandler(deps Deps) func(context.Context, bucketInput, contract.Principal) (bucketNameOutput, error) {
	return func(ctx context.Context, in bucketInput, _ contract.Principal) (bucketNameOutput, error) {
		if err := requireName("name", in.Name); err != nil {
			return bucketNameOutput{}, err
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return bucketNameOutput{}, err
		}
		if err := st.Trove.CreateBucket(ctx, in.Name); err != nil {
			return bucketNameOutput{}, deps.mapError("buckets.create", err)
		}
		return bucketNameOutput{Name: in.Name}, nil
	}
}

// bucketsDeleteHandler refuses a bucket that might still hold anything, on
// every driver. local, mem, sftp and azure would delete it recursively and s3
// and gcs would refuse with an unclassified error, so the check here is what
// makes the behaviour the same everywhere. It asks the default driver
// directly, because that is the driver Trove.DeleteBucket acts on; Trove.List
// would route by bucket and could read a different backend. A listing that
// shows nothing but returns a continuation token counts as non-empty, since
// a driver may return an empty page with more to come. The local driver
// hides its *.meta.json and .trove-tmp-* files from listings, so a bucket
// holding only those looks empty and is deleted. Something written between
// the check and the delete can also be lost on the recursive drivers.
func bucketsDeleteHandler(deps Deps) func(context.Context, bucketInput, contract.Principal) (bucketNameOutput, error) {
	return func(ctx context.Context, in bucketInput, _ contract.Principal) (bucketNameOutput, error) {
		if err := requireName("name", in.Name); err != nil {
			return bucketNameOutput{}, err
		}
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return bucketNameOutput{}, err
		}
		it, err := st.Trove.Driver().List(ctx, in.Name, driver.WithMaxKeys(1), driver.WithDelimiter("/"))
		if err != nil {
			return bucketNameOutput{}, deps.mapError("buckets.delete", err)
		}
		objects, err := it.All(ctx)
		if err != nil {
			return bucketNameOutput{}, deps.mapError("buckets.delete", err)
		}
		if len(objects) > 0 || len(it.CommonPrefixes()) > 0 || it.NextToken() != "" {
			return bucketNameOutput{}, conflict("This bucket still holds objects. Delete them first.")
		}
		if err := st.Trove.DeleteBucket(ctx, in.Name); err != nil {
			return bucketNameOutput{}, deps.mapError("buckets.delete", err)
		}
		return bucketNameOutput{Name: in.Name}, nil
	}
}
