package hours

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresJobs struct{ Pool *pgxpool.Pool }

func (q PostgresJobs) Migrate(ctx context.Context) error {
	_, err := q.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS generation_jobs (
		id text PRIMARY KEY,
		program_id text NOT NULL REFERENCES programs(id),
		expected_revision integer NOT NULL,
		result_revision integer NOT NULL DEFAULT 0,
		state text NOT NULL,
		attempts integer NOT NULL DEFAULT 0,
		error text NOT NULL DEFAULT '',
		locked_until timestamptz,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now(),
		UNIQUE(program_id,expected_revision)
	)`)
	return err
}

func scanJob(row pgx.Row) (GenerationJob, error) {
	var job GenerationJob
	err := row.Scan(&job.ID, &job.ProgramID, &job.ExpectedRevision, &job.ResultRevision, &job.State, &job.Attempts, &job.Error, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, ErrNotFound
	}
	return job, err
}

const jobColumns = `id,program_id,expected_revision,result_revision,state,attempts,error,updated_at`

func (q PostgresJobs) Enqueue(ctx context.Context, programID string, revision int) (GenerationJob, error) {
	return scanJob(q.Pool.QueryRow(ctx, `INSERT INTO generation_jobs (id,program_id,expected_revision,state)
		VALUES ($1,$2,$3,'queued')
		ON CONFLICT (program_id,expected_revision) DO UPDATE SET
		state=CASE WHEN generation_jobs.state='failed' THEN 'queued' ELSE generation_jobs.state END,
		error=CASE WHEN generation_jobs.state='failed' THEN '' ELSE generation_jobs.error END,
		updated_at=CASE WHEN generation_jobs.state='failed' THEN now() ELSE generation_jobs.updated_at END
		RETURNING `+jobColumns, newID(), programID, revision))
}

func (q PostgresJobs) Get(ctx context.Context, id string) (GenerationJob, error) {
	return scanJob(q.Pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM generation_jobs WHERE id=$1`, id))
}

func (q PostgresJobs) Claim(ctx context.Context) (GenerationJob, error) {
	return scanJob(q.Pool.QueryRow(ctx, `UPDATE generation_jobs SET state='running',attempts=attempts+1,
		locked_until=now()+interval '35 minutes',updated_at=now()
		WHERE id=(SELECT id FROM generation_jobs WHERE state='queued'
			OR (state='running' AND locked_until<now())
			ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
		RETURNING `+jobColumns))
}

func (q PostgresJobs) Finish(ctx context.Context, id string, resultRevision int, workErr error) error {
	state, message := JobDone, ""
	if workErr != nil {
		state, message = JobFailed, workErr.Error()
	}
	command, err := q.Pool.Exec(ctx, `UPDATE generation_jobs SET state=$2,result_revision=$3,error=$4,
		locked_until=NULL,updated_at=now() WHERE id=$1`, id, state, resultRevision, message)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
