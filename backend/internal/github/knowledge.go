package github

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	"github.com/forgeflow/forgeflow/backend/internal/platform/identity"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
)

type KnowledgeDocument struct {
	ID                string             `json:"id"`
	OrganizationID    string             `json:"organization_id"`
	ProjectID         string             `json:"project_id"`
	RepositoryID      string             `json:"repository_id,omitempty"`
	Slug              string             `json:"slug"`
	Title             string             `json:"title"`
	Kind              string             `json:"kind"`
	CurrentProvenance string             `json:"current_provenance"`
	CreatedBy         string             `json:"created_by"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	LatestRevision    *KnowledgeRevision `json:"latest_revision,omitempty"`
}

type KnowledgeRevision struct {
	ID               string    `json:"id"`
	DocumentID       string    `json:"document_id"`
	RevisionNumber   int       `json:"revision_number"`
	Content          string    `json:"content"`
	Provenance       string    `json:"provenance"`
	SourceSnapshotID string    `json:"source_snapshot_id,omitempty"`
	CreatedBy        string    `json:"created_by"`
	CreatedAt        time.Time `json:"created_at"`
}

type KnowledgeInput struct {
	RepositoryID     string
	Slug             string
	Title            string
	Kind             string
	Content          string
	Provenance       string
	SourceSnapshotID string
}

type KnowledgeStore interface {
	Create(context.Context, KnowledgeDocument, KnowledgeRevision) (KnowledgeDocument, error)
	List(context.Context, string, string, string, int) ([]KnowledgeDocument, error)
	Get(context.Context, string, string, string, string) (KnowledgeDocument, error)
	ListRevisions(context.Context, string, string, string, string, int) ([]KnowledgeRevision, error)
	AppendRevision(context.Context, string, string, string, string, KnowledgeRevision) (KnowledgeRevision, error)
}

type KnowledgeService struct {
	parent *Service
	store  KnowledgeStore
}

func NewKnowledgeService(parent *Service, store KnowledgeStore) *KnowledgeService {
	return &KnowledgeService{parent: parent, store: store}
}

func (s *KnowledgeService) Create(ctx context.Context, actor identity.Actor, projectID string, input KnowledgeInput) (KnowledgeDocument, error) {
	if err := s.authorize(actor, true); err != nil {
		return KnowledgeDocument{}, err
	}
	if s.store == nil {
		return KnowledgeDocument{}, knowledgeUnavailable()
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, input.RepositoryID); err != nil {
			return KnowledgeDocument{}, err
		}
	}
	if err := validateKnowledgeInput(input); err != nil {
		return KnowledgeDocument{}, err
	}
	if input.Provenance == "HUMAN_VERIFIED" && actor.Type != "human" {
		return KnowledgeDocument{}, apperr.New(apperr.CodeAICannotVerify, http.StatusForbidden, "AI actors cannot verify knowledge", nil)
	}
	documentID, err := ids.New()
	if err != nil {
		return KnowledgeDocument{}, err
	}
	revisionID, err := ids.New()
	if err != nil {
		return KnowledgeDocument{}, err
	}
	now := time.Now().UTC()
	document := KnowledgeDocument{ID: documentID, OrganizationID: actor.OrganizationID, ProjectID: projectID, RepositoryID: strings.TrimSpace(input.RepositoryID), Slug: strings.TrimSpace(input.Slug), Title: strings.TrimSpace(input.Title), Kind: strings.TrimSpace(input.Kind), CurrentProvenance: strings.TrimSpace(input.Provenance), CreatedBy: actor.ID, CreatedAt: now, UpdatedAt: now}
	revision := KnowledgeRevision{ID: revisionID, DocumentID: documentID, RevisionNumber: 1, Content: input.Content, Provenance: input.Provenance, SourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID), CreatedBy: actor.ID, CreatedAt: now}
	created, err := s.store.Create(ctx, document, revision)
	if err != nil {
		return KnowledgeDocument{}, err
	}
	if s.parent != nil {
		if err := s.parent.record(ctx, actor, "knowledge.document.created", document.ID, nil, created); err != nil {
			return KnowledgeDocument{}, err
		}
	}
	return created, nil
}

func (s *KnowledgeService) List(ctx context.Context, actor identity.Actor, projectID, repositoryID string, limit int) ([]KnowledgeDocument, error) {
	if err := s.authorize(actor, false); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, knowledgeUnavailable()
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	return s.store.List(ctx, actor.OrganizationID, projectID, repositoryID, limit)
}

func (s *KnowledgeService) Get(ctx context.Context, actor identity.Actor, projectID, repositoryID, documentID string) (KnowledgeDocument, error) {
	if err := s.authorize(actor, false); err != nil {
		return KnowledgeDocument{}, err
	}
	if s.store == nil {
		return KnowledgeDocument{}, knowledgeUnavailable()
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return KnowledgeDocument{}, err
		}
	}
	return s.store.Get(ctx, actor.OrganizationID, projectID, repositoryID, documentID)
}

func (s *KnowledgeService) Revisions(ctx context.Context, actor identity.Actor, projectID, repositoryID, documentID string, limit int) ([]KnowledgeRevision, error) {
	if err := s.authorize(actor, false); err != nil {
		return nil, err
	}
	if s.store == nil {
		return nil, knowledgeUnavailable()
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return nil, err
		}
	}
	return s.store.ListRevisions(ctx, actor.OrganizationID, projectID, repositoryID, documentID, limit)
}

func (s *KnowledgeService) AppendRevision(ctx context.Context, actor identity.Actor, projectID, repositoryID, documentID string, input KnowledgeInput) (KnowledgeRevision, error) {
	if err := s.authorize(actor, true); err != nil {
		return KnowledgeRevision{}, err
	}
	if s.store == nil {
		return KnowledgeRevision{}, knowledgeUnavailable()
	}
	if s.parent != nil {
		if err := s.parent.requireLinkedRepository(ctx, actor.OrganizationID, projectID, repositoryID); err != nil {
			return KnowledgeRevision{}, err
		}
	}
	if strings.TrimSpace(input.Content) == "" || len(input.Content) > 512<<10 {
		return KnowledgeRevision{}, apperr.New(apperr.CodeInvalidArgument, 422, "knowledge content must be 1-512 KiB", nil)
	}
	if !validKnowledgeProvenance(input.Provenance) {
		return KnowledgeRevision{}, apperr.New(apperr.CodeInvalidArgument, 422, "knowledge provenance is invalid", nil)
	}
	if input.Provenance == "HUMAN_VERIFIED" && actor.Type != "human" {
		return KnowledgeRevision{}, apperr.New(apperr.CodeAICannotVerify, http.StatusForbidden, "AI actors cannot verify knowledge", nil)
	}
	revisionID, err := ids.New()
	if err != nil {
		return KnowledgeRevision{}, err
	}
	revision, err := s.store.AppendRevision(ctx, actor.OrganizationID, projectID, repositoryID, documentID, KnowledgeRevision{ID: revisionID, Provenance: input.Provenance, Content: input.Content, SourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID), CreatedBy: actor.ID, CreatedAt: time.Now().UTC()})
	if err != nil {
		return KnowledgeRevision{}, err
	}
	if s.parent != nil {
		if err := s.parent.record(ctx, actor, "knowledge.revision.created", revision.ID, nil, revision); err != nil {
			return KnowledgeRevision{}, err
		}
	}
	return revision, nil
}

func (s *KnowledgeService) authorize(actor identity.Actor, write bool) error {
	capability := identity.CapabilityRepositoryRead
	if write {
		capability = identity.CapabilityRepositoryManage
	}
	if !actor.Has(capability) {
		return apperr.New(apperr.CodeForbidden, http.StatusForbidden, "permission denied", map[string]any{"capability": capability})
	}
	if strings.TrimSpace(actor.OrganizationID) == "" {
		return apperr.New(apperr.CodeUnauthorized, http.StatusUnauthorized, "organization scope is required", nil)
	}
	return nil
}

func validateKnowledgeInput(input KnowledgeInput) error {
	if strings.TrimSpace(input.Slug) == "" || len(input.Slug) > 96 || !validKnowledgeSlug(input.Slug) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "knowledge slug is invalid", nil)
	}
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 160 || strings.TrimSpace(input.Content) == "" || len(input.Content) > 512<<10 {
		return apperr.New(apperr.CodeInvalidArgument, 422, "knowledge title/content is invalid", nil)
	}
	if !validKnowledgeKind(input.Kind) || !validKnowledgeProvenance(input.Provenance) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "knowledge kind or provenance is invalid", nil)
	}
	return nil
}

func validKnowledgeSlug(value string) bool {
	for index, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			if index == 0 && (r == '.' || r == '_' || r == '-') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

func validKnowledgeKind(value string) bool {
	switch strings.TrimSpace(value) {
	case "ARCHITECTURE", "CONVENTIONS", "TESTING", "DOMAIN_RULES", "KNOWN_ISSUES", "MODULE":
		return true
	default:
		return false
	}
}

func validKnowledgeProvenance(value string) bool {
	switch strings.TrimSpace(value) {
	case "MANUAL", "EXTRACTED", "AI_PROPOSED", "HUMAN_VERIFIED":
		return true
	default:
		return false
	}
}

func knowledgeUnavailable() error {
	return apperr.New(apperr.CodeInternal, http.StatusServiceUnavailable, "knowledge layer is not configured", nil)
}
