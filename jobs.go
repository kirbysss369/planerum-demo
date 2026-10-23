package hours

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

type JobState string

const (
	JobQueued  JobState = "queued"
	JobRunning JobState = "running"
	JobDone    JobState = "done"
	JobFailed  JobState = "failed"
)

type GenerationJob struct {
	ID               string    `json:"id"`
	ProgramID        string    `json:"program_id"`
	ExpectedRevision int       `json:"expected_revision"`
	ResultRevision   int       `json:"result_revision,omitempty"`
	State            JobState  `json:"state"`
	Attempts         int       `json:"attempts"`
	Error            string    `json:"error,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type JobQueue interface {
	Enqueue(context.Context, string, int) (GenerationJob, error)
	Get(context.Context, string) (GenerationJob, error)
	Claim(context.Context) (GenerationJob, error)
	Finish(context.Context, string, int, error) error
}

type MemoryJobs struct {
	mu   sync.Mutex
	jobs map[string]GenerationJob
	ids  []string
}

func NewMemoryJobs() *MemoryJobs { return &MemoryJobs{jobs: map[string]GenerationJob{}} }

func (q *MemoryJobs) Enqueue(_ context.Context, programID string, revision int) (GenerationJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range q.ids {
		job := q.jobs[id]
		if job.ProgramID == programID && job.ExpectedRevision == revision {
			if job.State == JobFailed {
				job.State, job.Error, job.UpdatedAt = JobQueued, "", time.Now().UTC()
				q.jobs[id] = job
			}
			return job, nil
		}
	}
	job := GenerationJob{ID: newID(), ProgramID: programID, ExpectedRevision: revision, State: JobQueued, UpdatedAt: time.Now().UTC()}
	q.jobs[job.ID] = job
	q.ids = append(q.ids, job.ID)
	return job, nil
}

func (q *MemoryJobs) Get(_ context.Context, id string) (GenerationJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return job, ErrNotFound
	}
	return job, nil
}

func (q *MemoryJobs) Claim(_ context.Context) (GenerationJob, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, id := range q.ids {
		job := q.jobs[id]
		if job.State == JobQueued {
			job.State, job.Attempts, job.UpdatedAt = JobRunning, job.Attempts+1, time.Now().UTC()
			q.jobs[id] = job
			return job, nil
		}
	}
	return GenerationJob{}, ErrNotFound
}

func (q *MemoryJobs) Finish(_ context.Context, id string, resultRevision int, workErr error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	job, ok := q.jobs[id]
	if !ok {
		return ErrNotFound
	}
	job.ResultRevision, job.UpdatedAt = resultRevision, time.Now().UTC()
	if workErr == nil {
		job.State, job.Error = JobDone, ""
	} else {
		job.State, job.Error = JobFailed, workErr.Error()
	}
	q.jobs[id] = job
	return nil
}

// RunGenerationWorker claims one job at a time. PostgreSQL leases allow a
// replacement worker to reclaim a job after a process failure.
func RunGenerationWorker(ctx context.Context, queue JobQueue, service Service) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		job, err := queue.Claim(ctx)
		if err == nil {
			workCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			p, workErr := service.Generate(workCtx, job.ProgramID, job.ExpectedRevision)
			cancel()
			if errors.Is(workErr, ErrConflict) {
				latest, readErr := service.Store.Get(ctx, job.ProgramID)
				if readErr == nil && latest.Revision == job.ExpectedRevision+1 && latest.GeneratedFromRevision == job.ExpectedRevision && latest.State == Ready {
					p, workErr = latest, nil
				}
			}
			// Persist the terminal result even when shutdown canceled the work.
			finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			finishErr := queue.Finish(finishCtx, job.ID, p.Revision, workErr)
			finishCancel()
			if finishErr != nil {
				log.Printf("generation job %s: failed to persist result: %v", job.ID, finishErr)
			}
			continue
		}
		if !errors.Is(err, ErrNotFound) && ctx.Err() == nil {
			log.Printf("generation worker: claim failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
