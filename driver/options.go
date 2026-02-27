package driver

// Option configures a driver during Open.
type Option func(*Config)

// Config holds driver-level configuration applied during Open.
type Config struct {
	// Region is the cloud region for the storage backend.
	Region string

	// Endpoint overrides the default service endpoint.
	Endpoint string

	// ForcePathStyle forces path-style addressing for S3-compatible backends.
	ForcePathStyle bool
}

// ApplyOptions applies a set of options to a Config.
func ApplyOptions(opts ...Option) Config {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithRegion sets the cloud region.
func WithRegion(region string) Option {
	return func(c *Config) {
		c.Region = region
	}
}

// WithEndpoint overrides the default service endpoint.
func WithEndpoint(endpoint string) Option {
	return func(c *Config) {
		c.Endpoint = endpoint
	}
}

// WithForcePathStyle forces path-style addressing.
func WithForcePathStyle(force bool) Option {
	return func(c *Config) {
		c.ForcePathStyle = force
	}
}

// --- Put Options ---

// PutOption configures a Put operation.
type PutOption func(*PutConfig)

// PutConfig holds options for a Put operation.
type PutConfig struct {
	ContentType  string
	Metadata     map[string]string
	Tags         map[string]string
	StorageClass string
}

// ApplyPutOptions applies a set of PutOptions to a PutConfig.
func ApplyPutOptions(opts ...PutOption) PutConfig {
	cfg := PutConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithContentType sets the object's content type.
func WithContentType(ct string) PutOption {
	return func(c *PutConfig) {
		c.ContentType = ct
	}
}

// WithMetadata sets custom metadata on the object.
func WithMetadata(meta map[string]string) PutOption {
	return func(c *PutConfig) {
		c.Metadata = meta
	}
}

// WithTags sets custom tags on the object.
func WithTags(tags map[string]string) PutOption {
	return func(c *PutConfig) {
		c.Tags = tags
	}
}

// WithStorageClass sets the storage class for the object.
func WithStorageClass(class string) PutOption {
	return func(c *PutConfig) {
		c.StorageClass = class
	}
}

// --- Get Options ---

// GetOption configures a Get operation.
type GetOption func(*GetConfig)

// GetConfig holds options for a Get operation.
type GetConfig struct {
	// Range specifies a byte range to retrieve (e.g., "bytes=0-1023").
	Range string

	// VersionID retrieves a specific version of the object.
	VersionID string
}

// ApplyGetOptions applies a set of GetOptions to a GetConfig.
func ApplyGetOptions(opts ...GetOption) GetConfig {
	cfg := GetConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithRange sets the byte range for a Get operation.
func WithRange(r string) GetOption {
	return func(c *GetConfig) {
		c.Range = r
	}
}

// WithVersionID retrieves a specific version.
func WithVersionID(vid string) GetOption {
	return func(c *GetConfig) {
		c.VersionID = vid
	}
}

// --- Delete Options ---

// DeleteOption configures a Delete operation.
type DeleteOption func(*DeleteConfig)

// DeleteConfig holds options for a Delete operation.
type DeleteConfig struct {
	// VersionID deletes a specific version.
	VersionID string
}

// ApplyDeleteOptions applies a set of DeleteOptions to a DeleteConfig.
func ApplyDeleteOptions(opts ...DeleteOption) DeleteConfig {
	cfg := DeleteConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// --- List Options ---

// ListOption configures a List operation.
type ListOption func(*ListConfig)

// ListConfig holds options for a List operation.
type ListConfig struct {
	// Prefix filters objects by key prefix.
	Prefix string

	// Delimiter groups keys by a delimiter character (e.g., "/").
	Delimiter string

	// MaxKeys is the maximum number of objects to return.
	MaxKeys int

	// Cursor is the pagination cursor from a previous List call.
	Cursor string
}

// ApplyListOptions applies a set of ListOptions to a ListConfig.
func ApplyListOptions(opts ...ListOption) ListConfig {
	cfg := ListConfig{
		MaxKeys: 1000, // sensible default
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithPrefix filters objects by key prefix.
func WithPrefix(prefix string) ListOption {
	return func(c *ListConfig) {
		c.Prefix = prefix
	}
}

// WithDelimiter groups keys by a delimiter character.
func WithDelimiter(delimiter string) ListOption {
	return func(c *ListConfig) {
		c.Delimiter = delimiter
	}
}

// WithMaxKeys sets the maximum number of objects to return.
func WithMaxKeys(n int) ListOption {
	return func(c *ListConfig) {
		c.MaxKeys = n
	}
}

// WithCursor sets the pagination cursor.
func WithCursor(cursor string) ListOption {
	return func(c *ListConfig) {
		c.Cursor = cursor
	}
}

// --- Copy Options ---

// CopyOption configures a Copy operation.
type CopyOption func(*CopyConfig)

// CopyConfig holds options for a Copy operation.
type CopyConfig struct {
	// Metadata overrides the source object's metadata on the copy.
	Metadata map[string]string
}

// WithCopyMetadata overrides the source object's metadata on the copy.
func WithCopyMetadata(meta map[string]string) CopyOption {
	return func(c *CopyConfig) { c.Metadata = meta }
}

// ApplyCopyOptions applies a set of CopyOptions to a CopyConfig.
func ApplyCopyOptions(opts ...CopyOption) CopyConfig {
	cfg := CopyConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// --- Bucket Options ---

// BucketOption configures a CreateBucket operation.
type BucketOption func(*BucketConfig)

// BucketConfig holds options for a CreateBucket operation.
type BucketConfig struct {
	// Region overrides the default region for the bucket.
	Region string
}

// ApplyBucketOptions applies a set of BucketOptions to a BucketConfig.
func ApplyBucketOptions(opts ...BucketOption) BucketConfig {
	cfg := BucketConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// WithBucketRegion sets the region for a new bucket.
func WithBucketRegion(region string) BucketOption {
	return func(c *BucketConfig) {
		c.Region = region
	}
}
