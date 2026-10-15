package hours

import (
	"context"
	"errors"
	"reflect"
	"strings"
)

var ErrInvalidState = errors.New("action is not allowed in the current state")

type Service struct {
	Store     Store
	Generator Generator
}

func MissingQuestions(spec ProgramSpec) []string {
	var questions []string
	if spec.Kind != Qualification && spec.Kind != General {
		questions = append(questions, "Какой тип программы нужен: повышение квалификации или общеразвивающая?")
	}
	if spec.Requested <= 0 {
		questions = append(questions, "Какой объем программы в академических часах?")
	}
	if strings.TrimSpace(spec.Audience) == "" || strings.TrimSpace(spec.EntryLevel) == "" {
		questions = append(questions, "Кто слушатели и каков их начальный уровень?")
	}
	if strings.TrimSpace(spec.Delivery) == "" {
		questions = append(questions, "Какая форма обучения и проведения занятий?")
	}
	if strings.TrimSpace(spec.Attestation) == "" {
		questions = append(questions, "Как пройдет итоговая аттестация?")
	}
	return questions
}

func (s Service) Create(ctx context.Context, spec ProgramSpec) (Program, []string, error) {
	questions := MissingQuestions(spec)
	if len(questions) > 0 {
		return Program{}, questions, nil
	}
	if err := spec.Validate(); err != nil {
		return Program{}, nil, err
	}
	outline, usage, err := s.Generator.Outline(ctx, spec)
	if err != nil {
		return Program{}, nil, err
	}
	p := Program{ID: newID(), Revision: 1, OutlineRevision: 1, State: OutlineReady, Spec: spec, Goal: outline.Goal, Outcomes: outline.Outcomes, Modules: outline.Modules, FinalAttestation: outline.FinalAttestation, Sources: outline.Sources, Usage: usage}
	p.Issues = p.ValidateOutline()
	if err := s.Store.Put(ctx, 0, p); err != nil {
		return Program{}, nil, err
	}
	return p, nil, nil
}

func (s Service) Approve(ctx context.Context, id string, expectedRevision int) (Program, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != expectedRevision {
		return p, ErrConflict
	}
	if p.State != OutlineReady {
		return p, ErrInvalidState
	}
	p.Issues = p.ValidateOutline()
	if hasErrors(p.Issues) {
		return p, ErrInvalidState
	}
	p.Revision++
	p.ApprovedRevision = p.OutlineRevision
	p.State = Approved
	return p, s.Store.Put(ctx, expectedRevision, p)
}

func (s Service) Generate(ctx context.Context, id string, expectedRevision int) (Program, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != expectedRevision {
		return p, ErrConflict
	}
	if p.ApprovedRevision != p.OutlineRevision || (p.State != Approved && p.State != Ready) {
		return p, ErrInvalidState
	}
	materials, usage, err := s.Generator.Materials(ctx, p)
	if err != nil {
		if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
			p.Usage.Add(usage)
			p.Revision++
			if saveErr := s.Store.Put(ctx, expectedRevision, p); saveErr != nil {
				return p, saveErr
			}
		}
		return p, err
	}
	p.Assessments = materials.Assessments
	p.Lectures = materials.Lectures
	p.Sources = append(p.Sources, materials.Sources...)
	for i := range p.Outcomes {
		p.Outcomes[i].AssessmentIDs = nil
		for _, a := range p.Assessments {
			for _, id := range a.OutcomeIDs {
				if id == p.Outcomes[i].ID {
					p.Outcomes[i].AssessmentIDs = append(p.Outcomes[i].AssessmentIDs, a.ID)
					break
				}
			}
		}
	}
	p.Usage.Add(usage)
	p.GeneratedFromRevision = expectedRevision
	p.Issues = p.ValidateComplete()
	p.Revision++
	if hasErrors(p.Issues) {
		p.State = Approved
	} else {
		p.State = Ready
	}
	return p, s.Store.Put(ctx, expectedRevision, p)
}

// Revise replaces a proposed program revision with optimistic concurrency.
// Structural changes invalidate outline approval; material edits keep it.
func (s Service) Revise(ctx context.Context, id string, expectedRevision int, proposed Program) (Program, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return p, err
	}
	if p.Revision != expectedRevision {
		return p, ErrConflict
	}
	structural := !reflect.DeepEqual(p.Spec, proposed.Spec) || p.Goal != proposed.Goal || !sameOutlineOutcomes(p.Outcomes, proposed.Outcomes) || !reflect.DeepEqual(p.Modules, proposed.Modules) || p.FinalAttestation != proposed.FinalAttestation
	p.Spec, p.Goal, p.Outcomes, p.Modules, p.FinalAttestation = proposed.Spec, proposed.Goal, proposed.Outcomes, proposed.Modules, proposed.FinalAttestation
	p.Assessments, p.Lectures, p.Sources = proposed.Assessments, proposed.Lectures, proposed.Sources
	p.Revision++
	if structural {
		p.OutlineRevision++
		p.ApprovedRevision = 0
		p.Assessments, p.Lectures = nil, nil
		for i := range p.Outcomes {
			p.Outcomes[i].AssessmentIDs = nil
		}
		p.State = OutlineReady
		p.GeneratedFromRevision = 0
		p.Issues = p.ValidateOutline()
	} else {
		p.Issues = p.ValidateComplete()
		if p.ApprovedRevision != p.OutlineRevision {
			p.State = OutlineReady
		} else if hasErrors(p.Issues) {
			p.State = Approved
		} else {
			p.State = Ready
		}
	}
	return p, s.Store.Put(ctx, expectedRevision, p)
}

func sameOutlineOutcomes(a, b []Outcome) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		x.AssessmentIDs, y.AssessmentIDs = nil, nil
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}
