// Package vfs provides a virtual filesystem abstraction over Trove object storage.
// It adapts flat object key spaces into hierarchical directory structures,
// enabling familiar file-system operations (Stat, ReadDir, Walk, Open, Create,
// Remove, Rename) and compatibility with Go's io/fs.FS interface.
package vfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/xraph/trove/driver"
)

// Storage defines the subset of Trove operations needed by VFS.
// This avoids a circular import with the trove package.
type Storage interface {
	Put(ctx context.Context, bucket, key string, r io.Reader, opts ...driver.PutOption) (*driver.ObjectInfo, error)
	Get(ctx context.Context, bucket, key string, opts ...driver.GetOption) (*driver.ObjectReader, error)
	Delete(ctx context.Context, bucket, key string, opts ...driver.DeleteOption) error
	Head(ctx context.Context, bucket, key string) (*driver.ObjectInfo, error)
	List(ctx context.Context, bucket string, opts ...driver.ListOption) (*driver.ObjectIterator, error)
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string, opts ...driver.CopyOption) (*driver.ObjectInfo, error)
}

// FS provides a virtual filesystem over Trove object storage.
// Object keys are treated as file paths with "/" as the directory separator.
type FS struct {
	store  Storage
	bucket string
}

// New creates a new VFS for the given storage and bucket.
func New(store Storage, bucket string) *FS {
	return &FS{store: store, bucket: bucket}
}

// Stat returns file info for the given path.
func (f *FS) Stat(ctx context.Context, name string) (*FileInfo, error) {
	name = cleanPath(name)

	// Try as a file first.
	info, err := f.store.Head(ctx, f.bucket, name)
	if err == nil {
		return &FileInfo{
			name:    path.Base(name),
			size:    info.Size,
			modTime: info.LastModified,
			isDir:   false,
		}, nil
	}

	// Check if it's a directory (has children).
	prefix := name
	if prefix != "" {
		prefix += "/"
	}
	iter, err := f.store.List(ctx, f.bucket, driver.WithPrefix(prefix), driver.WithDelimiter("/"))
	if err != nil {
		return nil, fmt.Errorf("vfs: stat %q: %w", name, err)
	}
	defer iter.Close()

	_, nextErr := iter.Next(ctx)
	if errors.Is(nextErr, io.EOF) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	if nextErr != nil {
		return nil, fmt.Errorf("vfs: stat %q: %w", name, nextErr)
	}

	return &FileInfo{
		name:  path.Base(name),
		isDir: true,
	}, nil
}

// ReadDir lists entries in a directory.
func (f *FS) ReadDir(ctx context.Context, name string) ([]DirEntry, error) {
	name = cleanPath(name)

	prefix := name
	if prefix != "" {
		prefix += "/"
	}

	iter, err := f.store.List(ctx, f.bucket, driver.WithPrefix(prefix), driver.WithDelimiter("/"))
	if err != nil {
		return nil, fmt.Errorf("vfs: readdir %q: %w", name, err)
	}
	defer iter.Close()

	seen := make(map[string]bool)
	var entries []DirEntry

	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("vfs: readdir %q: %w", name, err)
		}

		// Strip the prefix to get the relative name.
		rel := strings.TrimPrefix(obj.Key, prefix)
		if rel == "" {
			continue
		}

		// Check if this is a "directory" (key ending with /).
		if strings.HasSuffix(rel, "/") {
			dirName := strings.TrimSuffix(rel, "/")
			if !seen[dirName] {
				seen[dirName] = true
				entries = append(entries, DirEntry{
					info: FileInfo{name: dirName, isDir: true},
				})
			}
			continue
		}

		// Check if this key has sub-path components (implicit directory).
		if idx := strings.Index(rel, "/"); idx >= 0 {
			dirName := rel[:idx]
			if !seen[dirName] {
				seen[dirName] = true
				entries = append(entries, DirEntry{
					info: FileInfo{name: dirName, isDir: true},
				})
			}
			continue
		}

		// Regular file.
		if !seen[rel] {
			seen[rel] = true
			entries = append(entries, DirEntry{
				info: FileInfo{
					name:    rel,
					size:    obj.Size,
					modTime: obj.LastModified,
					isDir:   false,
				},
			})
		}
	}

	return entries, nil
}

// Walk traverses the file tree rooted at root, calling fn for each file or directory.
func (f *FS) Walk(ctx context.Context, root string, fn WalkFunc) error {
	root = cleanPath(root)

	info, err := f.Stat(ctx, root)
	if err != nil {
		return fn(root, nil, err)
	}

	return f.walk(ctx, root, info, fn)
}

