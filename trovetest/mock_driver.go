package trovetest

import (
	"context"
	"io"

	"github.com/xraph/trove/driver"
)

// Compile-time interface check.
var _ driver.Driver = (*MockDriver)(nil)

// MockDriver is a configurable mock driver for unit testing.
// Set the function fields to control behavior per method.
type MockDriver struct {
	NameFunc         func() string
	OpenFunc         func(ctx context.Context, dsn string, opts ...driver.Option) error
	CloseFunc        func(ctx context.Context) error
	PingFunc         func(ctx context.Context) error
	PutFunc          func(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error)
	GetFunc          func(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error)
	DeleteFunc       func(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) error
	HeadFunc         func(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error)
	ListFunc         func(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error)
	CopyFunc         func(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error)
	CreateBucketFunc func(ctx context.Context, name string, opts ...driver.BucketOption) error
	DeleteBucketFunc func(ctx context.Context, name string) error
	ListBucketsFunc  func(ctx context.Context) ([]driver.BucketInfo, error)
}

// Name returns "mock" or delegates to NameFunc.
func (m *MockDriver) Name() string {
	if m.NameFunc != nil {
		return m.NameFunc()
	}
	return "mock"
}

// Open delegates to OpenFunc or returns nil.
func (m *MockDriver) Open(ctx context.Context, dsn string, opts ...driver.Option) error {
	if m.OpenFunc != nil {
		return m.OpenFunc(ctx, dsn, opts...)
	}
	return nil
}

// Close delegates to CloseFunc or returns nil.
func (m *MockDriver) Close(ctx context.Context) error {
	if m.CloseFunc != nil {
		return m.CloseFunc(ctx)
	}
	return nil
}

// Ping delegates to PingFunc or returns nil.
func (m *MockDriver) Ping(ctx context.Context) error {
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return nil
}

// Put delegates to PutFunc or returns nil.
func (m *MockDriver) Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error) {
	if m.PutFunc != nil {
		return m.PutFunc(ctx, bucket, key, r, opts...)
	}
	return &driver.ObjectInfo{Key: key}, nil
}

// Get delegates to GetFunc or returns nil.
func (m *MockDriver) Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, bucket, key, opts...)
	}
	return nil, nil
}

// Delete delegates to DeleteFunc or returns nil.
func (m *MockDriver) Delete(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, bucket, key, opts...)
	}
	return nil
}

// Head delegates to HeadFunc or returns nil.
func (m *MockDriver) Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error) {
	if m.HeadFunc != nil {
		return m.HeadFunc(ctx, bucket, key)
	}
	return &driver.ObjectInfo{Key: key}, nil
}

// List delegates to ListFunc or returns an empty iterator.
func (m *MockDriver) List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, bucket, opts...)
	}
	return driver.NewObjectIterator(nil, ""), nil
}

// Copy delegates to CopyFunc or returns nil.
func (m *MockDriver) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error) {
	if m.CopyFunc != nil {
		return m.CopyFunc(ctx, srcBucket, srcKey, dstBucket, dstKey, opts...)
	}
	return &driver.ObjectInfo{Key: dstKey}, nil
}

// CreateBucket delegates to CreateBucketFunc or returns nil.
func (m *MockDriver) CreateBucket(ctx context.Context, name string, opts ...driver.BucketOption) error {
	if m.CreateBucketFunc != nil {
		return m.CreateBucketFunc(ctx, name, opts...)
	}
	return nil
}

// DeleteBucket delegates to DeleteBucketFunc or returns nil.
func (m *MockDriver) DeleteBucket(ctx context.Context, name string) error {
	if m.DeleteBucketFunc != nil {
		return m.DeleteBucketFunc(ctx, name)
	}
	return nil
}

// ListBuckets delegates to ListBucketsFunc or returns empty.
func (m *MockDriver) ListBuckets(ctx context.Context) ([]driver.BucketInfo, error) {
	if m.ListBucketsFunc != nil {
		return m.ListBucketsFunc(ctx)
	}
	return nil, nil
}
