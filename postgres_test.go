package hours

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestPostgresStoreRoundTripAndConflict(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration test")
	}
	ctx := context.Background()
	store, err := NewPostgresStore(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Pool.Close)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	missing := Program{ID: newID(), Revision: 2}
	if err := store.Put(ctx, 1, missing); !errors.Is(err, ErrConflict) {
		t.Fatalf("update inserted a missing program: %v", err)
	}
	p := Program{ID: newID(), Revision: 1, State: OutlineReady, Spec: ProgramSpec{Title: "PostgreSQL test", Requested: 24}}
	t.Cleanup(func() { _, _ = store.Pool.Exec(context.Background(), "DELETE FROM programs WHERE id=$1", p.ID) })
	if err := store.Put(ctx, 0, p); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, 0, p); !errors.Is(err, ErrConflict) {
		t.Fatalf("create overwrote an existing program: %v", err)
	}
	got, err := store.Get(ctx, p.ID)
	if err != nil || got.Spec.Title != p.Spec.Title || got.Revision != 1 {
		t.Fatalf("round trip: got=%+v err=%v", got, err)
	}
	p.Revision = 2
	if err := store.Put(ctx, 1, p); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, 1, p); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale write = %v, want conflict", err)
	}
	jobs := PostgresJobs{Pool: store.Pool}
	if err := jobs.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := jobs.Enqueue(ctx, p.ID, p.Revision)
	if err != nil || job.State != JobQueued {
		t.Fatalf("enqueue: %+v %v", job, err)
	}
	t.Cleanup(func() {
		_, _ = store.Pool.Exec(context.Background(), "DELETE FROM generation_jobs WHERE id=$1", job.ID)
	})
	duplicate, err := jobs.Enqueue(ctx, p.ID, p.Revision)
	if err != nil || duplicate.ID != job.ID {
		t.Fatalf("duplicate: %+v %v", duplicate, err)
	}
	claimed, err := jobs.Claim(ctx)
	if err != nil || claimed.ID != job.ID || claimed.State != JobRunning || claimed.Attempts != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if err := jobs.Finish(ctx, job.ID, 3, nil); err != nil {
		t.Fatal(err)
	}
	finished, err := jobs.Get(ctx, job.ID)
	if err != nil || finished.State != JobDone || finished.ResultRevision != 3 {
		t.Fatalf("finished: %+v %v", finished, err)
	}
}
