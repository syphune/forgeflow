package github

import (
	"context"
	"fmt"

	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresKnowledgeStore struct{ pool *pgxpool.Pool }

func NewPostgresKnowledgeStore(pool *pgxpool.Pool) *PostgresKnowledgeStore {
	return &PostgresKnowledgeStore{pool: pool}
}

func (s *PostgresKnowledgeStore) Create(ctx context.Context, document KnowledgeDocument, revision KnowledgeRevision) (KnowledgeDocument, error) {
	err := platformdb.NewTransactionRunner(s.pool).WithinTransaction(ctx, func(txCtx context.Context) error {
		exec := platformdb.ExecutorFrom(txCtx, s.pool)
		if _, err := exec.Exec(txCtx, `INSERT INTO knowledge_documents (id,organization_id,project_id,repository_id,slug,title,kind,current_provenance,created_by,created_at,updated_at) VALUES ($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,$7,$8,$9,$10,$10)`, document.ID, document.OrganizationID, document.ProjectID, document.RepositoryID, document.Slug, document.Title, document.Kind, document.CurrentProvenance, document.CreatedBy, document.CreatedAt); err != nil {
			return fmt.Errorf("create knowledge document: %w", err)
		}
		if _, err := exec.Exec(txCtx, `INSERT INTO knowledge_revisions (id,organization_id,project_id,document_id,revision_number,content,provenance,source_snapshot_id,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10)`, revision.ID, document.OrganizationID, document.ProjectID, document.ID, revision.RevisionNumber, revision.Content, revision.Provenance, revision.SourceSnapshotID, revision.CreatedBy, revision.CreatedAt); err != nil {
			return fmt.Errorf("create knowledge revision: %w", err)
		}
		return nil
	})
	if err != nil {
		return KnowledgeDocument{}, err
	}
	document.LatestRevision = &revision
	return document, nil
}

func (s *PostgresKnowledgeStore) List(ctx context.Context, organizationID, projectID, repositoryID string, limit int) ([]KnowledgeDocument, error) {
	limit = knowledgeLimit(limit)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text,organization_id::text,project_id::text,COALESCE(repository_id::text,''),slug,title,kind,current_provenance,created_by,created_at,updated_at FROM knowledge_documents WHERE organization_id=$1 AND project_id=$2 AND (repository_id=NULLIF($3,'')::uuid OR repository_id IS NULL) ORDER BY updated_at DESC,id DESC LIMIT $4`, organizationID, projectID, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("list knowledge documents: %w", err)
	}
	defer rows.Close()
	result := make([]KnowledgeDocument, 0, limit)
	for rows.Next() {
		item, err := scanKnowledgeDocument(rows)
		if err != nil {
			return nil, fmt.Errorf("scan knowledge document: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *PostgresKnowledgeStore) Get(ctx context.Context, organizationID, projectID, repositoryID, documentID string) (KnowledgeDocument, error) {
	item, err := scanKnowledgeDocument(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text,organization_id::text,project_id::text,COALESCE(repository_id::text,''),slug,title,kind,current_provenance,created_by,created_at,updated_at FROM knowledge_documents WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND (repository_id=NULLIF($4,'')::uuid OR repository_id IS NULL)`, organizationID, projectID, documentID, repositoryID))
	if err == pgx.ErrNoRows {
		return KnowledgeDocument{}, knowledgeNotFound()
	}
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("get knowledge document: %w", err)
	}
	var revision KnowledgeRevision
	err = platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text,document_id::text,revision_number,content,provenance,COALESCE(source_snapshot_id::text,''),created_by,created_at FROM knowledge_revisions WHERE organization_id=$1 AND project_id=$2 AND document_id=$3 ORDER BY revision_number DESC LIMIT 1`, organizationID, projectID, documentID).Scan(&revision.ID, &revision.DocumentID, &revision.RevisionNumber, &revision.Content, &revision.Provenance, &revision.SourceSnapshotID, &revision.CreatedBy, &revision.CreatedAt)
	if err == nil {
		item.LatestRevision = &revision
	} else if err != pgx.ErrNoRows {
		return KnowledgeDocument{}, fmt.Errorf("get knowledge revision: %w", err)
	}
	return item, nil
}

func (s *PostgresKnowledgeStore) ListRevisions(ctx context.Context, organizationID, projectID, repositoryID, documentID string, limit int) ([]KnowledgeRevision, error) {
	limit = knowledgeLimit(limit)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT kr.id::text,kr.document_id::text,kr.revision_number,kr.content,kr.provenance,COALESCE(kr.source_snapshot_id::text,''),kr.created_by,kr.created_at
FROM knowledge_revisions kr
JOIN knowledge_documents kd ON kd.organization_id=kr.organization_id AND kd.project_id=kr.project_id AND kd.id=kr.document_id
WHERE kr.organization_id=$1 AND kr.project_id=$2 AND kr.document_id=$3
  AND (kd.repository_id=NULLIF($4,'')::uuid OR kd.repository_id IS NULL)
ORDER BY kr.revision_number DESC
LIMIT $5`, organizationID, projectID, documentID, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("list knowledge revisions: %w", err)
	}
	defer rows.Close()
	result := make([]KnowledgeRevision, 0, limit)
	for rows.Next() {
		var revision KnowledgeRevision
		if err := rows.Scan(&revision.ID, &revision.DocumentID, &revision.RevisionNumber, &revision.Content, &revision.Provenance, &revision.SourceSnapshotID, &revision.CreatedBy, &revision.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan knowledge revision: %w", err)
		}
		result = append(result, revision)
	}
	return result, rows.Err()
}

