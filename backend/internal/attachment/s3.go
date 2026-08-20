package attachment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StorageConfig struct {
	Mode      string
	LocalDir  string
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	Bucket    string
	Prefix    string
	Secure    bool
}

type S3BlobStore struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewBlobStore(config StorageConfig) (BlobStore, error) {
	if strings.EqualFold(strings.TrimSpace(config.Mode), "s3") {
		return NewS3BlobStore(config)
	}
	return NewLocalBlobStore(config.LocalDir)
}

func NewS3BlobStore(config StorageConfig) (*S3BlobStore, error) {
	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" || strings.TrimSpace(config.AccessKey) == "" || strings.TrimSpace(config.SecretKey) == "" || strings.TrimSpace(config.Bucket) == "" {
		return nil, fmt.Errorf("S3 attachment storage requires endpoint, access key, secret key, and bucket")
	}
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		if parsed.Path != "" && parsed.Path != "/" {
			return nil, fmt.Errorf("S3 attachment endpoint must not contain a path")
		}
		if parsed.Scheme == "https" {
			config.Secure = true
		}
		endpoint = parsed.Host
	} else if strings.Contains(endpoint, "/") {
		return nil, fmt.Errorf("S3 attachment endpoint is invalid")
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.Secure,
		Region: strings.TrimSpace(config.Region),
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 attachment client: %w", err)
	}
	prefix := strings.Trim(strings.TrimSpace(config.Prefix), "/")
	return &S3BlobStore{client: client, bucket: strings.TrimSpace(config.Bucket), prefix: prefix}, nil
}

func (s *S3BlobStore) Put(ctx context.Context, key string, source io.Reader, maxBytes int64) (Blob, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return Blob{}, err
	}
	if maxBytes <= 0 {
		maxBytes = MaxBytes
	}
	content, err := io.ReadAll(&contextReader{ctx: ctx, reader: io.LimitReader(source, maxBytes+1)})
	if err != nil {
		return Blob{}, fmt.Errorf("read attachment: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return Blob{}, fmt.Errorf("attachment exceeds %d bytes", maxBytes)
	}
	if _, err := s.client.PutObject(ctx, s.bucket, objectKey, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		return Blob{}, fmt.Errorf("put attachment object: %w", err)
	}
	digest := sha256.Sum256(content)
	return Blob{SizeBytes: int64(len(content)), SHA256: hex.EncodeToString(digest[:])}, nil
}

func (s *S3BlobStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return nil, err
	}
	if _, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{}); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return object, nil
}

func (s *S3BlobStore) Delete(ctx context.Context, key string) error {
	objectKey, err := s.objectKey(key)
	if err != nil {
		return err
	}
	return s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{})
}

func (s *S3BlobStore) objectKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || path.Base(key) != key || key == "." || key == ".." {
		return "", fmt.Errorf("invalid attachment storage key")
	}
	if s.prefix == "" {
		return key, nil
	}
	return path.Join(s.prefix, key), nil
}

var _ BlobStore = (*S3BlobStore)(nil)
