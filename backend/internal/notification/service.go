package notification

import (
	"context"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
)

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, actor identity.Actor, limit int) ([]Notification, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return s.store.List(ctx, actor.OrganizationID, actor.ID, limit)
}

func (s *Service) UnreadCount(ctx context.Context, actor identity.Actor) (int, error) {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return 0, err
	}
	return s.store.CountUnread(ctx, actor.OrganizationID, actor.ID)
}

func (s *Service) MarkRead(ctx context.Context, actor identity.Actor, id string) error {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return err
	}
	if strings.TrimSpace(id) == "" {
		return apperr.New(apperr.CodeInvalidArgument, 422, "notification id is required", nil)
	}
	return s.store.MarkRead(ctx, actor.OrganizationID, actor.ID, id)
}

func (s *Service) MarkAllRead(ctx context.Context, actor identity.Actor) error {
	if err := authorize(actor, identity.CapabilityProjectRead); err != nil {
		return err
	}
	return s.store.MarkAllRead(ctx, actor.OrganizationID, actor.ID)
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
