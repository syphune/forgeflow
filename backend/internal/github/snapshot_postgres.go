package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgeflow/forgeflow/backend/internal/intelligence"
	"github.com/forgeflow/forgeflow/backend/internal/platform/apperr"
	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/forgeflow/forgeflow/backend/internal/platform/ids"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresSnapshotStore struct {
	pool *pgxpool.Pool
}

func NewPostgresSnapshotStore(pool *pgxpool.Pool) *PostgresSnapshotStore {
	return &PostgresSnapshotStore{pool: pool}
}

func (s *PostgresSnapshotStore) SaveSnapshot(ctx context.Context, record SnapshotRecord, snapshot *intelligence.Snapshot) (SnapshotRecord, error) {
	if snapshot == nil {
		return SnapshotRecord{}, fmt.Errorf("snapshot is nil")
	}
	if record.ID == "" {
		id, err := ids.New()
		if err != nil {
			return SnapshotRecord{}, err
		}
		record.ID = id
	}
	err := platformdb.NewTransactionRunner(s.pool).WithinTransaction(ctx, func(txCtx context.Context) error {
		exec := platformdb.ExecutorFrom(txCtx, s.pool)
		var id string
		if err := exec.QueryRow(txCtx, `
INSERT INTO repository_snapshots (id,organization_id,project_id,repository_id,commit_sha,ref_name,status,file_count,symbol_count,skipped_count,error_message,started_at,finished_at,created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (organization_id,project_id,repository_id,commit_sha)
DO UPDATE SET ref_name=EXCLUDED.ref_name,status=EXCLUDED.status,file_count=EXCLUDED.file_count,symbol_count=EXCLUDED.symbol_count,skipped_count=EXCLUDED.skipped_count,error_message=EXCLUDED.error_message,started_at=EXCLUDED.started_at,finished_at=EXCLUDED.finished_at
RETURNING id::text`, record.ID, record.OrganizationID, record.ProjectID, record.RepositoryID, record.CommitSHA, record.RefName, record.Status, record.FileCount, record.SymbolCount, record.SkippedCount, record.ErrorMessage, record.StartedAt, record.FinishedAt, record.CreatedAt).Scan(&id); err != nil {
			return fmt.Errorf("save repository snapshot: %w", err)
		}
		record.ID = id
		if _, err := exec.Exec(txCtx, `DELETE FROM repository_files WHERE organization_id=$1 AND project_id=$2 AND snapshot_id=$3`, record.OrganizationID, record.ProjectID, record.ID); err != nil {
			return fmt.Errorf("replace repository snapshot files: %w", err)
		}
		if _, err := exec.Exec(txCtx, `DELETE FROM repository_symbols WHERE organization_id=$1 AND project_id=$2 AND snapshot_id=$3`, record.OrganizationID, record.ProjectID, record.ID); err != nil {
			return fmt.Errorf("replace repository snapshot symbols: %w", err)
		}
		if _, err := exec.Exec(txCtx, `DELETE FROM repository_edges WHERE organization_id=$1 AND project_id=$2 AND snapshot_id=$3`, record.OrganizationID, record.ProjectID, record.ID); err != nil {
			return fmt.Errorf("replace repository snapshot edges: %w", err)
		}
		for _, file := range snapshot.Files {
			content, err := snapshot.GetFile(file.Path)
			if err != nil {
				return fmt.Errorf("read indexed repository file %s: %w", file.Path, err)
			}
			if _, err := exec.Exec(txCtx, `INSERT INTO repository_files (organization_id,project_id,snapshot_id,path,language,size_bytes,content_hash,content) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, record.OrganizationID, record.ProjectID, record.ID, file.Path, file.Language, file.Size, file.ContentHash, content); err != nil {
				return fmt.Errorf("save repository file %s: %w", file.Path, err)
			}
		}
		for _, symbol := range snapshot.Symbols {
			if _, err := exec.Exec(txCtx, `INSERT INTO repository_symbols (organization_id,project_id,snapshot_id,path,name,qualified_name,kind,start_line,end_line,confidence,provenance) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, record.OrganizationID, record.ProjectID, record.ID, symbol.Path, symbol.Name, symbol.Qualified, symbol.Kind, symbol.StartLine, symbol.EndLine, symbol.Confidence, symbol.Provenance); err != nil {
				return fmt.Errorf("save repository symbol %s: %w", symbol.Name, err)
			}
		}
		for _, edge := range snapshot.Edges {
			if _, err := exec.Exec(txCtx, `INSERT INTO repository_edges (organization_id,project_id,snapshot_id,from_symbol,to_symbol,kind,confidence,provenance) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, record.OrganizationID, record.ProjectID, record.ID, edge.From, edge.To, edge.Kind, edge.Confidence, edge.Provenance); err != nil {
				return fmt.Errorf("save repository edge %s -> %s: %w", edge.From, edge.To, err)
			}
		}
		return nil
	})
	if err != nil {
		return SnapshotRecord{}, err
	}
	return record, nil
}

func (s *PostgresSnapshotStore) ListSnapshots(ctx context.Context, organizationID, projectID, repositoryID string, limit int) ([]SnapshotRecord, error) {
	limit = snapshotLimit(limit)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `SELECT id::text,organization_id::text,project_id::text,repository_id::text,commit_sha,ref_name,status,file_count,symbol_count,skipped_count,error_message,started_at,finished_at,created_at FROM repository_snapshots WHERE organization_id=$1 AND project_id=$2 AND repository_id=$3 ORDER BY COALESCE(finished_at,created_at) DESC,id DESC LIMIT $4`, organizationID, projectID, repositoryID, limit)
	if err != nil {
		return nil, fmt.Errorf("list repository snapshots: %w", err)
	}
	defer rows.Close()
	result := make([]SnapshotRecord, 0)
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository snapshot: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository snapshots: %w", err)
	}
	return result, nil
}

func (s *PostgresSnapshotStore) GetSnapshot(ctx context.Context, organizationID, projectID, repositoryID, snapshotID string) (SnapshotRecord, error) {
	item, err := scanSnapshot(platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `SELECT id::text,organization_id::text,project_id::text,repository_id::text,commit_sha,ref_name,status,file_count,symbol_count,skipped_count,error_message,started_at,finished_at,created_at FROM repository_snapshots WHERE organization_id=$1 AND project_id=$2 AND repository_id=$3 AND id=$4`, organizationID, projectID, repositoryID, snapshotID))
	if err == pgx.ErrNoRows {
		return SnapshotRecord{}, snapshotNotFound()
	}
	if err != nil {
		return SnapshotRecord{}, fmt.Errorf("get repository snapshot: %w", err)
	}
	return item, nil
}

func (s *PostgresSnapshotStore) GetSnapshotFile(ctx context.Context, organizationID, projectID, repositoryID, snapshotID, path string) (SnapshotFile, error) {
	if err := validateSnapshotPath(path); err != nil {
		return SnapshotFile{}, err
	}
	var item SnapshotFile
	var content []byte
	err := platformdb.ExecutorFrom(ctx, s.pool).QueryRow(ctx, `
SELECT f.path,f.language,f.size_bytes,f.content_hash,f.content
FROM repository_files f
JOIN repository_snapshots s ON s.organization_id=f.organization_id AND s.project_id=f.project_id AND s.id=f.snapshot_id
WHERE f.organization_id=$1 AND f.project_id=$2 AND s.repository_id=$3 AND f.snapshot_id=$4 AND f.path=$5`, organizationID, projectID, repositoryID, snapshotID, path).Scan(&item.Path, &item.Language, &item.Size, &item.ContentHash, &content)
	if err == pgx.ErrNoRows {
		return SnapshotFile{}, apperr.New(apperr.CodeNotFound, 404, "repository file not found", nil)
	}
	if err != nil {
		return SnapshotFile{}, fmt.Errorf("get repository snapshot file: %w", err)
	}
	item.Content = string(content)
	return item, nil
}

func (s *PostgresSnapshotStore) SearchSnapshot(ctx context.Context, organizationID, projectID, repositoryID, snapshotID, query string, limit int) ([]SnapshotFile, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, apperr.New(apperr.CodeInvalidArgument, 422, "snapshot search query must be 1-256 characters", nil)
	}
	limit = snapshotLimit(limit)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT f.path,f.language,f.size_bytes,f.content_hash,f.content
FROM repository_files f
JOIN repository_snapshots s ON s.organization_id=f.organization_id AND s.project_id=f.project_id AND s.id=f.snapshot_id
WHERE f.organization_id=$1 AND f.project_id=$2 AND s.repository_id=$3 AND f.snapshot_id=$4
ORDER BY f.path LIMIT 2000`, organizationID, projectID, repositoryID, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("search repository snapshot: %w", err)
	}
	defer rows.Close()
	needle := strings.ToLower(query)
	result := make([]SnapshotFile, 0, limit)
	for rows.Next() {
		var item SnapshotFile
		var content []byte
		if err := rows.Scan(&item.Path, &item.Language, &item.Size, &item.ContentHash, &content); err != nil {
			return nil, fmt.Errorf("scan repository search result: %w", err)
		}
		if strings.Contains(strings.ToLower(item.Path), needle) || strings.Contains(strings.ToLower(string(content)), needle) {
			result = append(result, item)
			if len(result) >= limit {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository search results: %w", err)
	}
	return result, nil
}

func (s *PostgresSnapshotStore) ListSnapshotSymbols(ctx context.Context, organizationID, projectID, repositoryID, snapshotID, name string, limit int) ([]SnapshotSymbol, error) {
	limit = snapshotLimit(limit)
	name = strings.TrimSpace(name)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT rs.path,rs.name,rs.qualified_name,rs.kind,rs.start_line,rs.end_line,rs.confidence,rs.provenance
FROM repository_symbols rs
JOIN repository_snapshots s ON s.organization_id=rs.organization_id AND s.project_id=rs.project_id AND s.id=rs.snapshot_id
WHERE rs.organization_id=$1 AND rs.project_id=$2 AND s.repository_id=$3 AND rs.snapshot_id=$4 AND ($5='' OR lower(rs.name)=lower($5) OR lower(rs.qualified_name)=lower($5))
ORDER BY rs.path,rs.start_line,rs.name LIMIT $6`, organizationID, projectID, repositoryID, snapshotID, name, limit)
	if err != nil {
		return nil, fmt.Errorf("list repository symbols: %w", err)
	}
	defer rows.Close()
	result := make([]SnapshotSymbol, 0, limit)
	for rows.Next() {
		var item SnapshotSymbol
		if err := rows.Scan(&item.Path, &item.Name, &item.Qualified, &item.Kind, &item.StartLine, &item.EndLine, &item.Confidence, &item.Provenance); err != nil {
			return nil, fmt.Errorf("scan repository symbol: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository symbols: %w", err)
	}
	return result, nil
}

func (s *PostgresSnapshotStore) ListSnapshotEdges(ctx context.Context, organizationID, projectID, repositoryID, snapshotID, from string, limit int) ([]SnapshotEdge, error) {
	limit = snapshotLimit(limit)
	from = strings.TrimSpace(from)
	rows, err := platformdb.ExecutorFrom(ctx, s.pool).Query(ctx, `
SELECT re.from_symbol,re.to_symbol,re.kind,re.confidence,re.provenance
FROM repository_edges re
JOIN repository_snapshots s ON s.organization_id=re.organization_id AND s.project_id=re.project_id AND s.id=re.snapshot_id
WHERE re.organization_id=$1 AND re.project_id=$2 AND s.repository_id=$3 AND re.snapshot_id=$4 AND ($5='' OR re.from_symbol=$5)
ORDER BY re.from_symbol,re.to_symbol,re.kind LIMIT $6`, organizationID, projectID, repositoryID, snapshotID, from, limit)
	if err != nil {
		return nil, fmt.Errorf("list repository edges: %w", err)
	}
	defer rows.Close()
	result := make([]SnapshotEdge, 0, limit)
	for rows.Next() {
		var item SnapshotEdge
		if err := rows.Scan(&item.From, &item.To, &item.Kind, &item.Confidence, &item.Provenance); err != nil {
			return nil, fmt.Errorf("scan repository edge: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read repository edges: %w", err)
	}
	return result, nil
}

type snapshotScanner interface {
	Scan(...any) error
}

func scanSnapshot(row snapshotScanner) (SnapshotRecord, error) {
	var item SnapshotRecord
	err := row.Scan(&item.ID, &item.OrganizationID, &item.ProjectID, &item.RepositoryID, &item.CommitSHA, &item.RefName, &item.Status, &item.FileCount, &item.SymbolCount, &item.SkippedCount, &item.ErrorMessage, &item.StartedAt, &item.FinishedAt, &item.CreatedAt)
	return item, err
}

func snapshotLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 20
	}
	return limit
}

func validateSnapshotPath(path string) error {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") || hasParentSegment(path) {
		return apperr.New(apperr.CodeInvalidArgument, 422, "snapshot path is invalid", nil)
	}
	return nil
}

func snapshotNotFound() error {
	return apperr.New(apperr.CodeNotFound, 404, "repository snapshot not found", nil)
}

var _ SnapshotStore = (*PostgresSnapshotStore)(nil)
