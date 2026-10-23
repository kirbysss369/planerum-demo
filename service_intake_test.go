package hours

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type intakeStepModel struct {
	parsed       []ProgramSpec
	inputs       []IntakeInput
	parseErr     error
	outlineErr   error
	outlineCalls int
}

func (m *intakeStepModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	if out, ok := output.(*ParsedSpec); ok {
		m.inputs = append(m.inputs, input.(IntakeInput))
		if m.parseErr != nil {
			return Usage{11, 3}, m.parseErr
		}
		out.Spec = m.parsed[len(m.inputs)-1]
		return Usage{11, 3}, nil
	}
	m.outlineCalls++
	if m.outlineErr != nil {
		return Usage{7, 2}, m.outlineErr
	}
	_, err := (DemoModel{}).JSON(ctx, instruction, input, output)
	return Usage{7, 2}, err
}

func TestIntakeRequiresExplicitOutlineAndPreservesDialogue(t *testing.T) {
	ctx := context.Background()
	partial := ProgramSpec{Title: "Анализ данных", Audience: "Сотрудники банка"}
	complete := testSpec()
	complete.Title = "Анализ данных"
	complete.Requested = 72
	model := &intakeStepModel{parsed: []ProgramSpec{partial, complete}}
	store := NewMemoryStore()
	s := Service{Store: store, Generator: Generator{Model: model}}
	p, err := s.Intake(ctx, "", 0, "Нужен курс анализа данных для сотрудников банка", ProgramSpec{})
	if err != nil || p.State != Draft || p.Revision != 1 || len(p.Questions) == 0 || len(p.Dialogue) != 2 || model.outlineCalls != 0 {
		t.Fatalf("initial intake: %+v %v", p, err)
	}
	if _, err := s.Approve(ctx, p.ID, p.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("draft approval: %v", err)
	}
	if _, err := s.Generate(ctx, p.ID, p.Revision); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("draft generation: %v", err)
	}
	if _, err := s.Revise(ctx, p.ID, p.Revision, p); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("draft revision bypass: %v", err)
	}
	// A new service instance restores the saved draft through the store boundary.
	s = Service{Store: store, Generator: Generator{Model: model}}
	p, err = s.Intake(ctx, p.ID, p.Revision, "72 часа, общеразвивающая, начальный уровень, очно, защита проекта", ProgramSpec{})
	if err != nil || p.State != Draft || p.Revision != 2 || len(p.Questions) != 0 || len(p.Dialogue) != 4 || model.outlineCalls != 0 {
		t.Fatalf("followup: %+v %v", p, err)
	}
	if model.inputs[1].Spec.Title != partial.Title || len(model.inputs[1].Messages) != 3 {
		t.Fatal("known parameters/history lost")
	}
	if _, err := s.Intake(ctx, p.ID, 1, "Ещё уточнение", ProgramSpec{}); !errors.Is(err, ErrConflict) || len(model.inputs) != 2 {
		t.Fatalf("stale intake called model: %v", err)
	}
	p, qs, err := s.CreateFromDraft(ctx, p.ID, p.Revision, p.Spec)
	if err != nil || len(qs) != 0 || p.State != OutlineReady || p.Revision != 3 || len(p.Dialogue) != 4 || p.Usage != (Usage{29, 8}) || model.outlineCalls != 1 {
		t.Fatalf("create from draft: %+v %v", p, err)
	}
	if _, err := s.Intake(ctx, p.ID, p.Revision, "Измените часы", ProgramSpec{}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("intake changed outline: %v", err)
	}
}

func TestIntakeErrorsSaveUsageAndAllowRecovery(t *testing.T) {
	ctx := context.Background()
	model := &intakeStepModel{parseErr: errors.New("provider error")}
	s := Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}
	p, err := s.Intake(ctx, "", 0, "Нужен курс", testSpec())
	if err == nil || p.ID == "" || p.Revision != 1 || p.Usage != (Usage{11, 3}) || len(p.Dialogue) != 2 {
		t.Fatalf("failure lost draft/usage: %+v %v", p, err)
	}
	got, err := s.Store.Get(ctx, p.ID)
	if err != nil || got.Usage != p.Usage || got.Spec.Title != testSpec().Title {
		t.Fatal("failed parse not persisted")
	}
	model.parseErr = nil
	model.parsed = []ProgramSpec{{}, testSpec()}
	p, err = s.Intake(ctx, p.ID, p.Revision, "Всё верно", ProgramSpec{})
	if err != nil || p.Usage != (Usage{22, 6}) {
		t.Fatalf("recovery: %+v %v", p, err)
	}
	model.outlineErr = errors.New("outline provider error")
	p, _, err = s.CreateFromDraft(ctx, p.ID, p.Revision, p.Spec)
	if err == nil || p.State != Draft || p.Revision != 3 || p.Usage != (Usage{29, 8}) || len(p.Dialogue) != 4 {
		t.Fatalf("outline failure lost draft/usage: %+v %v", p, err)
	}
	model.outlineErr = nil
	p, _, err = s.CreateFromDraft(ctx, p.ID, p.Revision, p.Spec)
	if err != nil || p.State != OutlineReady || p.Usage != (Usage{36, 10}) {
		t.Fatalf("retry: %+v %v", p, err)
	}
}

