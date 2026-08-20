package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	platformdb "github.com/forgeflow/forgeflow/backend/internal/platform/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInProgress = errors.New("idempotency key is already in progress")

type Claim struct {
	Replay       bool
	Status       int
	ResponseBody json.RawMessage
}

type Store interface {
	Claim(context.Context, string, string, string) (Claim, error)
	Complete(context.Context, string, string, string, int, []byte) error
	Release(context.Context, string, string, string) error
}

type PostgresStore struct{ pool *pgxpool.Pool }

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

func (s *PostgresStore) Claim(ctx context.Context, organizationID, actorID, key string) (Claim, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 200 {
		return Claim{}, errors.New("idempotency key is invalid")
	}
	exec := platformdb.ExecutorFrom(ctx, s.pool)
	_, _ = exec.Exec(ctx, `DELETE FROM idempotency_keys WHERE expires_at < now()`)
	tag, err := exec.Exec(ctx, `INSERT INTO idempotency_keys (organization_id,actor_id,key,expires_at) VALUES ($1,$2,$3,now()+interval '24 hours') ON CONFLICT (organization_id,actor_id,key) DO NOTHING`, organizationID, actorID, key)
	if err != nil {
		return Claim{}, err
	}
	if tag.RowsAffected() == 1 {
		return Claim{}, nil
	}
	var status *int
	var body []byte
	err = exec.QueryRow(ctx, `SELECT response_status,response_body FROM idempotency_keys WHERE organization_id=$1 AND actor_id=$2 AND key=$3 AND expires_at >= now()`, organizationID, actorID, key).Scan(&status, &body)
	if err == pgx.ErrNoRows {
		return Claim{}, errors.New("idempotency key expired")
	}
	if err != nil {
		return Claim{}, err
	}
	if status == nil {
		return Claim{}, ErrInProgress
	}
	return Claim{Replay: true, Status: *status, ResponseBody: append(json.RawMessage(nil), body...)}, nil
}

func (s *PostgresStore) Complete(ctx context.Context, organizationID, actorID, key string, status int, body []byte) error {
	if !json.Valid(body) {
		return errors.New("idempotency response is not valid JSON")
	}
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `UPDATE idempotency_keys SET response_status=$4,response_body=$5::jsonb WHERE organization_id=$1 AND actor_id=$2 AND key=$3`, organizationID, actorID, key, status, body)
	return err
}

func (s *PostgresStore) Release(ctx context.Context, organizationID, actorID, key string) error {
	_, err := platformdb.ExecutorFrom(ctx, s.pool).Exec(ctx, `DELETE FROM idempotency_keys WHERE organization_id=$1 AND actor_id=$2 AND key=$3 AND response_status IS NULL`, organizationID, actorID, key)
	return err
}

type MemoryStore struct {
	mu    sync.Mutex
	items map[string]memoryItem
}

type memoryItem struct {
	status    int
	body      json.RawMessage
	busy      bool
	expiresAt time.Time
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{items: make(map[string]memoryItem)} }

func (s *MemoryStore) Claim(_ context.Context, organizationID, actorID, key string) (Claim, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 200 {
		return Claim{}, errors.New("idempotency key is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key = organizationID + ":" + actorID + ":" + key
	if item, ok := s.items[key]; ok && item.expiresAt.After(time.Now()) {
		if item.busy {
			return Claim{}, ErrInProgress
		}
		return Claim{Replay: true, Status: item.status, ResponseBody: append(json.RawMessage(nil), item.body...)}, nil
	}
	s.items[key] = memoryItem{busy: true, expiresAt: time.Now().Add(24 * time.Hour)}
	return Claim{}, nil
}

func (s *MemoryStore) Complete(_ context.Context, organizationID, actorID, key string, status int, body []byte) error {
	if !json.Valid(body) {
		return errors.New("idempotency response is not valid JSON")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key = organizationID + ":" + actorID + ":" + strings.TrimSpace(key)
	item := s.items[key]
	item.busy = false
	item.status = status
	item.body = append(json.RawMessage(nil), body...)
	s.items[key] = item
	return nil
}

func (s *MemoryStore) Release(_ context.Context, organizationID, actorID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, organizationID+":"+actorID+":"+strings.TrimSpace(key))
	return nil
}

var _ Store = (*PostgresStore)(nil)
var _ Store = (*MemoryStore)(nil)
