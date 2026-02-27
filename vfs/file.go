package vfs

import (
	"io"
	"io/fs"
)

// File represents an open VFS file backed by object storage.
type File struct {
	name   string
	reader io.ReadCloser
	writer *io.PipeWriter
	info   FileInfo
	done   chan error // signals completion for Create() writes
}

// Read reads from the file (only valid for files opened with Open).
func (f *File) Read(p []byte) (int, error) {
	if f.reader == nil {
		return 0, io.ErrClosedPipe
	}
	return f.reader.Read(p)
}

// Write writes to the file (only valid for files created with Create).
func (f *File) Write(p []byte) (int, error) {
	if f.writer == nil {
		return 0, io.ErrClosedPipe
	}
	return f.writer.Write(p)
}

// Close closes the file, flushing any pending writes.
func (f *File) Close() error {
	if f.reader != nil {
		return f.reader.Close()
	}
	if f.writer != nil {
		if err := f.writer.Close(); err != nil {
			return err
		}
		// Wait for the upload goroutine to finish.
		return <-f.done
	}
	return nil
}

// Name returns the file name.
func (f *File) Name() string { return f.name }

// Stat returns the FileInfo for this file.
func (f *File) Stat() (*FileInfo, error) { return &f.info, nil }

// Compile-time checks.
var (
	_ io.Reader = (*File)(nil)
	_ io.Writer = (*File)(nil)
	_ io.Closer = (*File)(nil)
)

// fsFile adapts a VFS File for io/fs.FS compatibility.
type fsFile struct {
	file *File
	info *FileInfo
}

func (f *fsFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *fsFile) Read(p []byte) (int, error) { return f.file.Read(p) }
func (f *fsFile) Close() error               { return f.file.Close() }
