package hours

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testSpec() ProgramSpec {
	return ProgramSpec{Title: "Тестовая программа", Kind: General, Audience: "Начинающие", EntryLevel: "Нулевой", Delivery: "Очно", Requested: 24, Attestation: "Защита проекта"}
}

func TestServiceApprovalAndRevision(t *testing.T) {
	ctx := context.Background()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	if got := MissingQuestions(ProgramSpec{}); len(got) != 5 {
		t.Fatalf("questions = %d, want 5", len(got))
	}
	p, questions, err := svc.Create(ctx, testSpec())
	if err != nil || len(questions) != 0 || p.State != OutlineReady || hasErrors(p.Issues) {
		t.Fatalf("create: %+v %v %v", p, questions, err)
	}
	if _, err := svc.Generate(ctx, p.ID, p.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("generation before approval: %v", err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(ctx, p.ID, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale approval: %v", err)
	}
	p, err = svc.Generate(ctx, p.ID, p.Revision)
	if err != nil || p.State != Ready || hasErrors(p.Issues) {
		t.Fatalf("generate: %+v %v", p, err)
	}
	proposed := p
	proposed.Assessments = append([]Assessment(nil), p.Assessments...)
	proposed.Assessments[0].Answer = "Исправленный ответ"
	p, err = svc.Revise(ctx, p.ID, p.Revision, proposed)
	if err != nil || p.State != Ready || p.ApprovedRevision != p.OutlineRevision {
		t.Fatalf("material revision: %+v %v", p, err)
	}
	data, _ := json.Marshal(p)
	var structural Program
	_ = json.Unmarshal(data, &structural)
	structural.Modules[0].Topics[0].Title = "Измененная тема"
	p, err = svc.Revise(ctx, p.ID, p.Revision, structural)
	if err != nil || p.State != OutlineReady || p.ApprovedRevision != 0 {
		t.Fatalf("structural revision: %+v %v", p, err)
	}
	if _, err := svc.Generate(ctx, p.ID, p.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("generation after structural edit: %v", err)
	}
}

func TestIndependentInternshipDemo(t *testing.T) {
	spec := testSpec()
	spec.Kind, spec.Requested, spec.IndependentInternship = Qualification, 256, true
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	p, _, err := svc.Create(context.Background(), spec)
	if err != nil || hasErrors(p.Issues) {
		t.Fatalf("outline: %+v %v", p.Issues, err)
	}
	d, err := p.Distribution()
	if err != nil || d.SelfStudy != 254 || d.FinalAttestation != 2 || d.Lectures != 0 || d.Practice != 0 || d.Laboratory != 0 {
		t.Fatalf("distribution: %+v %v", d, err)
	}
}

func TestOutlineChecksCompetencyMapAndIDs(t *testing.T) {
	outline, _, err := (Generator{Model: DemoModel{}}).Outline(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p := Program{Spec: testSpec(), Goal: outline.Goal, Outcomes: outline.Outcomes, Modules: outline.Modules, FinalAttestation: outline.FinalAttestation}
	if hasErrors(p.ValidateOutline()) {
		t.Fatalf("valid demo map has issues: %+v", p.ValidateOutline())
	}
	p.Outcomes[0].Knowledge = nil
	if !hasErrors(p.ValidateOutline()) {
		t.Fatal("missing knowledge passed validation")
	}
	p.Outcomes[0].Knowledge = []string{"Демонстрационное знание"}
	p.Outcomes = append(p.Outcomes, p.Outcomes[0])
	if !hasErrors(p.ValidateOutline()) {
		t.Fatal("duplicate outcome ID passed validation")
	}
}

func TestCompleteRejectsOneSidedAssessmentCoverage(t *testing.T) {
	ctx := context.Background()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	second := p.Outcomes[0]
	second.ID, second.Text = "outcome-2", "Второй демонстрационный результат"
	p.Outcomes = append(p.Outcomes, second)
	p, err = svc.Revise(ctx, p.ID, p.Revision, p)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Generate(ctx, p.ID, p.Revision)
	if err != nil || p.State != Ready {
		t.Fatalf("valid two-outcome program: state=%s err=%v", p.State, err)
	}
	// All questions now assess the second outcome. The first still claims
	// coverage through its old IDs, so checking only that those IDs exist fails.
	p.Outcomes[1].AssessmentIDs = nil
	for i := range p.Assessments {
		p.Assessments[i].OutcomeIDs = []string{second.ID}
		p.Outcomes[1].AssessmentIDs = append(p.Outcomes[1].AssessmentIDs, p.Assessments[i].ID)
	}
	p, err = svc.Revise(ctx, p.ID, p.Revision, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.State == Ready || !hasErrors(p.ValidateComplete()) {
		t.Fatalf("unassessed outcome accepted as ready: %+v", p.Issues)
	}
	report, err := BuildValidationReport(p)
	if err != nil || report.StructurallyValid || report.OutcomesWithTwoMethods != 1 {
		t.Fatalf("report counts false coverage: %+v, err=%v", report, err)
	}
	if _, err := (Exporter{Root: t.TempDir(), Script: "office/export.py"}).Export(ctx, p); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("export of unassessed outcome: %v", err)
	}
}

func TestMaterialsAreGeneratedPerOutcomeAndLectureTopic(t *testing.T) {
	spec := testSpec()
	outline, _, err := (Generator{Model: DemoModel{}}).Outline(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	first := &outline.Modules[0].Topics[0]
	first.Activities[0].Hours = 3
	outline.Modules[0].Topics = append(outline.Modules[0].Topics, Topic{
		ID: "topic-2", Title: "Вторая тема", Activities: []Activity{{ID: "act-lecture-2", Name: "Лекция 2", Kind: "lecture", Hours: 1}},
	})
	outline.Outcomes = append(outline.Outcomes, Outcome{
		ID: "outcome-2", Text: "Второй результат", Knowledge: []string{"Знание"},
		Skills: []string{"Умение"}, Experience: []string{"Навык"}, TopicIDs: []string{"topic-2"},
	})
	p := Program{Spec: spec, Goal: outline.Goal, Outcomes: outline.Outcomes, Modules: outline.Modules, FinalAttestation: outline.FinalAttestation}
	if hasErrors(p.ValidateOutline()) {
		t.Fatalf("invalid multi-topic fixture: %+v", p.ValidateOutline())
	}
	materials, _, err := (Generator{Model: DemoModel{}}).Materials(context.Background(), p)
	if err != nil || len(materials.Assessments) != 4 || len(materials.Lectures) != 2 {
		t.Fatalf("materials = %+v, err=%v", materials, err)
	}
	if materials.Assessments[0].ID == materials.Assessments[2].ID || materials.Lectures[0].TopicID == materials.Lectures[1].TopicID {
		t.Fatalf("batches are not scoped: %+v", materials)
	}
}

type failingMaterialsModel struct{}

func (failingMaterialsModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	if _, ok := output.(*GeneratedAssessmentBatch); ok {
		return Usage{PromptTokens: 10, CompletionTokens: 5}, errors.New("model output failed")
	}
	return (DemoModel{}).JSON(ctx, instruction, input, output)
}

func TestFailedGenerationKeepsTokenUsage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := Service{Store: store, Generator: Generator{Model: failingMaterialsModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Generate(ctx, p.ID, p.Revision); err == nil {
		t.Fatal("expected generation failure")
	}
	updated, err := store.Get(ctx, p.ID)
	if err != nil || updated.Revision != 3 || updated.State != Approved || updated.Usage.PromptTokens != 10 || updated.Usage.CompletionTokens != 5 {
		t.Fatalf("failed generation usage = %+v, err=%v", updated, err)
	}
}

func TestStructuralFixturesAcrossCaseVolumes(t *testing.T) {
	cases := []struct {
		name        string
		kind        ProgramKind
		hours       Hours
		independent bool
	}{
		{"Python 16", General, 16, false},
		{"AI teachers 36", Qualification, 36, false},
		{"Bank data 72", Qualification, 72, false},
		{"DevOps 256", Qualification, 256, true},
		{"Boundary 500", Qualification, 500, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := testSpec()
			spec.Title, spec.Kind, spec.Requested, spec.IndependentInternship = tc.name, tc.kind, tc.hours, tc.independent
			svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
			ctx := context.Background()
			p, _, err := svc.Create(ctx, spec)
			if err != nil || hasErrors(p.Issues) {
				t.Fatalf("outline: %+v %v", p.Issues, err)
			}
			p, err = svc.Approve(ctx, p.ID, p.Revision)
			if err != nil {
				t.Fatal(err)
			}
			p, err = svc.Generate(ctx, p.ID, p.Revision)
			if err != nil || p.State != Ready || hasErrors(p.Issues) {
				t.Fatalf("materials: %+v %v", p.Issues, err)
			}
			d, err := p.Distribution()
			if err != nil || d.Validate() != nil {
				t.Fatalf("hours: %+v %v", d, err)
			}
		})
	}
}

func TestAgentAPIAuthAndWorkflow(t *testing.T) {
	app := App{Service: Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}, Token: "test-token"}
	call := func(body string, authorized bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/agent", strings.NewReader(body))
		if authorized {
			r.Header.Set("Authorization", "Bearer test-token")
		}
		w := httptest.NewRecorder()
		app.Handler().ServeHTTP(w, r)
		return w
	}
	if got := call(`{"action":"status","program_id":"x"}`, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", got)
	}
	if got := call(`{"action":"create","spec":{"title":"x","kind":"general","requested":24,"audience":"x","entry_level":"x","delivery":"x","attestation":"x"}}`, true).Code; got != http.StatusOK {
		t.Fatalf("create status = %d", got)
	}
}

func TestAsyncGenerationJob(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	jobs := NewMemoryJobs()
	app := App{Service: svc, Jobs: jobs}
	data, _ := json.Marshal(AgentRequest{Action: "generate_async", ProgramID: p.ID, Revision: p.Revision})
	w := httptest.NewRecorder()
	app.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agent", strings.NewReader(string(data))))
	if w.Code != http.StatusAccepted {
		t.Fatalf("enqueue status = %d: %s", w.Code, w.Body.String())
	}
	var response AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Job == nil || response.Job.State != JobQueued {
		t.Fatalf("enqueue response = %+v, err=%v", response, err)
	}
	done := make(chan struct{})
	go func() { RunGenerationWorker(ctx, jobs, svc); close(done) }()
	deadline := time.After(3 * time.Second)
	for {
		job, err := jobs.Get(ctx, response.Job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == JobDone {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("generation job remained %s: %s", job.State, job.Error)
		case <-time.After(10 * time.Millisecond):
		}
	}
	latest, err := svc.Store.Get(ctx, p.ID)
	if err != nil || latest.State != Ready {
		t.Fatalf("generated program = %+v, err=%v", latest, err)
	}
	cancel()
	<-done
}

func TestExportFiles(t *testing.T) {
	python := os.Getenv("OFFICE_PYTHON")
	if python == "" {
		t.Skip("set OFFICE_PYTHON with python-docx and python-pptx for office integration")
	}
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: DemoModel{}}}
	ctx := context.Background()
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Generate(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	exporter := Exporter{Root: t.TempDir(), Python: python, Script: filepath.Join("office", "export.py")}
	files, err := exporter.Export(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("files = %v", files)
	}
	// A second request reuses an immutable bundle even without an Office process.
	withoutPython := exporter
	withoutPython.Python = filepath.Join(t.TempDir(), "missing-python")
	if _, err := withoutPython.Export(ctx, p); err != nil {
		t.Fatalf("repeat export rebuilt existing files: %v", err)
	}
	for name := range files {
		path := filepath.Join(exporter.Root, p.ID, "revision-3", name)
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("artifact %s: %v", path, err)
		}
	}
	for _, name := range []string{"program.docx", "lectures.pptx"} {
		path := filepath.Join(exporter.Root, p.ID, "revision-3", name)
		archive, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, file := range archive.File {
			if file.Name != "docProps/core.xml" {
				continue
			}
			found = true
			reader, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			var properties struct {
				Title          string `xml:"http://purl.org/dc/elements/1.1/ title"`
				Creator        string `xml:"http://purl.org/dc/elements/1.1/ creator"`
				Description    string `xml:"http://purl.org/dc/elements/1.1/ description"`
				LastModifiedBy string `xml:"http://schemas.openxmlformats.org/package/2006/metadata/core-properties lastModifiedBy"`
			}
			if err := xml.Unmarshal(metadata, &properties); err != nil {
				t.Fatal(err)
			}
			if properties.Title != p.Spec.Title || properties.Creator != "" || properties.Description != "" || properties.LastModifiedBy != "" {
				t.Errorf("%s has unexpected core metadata: %+v", name, properties)
			}
		}
		archive.Close()
		if !found {
			t.Errorf("%s has no core metadata", name)
		}
	}
	report, err := BuildValidationReport(p)
	if err != nil || !report.StructurallyValid || report.OutcomesWithTwoMethods != len(p.Outcomes) || report.MethodicalReviewRequired != true {
		t.Fatalf("validation report = %+v, err=%v", report, err)
	}
}
