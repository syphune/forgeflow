package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const RequiredSchemaVersion int64 = 27

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database config: %w", err)
	}
	config.MaxConns = 10
	config.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("open database pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func Ready(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("database pool is nil")
	}
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	var migrationsTable bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.schema_migrations') IS NOT NULL`).Scan(&migrationsTable); err != nil {
		return fmt.Errorf("check migration table: %w", err)
	}
	if !migrationsTable {
		return fmt.Errorf("database migrations have not been applied")
	}
	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&version, &dirty); err != nil {
		return fmt.Errorf("check migration state: %w", err)
	}
	if dirty {
		return fmt.Errorf("database migration is dirty")
	}
	if version < RequiredSchemaVersion {
		return fmt.Errorf("database migration is behind required version %d", RequiredSchemaVersion)
	}
	return nil
}
