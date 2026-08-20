package attachment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type LocalBlobStore struct{ root string }

func NewLocalBlobStore(root string) (*LocalBlobStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("attachment storage directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve attachment storage directory: %w", err)
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, fmt.Errorf("create attachment storage directory: %w", err)
	}
	return &LocalBlobStore{root: abs}, nil
}

func (s *LocalBlobStore) Put(ctx context.Context, key string, source io.Reader, maxBytes int64) (Blob, error) {
	path, err := s.path(key)
	if err != nil {
		return Blob{}, err
	}
	if maxBytes <= 0 {
		maxBytes = MaxBytes
	}
	temporary, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return Blob{}, fmt.Errorf("create attachment upload: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), &contextReader{ctx: ctx, reader: io.LimitReader(source, maxBytes+1)})
	if copyErr != nil {
		_ = temporary.Close()
		return Blob{}, fmt.Errorf("write attachment: %w", copyErr)
	}
	if written > maxBytes {
		_ = temporary.Close()
		return Blob{}, fmt.Errorf("attachment exceeds %d bytes", maxBytes)
	}
	if err := temporary.Close(); err != nil {
		return Blob{}, fmt.Errorf("close attachment upload: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return Blob{}, fmt.Errorf("store attachment: %w", err)
	}
	return Blob{SizeBytes: written, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func (s *LocalBlobStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("attachment is not a regular file")
	}
	return os.Open(path)
}

func (s *LocalBlobStore) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s *LocalBlobStore) path(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || filepath.Base(key) != key || key == "." || key == ".." {
		return "", fmt.Errorf("invalid attachment storage key")
	}
	return filepath.Join(s.root, key), nil
}

type MemoryBlobStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func NewMemoryBlobStore() *MemoryBlobStore { return &MemoryBlobStore{blobs: make(map[string][]byte)} }

func (s *MemoryBlobStore) Put(ctx context.Context, key string, source io.Reader, maxBytes int64) (Blob, error) {
	if maxBytes <= 0 {
		maxBytes = MaxBytes
	}
	content, err := io.ReadAll(&contextReader{ctx: ctx, reader: io.LimitReader(source, maxBytes+1)})
	if err != nil {
		return Blob{}, err
	}
	if int64(len(content)) > maxBytes {
		return Blob{}, fmt.Errorf("attachment exceeds %d bytes", maxBytes)
	}
	digest := sha256.Sum256(content)
	s.mu.Lock()
	s.blobs[key] = append([]byte(nil), content...)
	s.mu.Unlock()
	return Blob{SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:])}, nil
}

func (s *MemoryBlobStore) Open(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	content, ok := s.blobs[key]
	s.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), content...))), nil
}

func (s *MemoryBlobStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	delete(s.blobs, key)
	s.mu.Unlock()
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(p)
	}
}

var _ BlobStore = (*LocalBlobStore)(nil)
var _ BlobStore = (*MemoryBlobStore)(nil)
