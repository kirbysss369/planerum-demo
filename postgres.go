package hours

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ Pool *pgxpool.Pool }

func NewPostgresStore(ctx context.Context, url string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PostgresStore{Pool: pool}, nil
}

func (s *PostgresStore) Migrate(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS programs (
		id text PRIMARY KEY,
		revision integer NOT NULL,
		payload jsonb NOT NULL,
		updated_at timestamptz NOT NULL DEFAULT now()
	)`)
	return err
}

func (s *PostgresStore) Get(ctx context.Context, id string) (Program, error) {
	var p Program
	var data []byte
	if err := s.Pool.QueryRow(ctx, `SELECT payload FROM programs WHERE id=$1`, id).Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, ErrNotFound
		}
		return p, err
	}
	return p, json.Unmarshal(data, &p)
}

func (s *PostgresStore) Put(ctx context.Context, expectedRevision int, p Program) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	query := `UPDATE programs SET revision=$2,payload=$3,updated_at=now() WHERE id=$1 AND revision=$4`
	args := []any{p.ID, p.Revision, data, expectedRevision}
	if expectedRevision == 0 {
		query = `INSERT INTO programs (id,revision,payload) VALUES ($1,$2,$3) ON CONFLICT (id) DO NOTHING`
		args = args[:3]
	}
	command, err := s.Pool.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}
