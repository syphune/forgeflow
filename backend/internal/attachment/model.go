package attachment

import (
	"context"
	"io"
	"time"
)

const MaxBytes int64 = 10 << 20

type Attachment struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	ProjectID      string    `json:"project_id"`
	WorkItemID     string    `json:"work_item_id"`
	Name           string    `json:"name"`
	ContentType    string    `json:"content_type"`
	StorageKey     string    `json:"-"`
	SHA256         string    `json:"sha256"`
	SizeBytes      int64     `json:"size_bytes"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
}

type Blob struct {
	SizeBytes int64
	SHA256    string
}

type BlobStore interface {
	Put(ctx context.Context, key string, source io.Reader, maxBytes int64) (Blob, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
