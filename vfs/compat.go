package vfs

import (
	"context"
	"io/fs"
)

// IOFS adapts a Trove VFS to Go's io/fs.FS interface, enabling use with
// http.FileServer(http.FS(vfs)), template.ParseFS, and other stdlib consumers.
type IOFS struct {
	fs  *FS
	ctx context.Context
}

// NewIOFS creates an io/fs.FS adapter for the VFS.
// The provided context is used for all operations.
func NewIOFS(ctx context.Context, vfs *FS) *IOFS {
	return &IOFS{fs: vfs, ctx: ctx}
}

// Open implements fs.FS.
func (f *IOFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	// Check if it's a directory.
	info, err := f.fs.Stat(f.ctx, name)
	if err != nil {
		return nil, err
	}

	if info.IsDir() {
		return &fsDir{
			fs:   f.fs,
			ctx:  f.ctx,
			name: name,
			info: info,
		}, nil
	}

	file, err := f.fs.Open(f.ctx, name)
	if err != nil {
		return nil, err
	}

	return &fsFile{file: file, info: info}, nil
}

// Compile-time check.
var _ fs.FS = (*IOFS)(nil)

// fsDir implements fs.ReadDirFile for directory listings.
type fsDir struct {
	fs      *FS
	ctx     context.Context
	name    string
	info    *FileInfo
	entries []DirEntry
	offset  int
}

func (d *fsDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *fsDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}
func (d *fsDir) Close() error { return nil }

// ReadDir implements fs.ReadDirFile.
func (d *fsDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.entries == nil {
		entries, err := d.fs.ReadDir(d.ctx, d.name)
		if err != nil {
			return nil, err
		}
		d.entries = entries
	}

	if n <= 0 {
		// Return all remaining entries.
		remaining := d.entries[d.offset:]
		d.offset = len(d.entries)
		result := make([]fs.DirEntry, len(remaining))
		for i := range remaining {
			e := remaining[i]
			result[i] = e
		}
		return result, nil
	}

	if d.offset >= len(d.entries) {
		return nil, fs.ErrInvalid
	}

	end := d.offset + n
	if end > len(d.entries) {
		end = len(d.entries)
	}

	slice := d.entries[d.offset:end]
	d.offset = end

	result := make([]fs.DirEntry, len(slice))
	for i := range slice {
		e := slice[i]
		result[i] = e
	}
	return result, nil
}

// Compile-time checks.
var (
	_ fs.File        = (*fsDir)(nil)
	_ fs.ReadDirFile = (*fsDir)(nil)
)
