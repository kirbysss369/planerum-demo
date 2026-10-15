package hours

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readyProgram(t *testing.T) (Service, Program) {
	t.Helper()
	ctx := context.Background()
	s := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	p, _, err := s.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Generate(ctx, p.ID, p.Revision)
	if err != nil || p.State != Ready {
		t.Fatalf("generate: %s %v", p.State, err)
	}
	return s, p
}

func TestCreateRejectsInvalidVolumeBeforeModelCall(t *testing.T) {
	for _, volume := range []Hours{15, 501, 24.001} {
		spec := testSpec()
		spec.Requested = volume
		// A nil model would return a different error if validation ran too late.
		s := Service{Store: NewMemoryStore()}
		if _, _, err := s.Create(context.Background(), spec); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("volume %v reached generation: %v", volume, err)
		}
	}
}

func TestRevisionCannotGrantOutlineApproval(t *testing.T) {
	ctx := context.Background()
	s := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	p, _, err := s.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.Revise(ctx, p.ID, p.Revision, p)
	if err != nil || p.State != OutlineReady || p.ApprovedRevision != 0 {
		t.Fatalf("revision implicitly approved outline: %+v %v", p, err)
	}
	p, err = s.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Approve(ctx, p.ID, p.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("repeat approval changed state: %v", err)
	}
}

func TestBlankKnowledgeAndSlidesAreInvalid(t *testing.T) {
	_, p := readyProgram(t)
	p.Outcomes[0].Knowledge = []string{" "}
	if !hasErrors(p.ValidateOutline()) {
		t.Fatal("blank knowledge accepted")
	}
	p.Outcomes[0].Knowledge = []string{"Знание"}
	p.Lectures[0].Slides = []string{"\n\t"}
	if !hasErrors(p.ValidateComplete()) {
		t.Fatal("blank slide accepted")
	}
}

func TestQueueDoesNotMistakeManualRevisionForGeneration(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		s, p := readyProgram(t)
		q := NewMemoryJobs()
		expected := p.Revision
		if recovered {
			expected = p.GeneratedFromRevision
		} else {
			p.Assessments[0].Answer = "Исправленный ответ"
			var err error
			p, err = s.Revise(context.Background(), p.ID, p.Revision, p)
			if err != nil {
				t.Fatal(err)
			}
		}
		job, err := q.Enqueue(context.Background(), p.ID, expected)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		done := make(chan struct{})
		go func() { defer close(done); RunGenerationWorker(ctx, q, s) }()
		for {
			got, err := q.Get(ctx, job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.State == JobDone || got.State == JobFailed {
				want := JobFailed
				if recovered {
					want = JobDone
				}
				cancel()
				<-done
				if got.State != want {
					t.Fatalf("recovered=%v: got %s, want %s", recovered, got.State, want)
				}
				break
			}
			select {
			case <-ctx.Done():
				cancel()
				<-done
				t.Fatal("worker did not finish")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

func TestFailedExportPublishesNoPartialFiles(t *testing.T) {
	_, p := readyProgram(t)
	root := t.TempDir()
	e := Exporter{Root: root, Python: filepath.Join(root, "missing-python"), Script: "office/export.py"}
	if _, err := e.Export(context.Background(), p); err == nil {
		t.Fatal("expected failed Office process")
	}
	entries, err := os.ReadDir(filepath.Join(root, p.ID))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed export left files: %v %v", entries, err)
	}
}

func TestArtifactRouteDoesNotExposeDirectories(t *testing.T) {
	root := t.TempDir()
	id := newID()
	dir := filepath.Join(root, id, "revision-1")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "program.json"), []byte(`{"demo":true}`), 0o640); err != nil {
		t.Fatal(err)
	}
	app := App{Exporter: Exporter{Root: root}, Token: "test-token"}
	for path, want := range map[string]int{
		"/artifacts/":                                     404,
		"/artifacts/" + id + "/":                          404,
		"/artifacts/" + id + "/revision-1/":               404,
		"/artifacts/" + id + "/revision-1/program.json":   200,
		"/artifacts/" + id + "/.export-test/program.json": 404,
		"/artifacts/" + id + "/revision-1/unexpected.txt": 404,
		"/missing": 404,
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer test-token")
		app.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("%s: got %d, want %d", path, w.Code, want)
		}
	}
}

func TestMoodleRetainsGradingCriteriaAsPlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tests.xml")
	assessments := []Assessment{{ID: "task", Kind: "practice", Question: "Сравните a < b", Rubric: "Объяснение и воспроизводимый результат"}}
	if err := writeMoodle(path, assessments); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), assessments[0].Rubric) || !strings.Contains(string(raw), `graderinfo format="plain_text"`) || !strings.Contains(string(raw), "a &lt; b") {
		t.Fatalf("grading criteria or text format lost: %s", raw)
	}
}
