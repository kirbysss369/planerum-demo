package hours

import (
	"context"
	"errors"
	"reflect"
	"time"
)

var ErrInvalidState = errors.New("action is not allowed in the current state")
var ErrGenerationValidation = errors.New("Материалы не прошли проверку; исправьте отмеченные ошибки или повторите генерацию")

type Service struct {
	Store     Store
	Generator Generator
}

func (s Service) Create(ctx context.Context, spec ProgramSpec) (Program, []string, error) {
	questions := MissingQuestions(spec)
	if len(questions) > 0 {
		return Program{}, questions, nil
	}
	if err := spec.Validate(); err != nil {
		return Program{}, nil, err
	}
	p, err := s.prepareOutline(ctx, Program{ID: newID(), State: Draft, Spec: spec}, 0)
	return p, nil, err
}

func (s Service) Approve(ctx context.Context, id string, expectedRevision int) (Program, error) {
	p, err := s.loadRevision(ctx, id, expectedRevision)
	if err != nil {
		return p, err
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
	p, err := s.generationInput(ctx, id, expectedRevision)
	if err != nil {
		return p, err
	}
	started := time.Now()
	generator, recorder := s.Generator.measured()
	materials, usage, err := generator.Materials(ctx, p)
	elapsed := time.Since(started)
	if err != nil {
		p.recordGeneration("materials", elapsed, usage, false, recorder.Calls...)
		p.Revision++
		if saveErr := s.persistGeneration(ctx, expectedRevision, p); saveErr != nil {
			return p, saveErr
		}
		return p, err
	}
	p.Assessments = materials.Assessments
	p.Lectures = materials.Lectures
	p.Sources = mergeSources(p.Sources, materials.Sources)
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
	p.GeneratedFromRevision = expectedRevision
	p.Issues = p.ValidateComplete()
	p.recordGeneration("materials", elapsed, usage, !hasErrors(p.Issues), recorder.Calls...)
	p.Revision++
	if hasErrors(p.Issues) {
		p.State = Approved
	} else {
		p.State = Ready
	}
	if saveErr := s.persistGeneration(ctx, expectedRevision, p); saveErr != nil {
		return p, saveErr
	}
	if p.State != Ready {
		return p, ErrGenerationValidation
	}
	return p, nil
}

// Revise replaces a proposed program revision with optimistic concurrency.
// Structural changes invalidate outline approval; material edits keep it.
func (s Service) Revise(ctx context.Context, id string, expectedRevision int, proposed Program) (Program, error) {
	p, err := s.loadRevision(ctx, id, expectedRevision)
	if err != nil {
		return p, err
	}
	if p.State == Draft {
		return p, ErrInvalidState
	}
	structural := !reflect.DeepEqual(p.Spec, proposed.Spec) || p.Goal != proposed.Goal || !sameOutlineOutcomes(p.Outcomes, proposed.Outcomes) || !reflect.DeepEqual(p.Modules, proposed.Modules) || p.FinalAttestation != proposed.FinalAttestation || !reflect.DeepEqual(standardSources(p), standardSources(proposed))
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

// generationInput is shared by synchronous and queued generation.
func (s Service) generationInput(ctx context.Context, id string, revision int) (Program, error) {
	p, err := s.loadRevision(ctx, id, revision)
	if err != nil {
		return p, err
	}
	if p.ApprovedRevision != p.OutlineRevision || (p.State != Approved && p.State != Ready) {
		return p, ErrInvalidState
	}
	return p, nil
}
