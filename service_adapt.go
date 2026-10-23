package hours

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

type ProgramOrigin struct {
	ProgramID string            `json:"program_id"`
	Revision  int               `json:"revision"`
	Reference *GeneratedOutline `json:"reference,omitempty"`
}

// Adapt creates a separate intake draft. Source content is a reference, never
// inherited approval or a substitute for newly generated course materials.
func (s Service) Adapt(ctx context.Context, id string, revision int, message string) (Program, error) {
	source, err := s.loadRevision(ctx, id, revision)
	if err != nil {
		return source, err
	}
	if source.State == Draft {
		return source, ErrInvalidState
	}
	reference := &GeneratedOutline{Goal: source.Goal, Outcomes: source.Outcomes, Modules: source.Modules, FinalAttestation: source.FinalAttestation, Sources: source.Sources}
	origin := &ProgramOrigin{ProgramID: source.ID, Revision: source.Revision, Reference: reference}
	probe := Program{Spec: source.Spec, Origin: origin}
	if _, err := outlineSpec(probe); err != nil {
		return Program{}, err
	}
	return s.intake(ctx, "", 0, message, source.Spec, origin)
}

// Keep the reference snapshot out of intake responses: the parser need not
// echo an old curriculum just to change the audience or duration.
func outlineSpec(p Program) (ProgramSpec, error) {
	spec := p.Spec
	if p.Origin == nil || p.Origin.Reference == nil {
		return spec, nil
	}
	reference, _ := json.Marshal(p.Origin.Reference)
	spec.Requirements += "\n\nПредыдущая программа для адаптации. Это референс: обновите содержание под новые условия; прежние часы и компетенции не являются обязательными.\n" + string(reference)
	if utf8.RuneCountInString(spec.Requirements) > intakeRequirementsLimit {
		return ProgramSpec{}, fmt.Errorf("%w: референс предыдущей программы превышает лимит требований", ErrInvalidInput)
	}
	return spec, nil
}
