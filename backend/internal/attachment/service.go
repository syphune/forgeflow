package attachment

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/forgeflow/forgeflow/backend/internal/audit"
	"github.com/forgeflow/forgeflow/backend/internal/outbox"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type Service struct {
	store       Store
	blobs       BlobStore
	now         func() time.Time
	recorder    MutationRecorder
	transaction TransactionRunner
}

type MutationRecorder struct {
	Audit  audit.Writer
	Outbox outbox.Writer
}

type Options struct {
	Recorder    MutationRecorder
	Transaction TransactionRunner
}

type TransactionRunner interface {
	WithinTransaction(context.Context, func(context.Context) error) error
}

type directTransactionRunner struct{}

func (directTransactionRunner) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func NewService(store Store, blobs BlobStore, now func() time.Time, options ...Options) *Service {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	configured := Options{}
	if len(options) > 0 {
		configured = options[0]
	}
	if configured.Transaction == nil {
		configured.Transaction = directTransactionRunner{}
	}
	return &Service{store: store, blobs: blobs, now: now, recorder: configured.Recorder, transaction: configured.Transaction}
}

func (s *Service) List(ctx context.Context, actor identity.Actor, projectID, workItemID string) ([]Attachment, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(workItemID) == "" {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and work_item_id are required", nil)
	}
	return s.store.List(ctx, actor.OrganizationID, projectID, workItemID)
}

func (s *Service) ValidateReferences(ctx context.Context, organizationID, projectID, workItemID string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	items, err := s.store.List(ctx, organizationID, projectID, workItemID)
	if err != nil {
		return err
	}
	available := make(map[string]struct{}, len(items))
	for _, item := range items {
		available[item.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := available[id]; !ok {
			return apperr.New(apperr.CodeInvalidArgument, 422, "multimedia reference is not attached to this work item", nil)
		}
	}
	return nil
}

func (s *Service) Create(ctx context.Context, actor identity.Actor, projectID, workItemID, name, contentType string, source io.Reader) (Attachment, error) {
	if err := authorize(actor, identity.CapabilityWorkItemEdit); err != nil {
		return Attachment{}, err
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(workItemID) == "" {
		return Attachment{}, apperr.New(apperr.CodeInvalidArgument, 422, "project_id and work_item_id are required", nil)
	}
	name, err := safeName(name)
	if err != nil {
		return Attachment{}, err
	}
	contentType = normalizeContentType(contentType)
	id, err := ids.New()
	if err != nil {
		return Attachment{}, err
	}
	blob, err := s.blobs.Put(ctx, id, source, MaxBytes)
	if err != nil {
		if strings.Contains(err.Error(), "exceeds") {
			return Attachment{}, apperr.New(apperr.CodeInvalidArgument, 413, "attachment exceeds the 10 MiB limit", nil)
		}
		return Attachment{}, fmt.Errorf("store attachment content: %w", err)
	}
	var item Attachment
	err = s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		var err error
		item, err = s.store.Create(txCtx, Attachment{ID: id, OrganizationID: actor.OrganizationID, ProjectID: projectID, WorkItemID: workItemID, Name: name, ContentType: contentType, StorageKey: id, SHA256: blob.SHA256, SizeBytes: blob.SizeBytes, CreatedBy: actor.ID, CreatedAt: s.now().UTC()})
		if err != nil {
			return err
		}
		return s.record(txCtx, actor, "attachment.created", item.ID, nil, item)
	})
	if err != nil {
		_ = s.blobs.Delete(context.Background(), id)
		return Attachment{}, err
	}
	return item, nil
}

func (s *Service) Open(ctx context.Context, actor identity.Actor, projectID, workItemID, id string) (Attachment, io.ReadCloser, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return Attachment{}, nil, err
	}
	item, err := s.store.Get(ctx, actor.OrganizationID, projectID, workItemID, id)
	if err != nil {
		return Attachment{}, nil, err
	}
	reader, err := s.blobs.Open(ctx, item.StorageKey)
	if err != nil {
		return Attachment{}, nil, apperr.New(apperr.CodeInternal, 500, "attachment content is unavailable", nil)
	}
	return item, reader, nil
}

func (s *Service) Delete(ctx context.Context, actor identity.Actor, projectID, workItemID, id string) error {
	if err := authorize(actor, identity.CapabilityWorkItemEdit); err != nil {
		return err
	}
	item, err := s.store.Get(ctx, actor.OrganizationID, projectID, workItemID, id)
	if err != nil {
		return err
	}
	if err := s.transaction.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.store.Delete(txCtx, actor.OrganizationID, projectID, workItemID, id); err != nil {
			return err
		}
		return s.record(txCtx, actor, "attachment.deleted", item.ID, item, nil)
	}); err != nil {
		return err
	}
	if err := s.blobs.Delete(ctx, item.StorageKey); err != nil {
		return fmt.Errorf("delete attachment content: %w", err)
	}
	return nil
}

func (s *Service) record(ctx context.Context, actor identity.Actor, action, resourceID string, before, after any) error {
	if s.recorder.Audit != nil {
		if err := s.recorder.Audit.Record(ctx, audit.Record{ActorType: actor.Type, ActorID: actor.ID, OrganizationID: actor.OrganizationID, Source: actor.Source, Action: action, ResourceType: "attachment", ResourceID: resourceID, Before: before, After: after, CreatedAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	if s.recorder.Outbox != nil {
		eventID, err := ids.New()
		if err != nil {
			return err
		}
		if err := s.recorder.Outbox.Append(ctx, outbox.Event{ID: eventID, OrganizationID: actor.OrganizationID, EventType: action, AggregateType: "attachment", AggregateID: resourceID, IdempotencyKey: action + ":" + resourceID, Payload: map[string]any{"attachment_id": resourceID}, OccurredAt: s.now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func safeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		return "", apperr.New(apperr.CodeInvalidArgument, 422, "attachment filename is invalid", nil)
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "", apperr.New(apperr.CodeInvalidArgument, 422, "attachment filename is invalid", nil)
		}
	}
	return filepath.Base(value), nil
}

func normalizeContentType(value string) string {
	value = strings.TrimSpace(value)
	if parsed, _, err := mime.ParseMediaType(value); err == nil && parsed != "" {
		value = parsed
	}
	if value == "" || len(value) > 128 {
		return "application/octet-stream"
	}
	return value
}

func authorize(actor identity.Actor, capability string) error {
	if strings.TrimSpace(actor.ID) == "" || strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, 401, "authenticated actor is required", nil)
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, 403, "permission denied", map[string]any{"capability": capability})
	}
	return nil
}
