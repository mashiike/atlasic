package awsadp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/mashiike/atlasic"
)

// s3File implements fs.File for S3 objects
type s3File struct {
	client *s3.Client
	bucket string
	key    string
	flag   int
	perm   os.FileMode

	mu     sync.RWMutex
	buffer *bytes.Buffer
	closed bool
	dirty  bool // true if buffer has been modified
}

// newS3File creates a new S3 file instance
func newS3File(client *s3.Client, bucket, key string, flag int, perm os.FileMode) *s3File {
	return &s3File{
		client: client,
		bucket: bucket,
		key:    key,
		flag:   flag,
		perm:   perm,
		buffer: &bytes.Buffer{},
	}
}

// Read implements io.Reader
func (f *s3File) Read(p []byte) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return 0, fs.ErrClosed
	}

	if f.flag&os.O_WRONLY != 0 {
		return 0, fmt.Errorf("file opened for write-only")
	}

	// Load file content if buffer is empty
	if f.buffer.Len() == 0 && !f.dirty {
		if err := f.loadContent(); err != nil {
			return 0, err
		}
	}

	return f.buffer.Read(p)
}

// Write implements io.Writer
func (f *s3File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, fs.ErrClosed
	}

	if f.flag&os.O_RDONLY != 0 {
		return 0, fmt.Errorf("file opened for read-only")
	}

	// For append mode, load existing content first
	if f.flag&os.O_APPEND != 0 && f.buffer.Len() == 0 && !f.dirty {
		if err := f.loadContent(); err != nil && err != atlasic.ErrFileNotFound {
			return 0, err
		}
	}

	// For truncate mode, clear buffer
	if f.flag&os.O_TRUNC != 0 && !f.dirty {
		f.buffer.Reset()
	}

	// Write to buffer
	n, err := f.buffer.Write(p)
	f.dirty = true
	return n, err
}

// Close implements io.Closer
func (f *s3File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return fs.ErrClosed
	}

	f.closed = true

	// Flush buffer to S3 if there's data to write
	if f.dirty && (f.flag&os.O_WRONLY != 0 || f.flag&os.O_RDWR != 0) {
		return f.flush()
	}

	return nil
}

// Stat implements fs.File
func (f *s3File) Stat() (fs.FileInfo, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if f.closed {
		return nil, fs.ErrClosed
	}

	return &s3FileInfo{
		name: filepath.Base(f.key),
		size: int64(f.buffer.Len()),
		mode: f.perm,
	}, nil
}

// loadContent loads file content from S3 into buffer
func (f *s3File) loadContent() error {
	ctx := context.Background()

	result, err := f.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(f.key),
	})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchKey") {
			return atlasic.ErrFileNotFound
		}
		return fmt.Errorf("failed to get S3 object: %w", err)
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return fmt.Errorf("failed to read S3 object body: %w", err)
	}

	f.buffer.Reset()
	f.buffer.Write(data)
	return nil
}

// flush writes buffer content to S3
func (f *s3File) flush() error {
	ctx := context.Background()

	_, err := f.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(f.bucket),
		Key:    aws.String(f.key),
		Body:   bytes.NewReader(f.buffer.Bytes()),
	})
	if err != nil {
		return fmt.Errorf("failed to put S3 object: %w", err)
	}

	f.dirty = false
	return nil
}

// Flush implements explicit flushing
func (f *s3File) Flush() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return fs.ErrClosed
	}

	if f.dirty {
		return f.flush()
	}

	return nil
}

// s3FileInfo implements fs.FileInfo for S3 objects
type s3FileInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (fi *s3FileInfo) Name() string       { return fi.name }
func (fi *s3FileInfo) Size() int64        { return fi.size }
func (fi *s3FileInfo) Mode() os.FileMode  { return fi.mode }
func (fi *s3FileInfo) ModTime() time.Time { return time.Now() }
func (fi *s3FileInfo) IsDir() bool        { return false }
func (fi *s3FileInfo) Sys() interface{}   { return nil }