func (s *PostgresKnowledgeStore) AppendRevision(ctx context.Context, organizationID, projectID, repositoryID, documentID string, revision KnowledgeRevision) (KnowledgeRevision, error) {
	err := platformdb.NewTransactionRunner(s.pool).WithinTransaction(ctx, func(txCtx context.Context) error {
		exec := platformdb.ExecutorFrom(txCtx, s.pool)
		var provenance string
		var next int
		if err := exec.QueryRow(txCtx, `SELECT current_provenance,COALESCE((SELECT max(revision_number)+1 FROM knowledge_revisions WHERE organization_id=$1 AND project_id=$2 AND document_id=$3),1) FROM knowledge_documents WHERE organization_id=$1 AND project_id=$2 AND id=$3 AND (repository_id=NULLIF($4,'')::uuid OR repository_id IS NULL) FOR UPDATE`, organizationID, projectID, documentID, repositoryID).Scan(&provenance, &next); err != nil {
			if err == pgx.ErrNoRows {
				return knowledgeNotFound()
			}
			return fmt.Errorf("lock knowledge document: %w", err)
		}
		if provenance == "HUMAN_VERIFIED" && revision.Provenance != "HUMAN_VERIFIED" {
			return apperr.New(apperr.CodeConflict, 409, "verified knowledge cannot be overwritten by an unverified revision", nil)
		}
		revision.RevisionNumber = next
		if _, err := exec.Exec(txCtx, `INSERT INTO knowledge_revisions (id,organization_id,project_id,document_id,revision_number,content,provenance,source_snapshot_id,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')::uuid,$9,$10)`, revision.ID, organizationID, projectID, documentID, revision.RevisionNumber, revision.Content, revision.Provenance, revision.SourceSnapshotID, revision.CreatedBy, revision.CreatedAt); err != nil {
			return fmt.Errorf("append knowledge revision: %w", err)
		}
		if _, err := exec.Exec(txCtx, `UPDATE knowledge_documents SET current_provenance=$1,updated_at=$2 WHERE organization_id=$3 AND project_id=$4 AND id=$5`, revision.Provenance, revision.CreatedAt, organizationID, projectID, documentID); err != nil {
			return fmt.Errorf("update knowledge document: %w", err)
		}
		return nil
	})
	if err != nil {
		return KnowledgeRevision{}, err
	}
	return revision, nil
}

type knowledgeScanner interface{ Scan(...any) error }

func scanKnowledgeDocument(row knowledgeScanner) (KnowledgeDocument, error) {
	var item KnowledgeDocument
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.RepositoryID, &item.Slug, &item.Title, &item.Kind, &item.CurrentProvenance, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func knowledgeLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

func knowledgeNotFound() error {
	return apperr.New(apperr.CodeNotFound, 404, "knowledge document not found", nil)
}

var _ KnowledgeStore = (*PostgresKnowledgeStore)(nil)