func TestStructuredCreateFailureKeepsReturnedUsage(t *testing.T) {
	m := &intakeStepModel{outlineErr: errors.New("invalid JSON")}
	s := Service{Store: NewMemoryStore(), Generator: Generator{Model: m}}
	p, _, err := s.Create(context.Background(), testSpec())
	if err == nil || p.ID == "" || p.State != Draft || p.Usage != (Usage{7, 2}) {
		t.Fatalf("usage lost: %+v %v", p, err)
	}
	got, err := s.Store.Get(context.Background(), p.ID)
	if err != nil || got.Usage != p.Usage {
		t.Fatal("failed create usage not persisted")
	}
}

func TestDraftValidationAndMessageLimitsAvoidModelCalls(t *testing.T) {
	m := &intakeStepModel{}
	s := Service{Store: NewMemoryStore(), Generator: Generator{Model: m}}
	for _, message := range []string{" ", strings.Repeat("я", MaxIntakeMessage+1)} {
		if _, err := s.Intake(context.Background(), "", 0, message, ProgramSpec{}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("message accepted: %v", err)
		}
	}
	if len(m.inputs) != 0 {
		t.Fatal("invalid messages reached provider")
	}
	if qs := draftQuestions(ProgramSpec{}); len(qs) != 5 {
		t.Fatalf("empty questions=%v", qs)
	}
	for _, volume := range []Hours{15, 501, 24.001, -1} {
		spec := testSpec()
		spec.Requested = volume
		if len(draftQuestions(spec)) == 0 || !hasErrors(draftIssues(spec)) {
			t.Fatalf("invalid volume %v ignored", volume)
		}
	}
	p := Program{ID: newID(), Revision: 1, State: Draft, Spec: testSpec()}
	for range MaxDialogueTurns {
		p.Dialogue = append(p.Dialogue, DialogueMessage{Role: "user", Content: "уточнение"})
	}
	if err := s.Store.Put(context.Background(), 0, p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Intake(context.Background(), p.ID, p.Revision, "Ещё", ProgramSpec{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("turn limit ignored: %v", err)
	}
	if len(m.inputs) != 0 {
		t.Fatal("limited dialogue reached model")
	}
}

func TestIntakeHTTPContractAndLegacyCreate(t *testing.T) {
	model := &intakeStepModel{parsed: []ProgramSpec{{Title: "Python"}, testSpec()}}
	app := App{Service: Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}, Jobs: NewMemoryJobs(), Token: "test-token"}
	handler := app.Handler()
	call := func(payload any, auth bool, want int) AgentResponse {
		t.Helper()
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/v1/agent", strings.NewReader(string(body)))
		if auth {
			req.Header.Set("Authorization", "Bearer test-token")
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("HTTP %d want %d: %s", res.Code, want, res.Body.String())
		}
		var got AgentResponse
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	call(AgentRequest{Action: "intake", Message: "Python"}, false, http.StatusUnauthorized)
	call(map[string]any{"action": "intake", "message": "Python", "unknown": true}, true, http.StatusBadRequest)
	first := call(AgentRequest{Action: "intake", Message: "Python"}, true, 200)
	p := first.Program
	if p == nil || len(first.Questions) == 0 || p.State != Draft {
		t.Fatal("intake missing persisted program/questions")
	}
	status := call(AgentRequest{Action: "status", ProgramID: p.ID}, true, 200)
	if len(status.Program.Dialogue) != 2 || len(status.Questions) == 0 {
		t.Fatal("status lost dialog/questions")
	}
	call(AgentRequest{Action: "generate_async", ProgramID: p.ID, Revision: p.Revision}, true, 422)
	call(AgentRequest{Action: "answer", ProgramID: p.ID, Revision: 0, Message: "Уточнение"}, true, 409)
	p = call(AgentRequest{Action: "answer", ProgramID: p.ID, Revision: p.Revision, Message: "Уточнение"}, true, 200).Program
	id := p.ID
	p = call(AgentRequest{Action: "create", ProgramID: p.ID, Revision: p.Revision, Spec: p.Spec}, true, 200).Program
	if p.ID != id || p.State != OutlineReady || len(p.Dialogue) != 4 {
		t.Fatal("outline lost draft identity")
	}
	call(AgentRequest{Action: "create", ProgramID: p.ID, Revision: 1, Spec: p.Spec}, true, 409)
	legacy := call(AgentRequest{Action: "answer", Spec: testSpec()}, true, 200)
	if legacy.Program.State != OutlineReady {
		t.Fatal("legacy answer spec broken")
	}
}
