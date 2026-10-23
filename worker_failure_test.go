package hours

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failureQueue struct {
	*MemoryJobs
	finished chan error
}

func (q failureQueue) Finish(ctx context.Context, id string, revision int, workErr error) error {
	if err := ctx.Err(); err != nil {
		q.finished <- err
		return err
	}
	err := q.MemoryJobs.Finish(ctx, id, revision, workErr)
	q.finished <- err
	return err
}

type shutdownModel struct{ started chan struct{} }

func (m shutdownModel) JSON(ctx context.Context, instruction string, input, output any) (Usage, error) {
	if _, ok := output.(*GeneratedAssessmentBatch); ok {
		close(m.started)
		<-ctx.Done()
		return Usage{PromptTokens: 7, CompletionTokens: 3}, ctx.Err()
	}
	return (DemoModel{}).JSON(ctx, instruction, input, output)
}

type invalidMaterialsModel struct{}

func (invalidMaterialsModel) JSON(ctx context.Context, instruction string, input, output any) (Usage, error) {
	usage, err := (DemoModel{}).JSON(ctx, instruction, input, output)
	if batch, ok := output.(*GeneratedAssessmentBatch); ok {
		batch.Assessments = append(batch.Assessments, Assessment{Kind: "test"})
		usage = Usage{PromptTokens: 7, CompletionTokens: 3}
	}
	return usage, err
}

func approvedFailureProgram(t *testing.T, model Model) (Service, Program) {
	t.Helper()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}
	p, _, err := svc.Create(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return svc, p
}

func TestWorkerPersistsJobFailureDuringShutdown(t *testing.T) {
	model := shutdownModel{started: make(chan struct{})}
	svc, p := approvedFailureProgram(t, model)
	q := failureQueue{MemoryJobs: NewMemoryJobs(), finished: make(chan error, 1)}
	job, err := q.Enqueue(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); RunGenerationWorker(ctx, q, svc) }()
	select {
	case <-model.started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop")
	}
	if err := <-q.finished; err != nil {
		t.Fatalf("shutdown lost terminal job state: %v", err)
	}
	got, _ := q.Get(context.Background(), job.ID)
	latest, _ := svc.Store.Get(context.Background(), p.ID)
	if got.State != JobFailed || got.ResultRevision != latest.Revision || latest.Revision != p.Revision+1 || latest.Usage != (Usage{PromptTokens: 7, CompletionTokens: 3}) {
		t.Fatalf("job=%+v program=%+v", got, latest)
	}
}

func TestInvalidGeneratedMaterialsAreReportedAsFailure(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP", true: "queue"}[async], func(t *testing.T) {
			svc, p := approvedFailureProgram(t, invalidMaterialsModel{})
			if !async {
				body, _ := json.Marshal(AgentRequest{Action: "generate", ProgramID: p.ID, Revision: p.Revision})
				response := httptest.NewRecorder()
				(App{Service: svc}).Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/agent", strings.NewReader(string(body))))
				var data AgentResponse
				json.Unmarshal(response.Body.Bytes(), &data)
				if response.Code != http.StatusUnprocessableEntity || data.Error == "" || data.Program == nil || data.Program.State != Approved {
					t.Fatalf("invalid output reported as success: %d %s", response.Code, response.Body.String())
				}
			} else {
				q := failureQueue{MemoryJobs: NewMemoryJobs(), finished: make(chan error, 1)}
				job, err := q.Enqueue(context.Background(), p.ID, p.Revision)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan struct{})
				go func() { defer close(done); RunGenerationWorker(ctx, q, svc) }()
				select {
				case err := <-q.finished:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("job did not finish")
				}
				cancel()
				<-done
				got, _ := q.Get(context.Background(), job.ID)
				if got.State != JobFailed || got.Error == "" {
					t.Fatalf("invalid output reported as done: %+v", got)
				}
			}
			latest, err := svc.Store.Get(context.Background(), p.ID)
			if err != nil || latest.State != Approved || !hasErrors(latest.Issues) || latest.Usage != (Usage{PromptTokens: 7, CompletionTokens: 3}) || latest.GenerationMetrics[len(latest.GenerationMetrics)-1].Succeeded {
				t.Fatalf("invalid attempt or its usage was lost: %+v %v", latest, err)
			}
		})
	}
}
