package hours

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxIntakeMessage = 8000
	MaxDialogueTurns = 20
	MaxDialogueText  = 64000
)

// Intake saves each turn before the user explicitly requests an outline.
// The model extracts parameters; the service owns questions and state transitions.
func (s Service) Intake(ctx context.Context, id string, revision int, message string, seed ProgramSpec) (Program, error) {
	return s.intake(ctx, id, revision, message, seed, nil)
}

func (s Service) intake(ctx context.Context, id string, revision int, message string, seed ProgramSpec, origin *ProgramOrigin) (Program, error) {
	message = strings.TrimSpace(message)
	if message == "" || utf8.RuneCountInString(message) > MaxIntakeMessage {
		return Program{}, fmt.Errorf("%w: сообщение должно содержать от 1 до %d символов", ErrInvalidInput, MaxIntakeMessage)
	}
	p := Program{ID: newID(), State: Draft, Origin: origin}
	if id != "" {
		var err error
		p, err = s.loadRevision(ctx, id, revision)
		if err != nil {
			return p, err
		}
		if p.State != Draft {
			return p, ErrInvalidState
		}
	} else if revision != 0 {
		return Program{}, ErrConflict
	}
	turns, size := 0, utf8.RuneCountInString(message)
	for _, item := range p.Dialogue {
		if item.Role == "user" {
			turns++
		}
		size += utf8.RuneCountInString(item.Content)
	}
	if turns >= MaxDialogueTurns || size > MaxDialogueText {
		return p, fmt.Errorf("%w: диалог достиг лимита; проверьте параметры или начните новый запрос", ErrInvalidInput)
	}
	if seed != (ProgramSpec{}) {
		p.Spec = seed
	}
	if err := validateSpecText(p.Spec); err != nil {
		return p, err
	}
	p.Dialogue = append(p.Dialogue, DialogueMessage{Role: "user", Content: message})
	started := time.Now()
	generator, recorder := s.Generator.measured()
	parsed, usage, modelErr := generator.Interpret(ctx, p.Spec, p.Dialogue)
	if modelErr == nil {
		if err := validateSpecText(parsed); err != nil {
			modelErr = err
		}
	}
	if modelErr == nil {
		if parsed.Requirements == "" {
			parsed.Requirements = p.Spec.Requirements
		}
		p.Spec = parsed
	}
	p.recordGeneration("intake", time.Since(started), usage, modelErr == nil, recorder.Calls...)
	p.Revision++
	p.Questions = draftQuestions(p.Spec)
	p.Issues = draftIssues(p.Spec)
	reply := "Параметры собраны. Проверьте их и нажмите «Подготовить каркас»."
	if len(p.Questions) > 0 {
		reply = strings.Join(p.Questions, "\n")
	}
	if modelErr != nil {
		reply = "Не удалось разобрать сообщение. Диалог сохранён; повторите уточнение или заполните параметры вручную."
	}
	p.Dialogue = append(p.Dialogue, DialogueMessage{Role: "assistant", Content: reply})
	if err := s.persistGeneration(ctx, revision, p); err != nil {
		return Program{}, err
	}
	return p, modelErr
}

// CreateFromDraft preserves the draft ID, conversation and accumulated usage.
// A complete intake is still a draft until the user explicitly requests this step.
func (s Service) CreateFromDraft(ctx context.Context, id string, revision int, spec ProgramSpec) (Program, []string, error) {
	p, err := s.loadRevision(ctx, id, revision)
	if err != nil {
		return p, nil, err
	}
	if p.State != Draft {
		return p, nil, ErrInvalidState
	}
	if err := validateSpecText(spec); err != nil {
		return p, nil, err
	}
	p.Spec = spec
	p.Questions = draftQuestions(spec)
	p.Issues = draftIssues(spec)
	if len(p.Questions) > 0 {
		p.Revision++
		return p, p.Questions, s.Store.Put(ctx, revision, p)
	}
	if err := spec.Validate(); err != nil {
		return p, nil, err
	}
	p, err = s.prepareOutline(ctx, p, revision)
	return p, p.Questions, err
}

func (s Service) loadRevision(ctx context.Context, id string, revision int) (Program, error) {
	p, err := s.Store.Get(ctx, id)
	if err != nil {
		return Program{}, err
	}
	if p.Revision != revision {
		return Program{}, ErrConflict
	}
	return p, nil
}

func draftQuestions(spec ProgramSpec) []string {
	questions := MissingQuestions(spec)
	if spec.Requested > 0 {
		_, err := spec.Requested.minutes()
		if spec.Requested < 16 || spec.Requested > 500 || err != nil {
			questions = append(questions, "Укажите корректный объём от 16 до 500 академических часов с точностью до минуты.")
		}
	}
	return questions
}

func draftIssues(spec ProgramSpec) []Issue {
	if spec.Requested != 0 {
		_, err := spec.Requested.minutes()
		if spec.Requested < 16 || spec.Requested > 500 || err != nil {
			return []Issue{{Severity: "error", Path: "spec.requested", Message: "Объём должен быть от 16 до 500 часов с точностью до минуты."}}
		}
	}
	return nil
}

func validateSpecText(spec ProgramSpec) error {
	for field, text := range map[string]string{"title": spec.Title, "audience": spec.Audience, "entry_level": spec.EntryLevel, "delivery": spec.Delivery, "attestation": spec.Attestation} {
		if utf8.RuneCountInString(text) > 500 {
			return fmt.Errorf("%w: поле %s превышает 500 символов", ErrInvalidInput, field)
		}
	}
	if utf8.RuneCountInString(spec.Requirements) > 64000 {
		return fmt.Errorf("%w: требования превышают 64000 символов", ErrInvalidInput)
	}
	return nil
}

// prepareOutline keeps the same persistence and usage rules for both entry paths.
func (s Service) prepareOutline(ctx context.Context, p Program, revision int) (Program, error) {
	started := time.Now()
	modelSpec, err := outlineSpec(p)
	if err != nil {
		return p, err
	}
	generator, recorder := s.Generator.measured()
	outline, usage, modelErr := generator.Outline(ctx, modelSpec)
	p.Revision++
	if modelErr == nil {
		p.OutlineRevision = 1
		p.State = OutlineReady
		p.Goal, p.Outcomes, p.Modules = outline.Goal, outline.Outcomes, outline.Modules
		p.FinalAttestation, p.Sources = outline.FinalAttestation, outline.Sources
		p.HoursRebalanced = outline.HoursRebalanced
		p.Questions = nil
		p.Issues = p.ValidateOutline()
	}
	p.recordGeneration("outline", time.Since(started), usage, modelErr == nil && !hasErrors(p.Issues), recorder.Calls...)
	if err := s.persistGeneration(ctx, revision, p); err != nil {
		return Program{}, err
	}
	return p, modelErr
}