func (f *FS) walk(ctx context.Context, name string, info *FileInfo, fn WalkFunc) error {
	err := fn(name, info, nil)
	if err != nil {
		if info.IsDir() && errors.Is(err, fs.SkipDir) {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	entries, err := f.ReadDir(ctx, name)
	if err != nil {
		return fn(name, info, err)
	}

	for _, entry := range entries {
		childPath := path.Join(name, entry.Name())
		childInfo := entry.info
		if err := f.walk(ctx, childPath, &childInfo, fn); err != nil {
			if errors.Is(err, fs.SkipAll) {
				return nil
			}
			return err
		}
	}

	return nil
}

// Open opens a file for reading.
func (f *FS) Open(ctx context.Context, name string) (*File, error) {
	name = cleanPath(name)

	obj, err := f.store.Get(ctx, f.bucket, name)
	if err != nil {
		return nil, fmt.Errorf("vfs: open %q: %w", name, err)
	}

	return &File{
		name:   name,
		reader: obj.ReadCloser,
		info: FileInfo{
			name:    path.Base(name),
			size:    obj.Info.Size,
			modTime: obj.Info.LastModified,
		},
	}, nil
}

// Create creates or overwrites a file. The caller must Close the returned File
// to flush the data to storage.
func (f *FS) Create(ctx context.Context, name string) (*File, error) {
	name = cleanPath(name)

	pr, pw := io.Pipe()

	file := &File{
		name:   name,
		writer: pw,
		done:   make(chan error, 1),
	}

	go func() {
		_, err := f.store.Put(ctx, f.bucket, name, pr)
		if err != nil {
			_ = pr.CloseWithError(err)
		}
		file.done <- err
	}()

	return file, nil
}

// Mkdir creates a directory marker object. This is a zero-length object with
// a trailing "/" in the key.
func (f *FS) Mkdir(ctx context.Context, name string) error {
	name = cleanPath(name)
	if name == "" {
		return nil // root always exists
	}

	key := name + "/"
	_, err := f.store.Put(ctx, f.bucket, key, strings.NewReader(""))
	if err != nil {
		return fmt.Errorf("vfs: mkdir %q: %w", name, err)
	}
	return nil
}

// Remove removes a file.
func (f *FS) Remove(ctx context.Context, name string) error {
	name = cleanPath(name)
	if err := f.store.Delete(ctx, f.bucket, name); err != nil {
		return fmt.Errorf("vfs: remove %q: %w", name, err)
	}
	return nil
}

// RemoveAll removes a path and all children.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	name = cleanPath(name)

	prefix := name
	if prefix != "" {
		prefix += "/"
	}

	// Delete the key itself.
	f.store.Delete(ctx, f.bucket, name) //nolint:errcheck // best effort

	// Delete all children.
	iter, err := f.store.List(ctx, f.bucket, driver.WithPrefix(prefix))
	if err != nil {
		return fmt.Errorf("vfs: removeall %q: %w", name, err)
	}
	defer iter.Close()

	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("vfs: removeall %q: %w", name, err)
		}
		if delErr := f.store.Delete(ctx, f.bucket, obj.Key); delErr != nil {
			return fmt.Errorf("vfs: removeall %q: %w", obj.Key, delErr)
		}
	}

	return nil
}

// Rename moves a file from oldName to newName.
func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	oldName = cleanPath(oldName)
	newName = cleanPath(newName)

	if _, err := f.store.Copy(ctx, f.bucket, oldName, f.bucket, newName); err != nil {
		return fmt.Errorf("vfs: rename %q -> %q: %w", oldName, newName, err)
	}
	if err := f.store.Delete(ctx, f.bucket, oldName); err != nil {
		return fmt.Errorf("vfs: rename delete old %q: %w", oldName, err)
	}
	return nil
}

// SetMetadata sets metadata on a file by copying it with updated metadata.
func (f *FS) SetMetadata(ctx context.Context, name string, meta map[string]string) error {
	name = cleanPath(name)

	_, err := f.store.Copy(ctx, f.bucket, name, f.bucket, name,
		driver.WithCopyMetadata(meta),
	)
	if err != nil {
		return fmt.Errorf("vfs: set metadata %q: %w", name, err)
	}
	return nil
}

// GetMetadata returns metadata for a file.
func (f *FS) GetMetadata(ctx context.Context, name string) (map[string]string, error) {
	name = cleanPath(name)

	info, err := f.store.Head(ctx, f.bucket, name)
	if err != nil {
		return nil, fmt.Errorf("vfs: get metadata %q: %w", name, err)
	}
	return info.Metadata, nil
}

// WalkFunc is the callback for Walk.
type WalkFunc func(path string, info *FileInfo, err error) error

// cleanPath normalizes a path by removing leading/trailing slashes and cleaning.
func cleanPath(name string) string {
	name = path.Clean(name)
	name = strings.TrimPrefix(name, "/")
	if name == "." {
		name = ""
	}
	return name
}

// FileInfo implements os.FileInfo for VFS entries.
type FileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (fi *FileInfo) Name() string { return fi.name }
func (fi *FileInfo) Size() int64  { return fi.size }
func (fi *FileInfo) Mode() fs.FileMode {
	if fi.isDir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (fi *FileInfo) ModTime() time.Time { return fi.modTime }
func (fi *FileInfo) IsDir() bool        { return fi.isDir }
func (fi *FileInfo) Sys() any           { return nil }

// DirEntry is a directory entry returned by ReadDir.
type DirEntry struct {
	info FileInfo
}

func (de DirEntry) Name() string               { return de.info.Name() }
func (de DirEntry) IsDir() bool                { return de.info.IsDir() }
func (de DirEntry) Type() fs.FileMode          { return de.info.Mode().Type() }
func (de DirEntry) Info() (fs.FileInfo, error) { return &de.info, nil }
