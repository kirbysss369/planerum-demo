package hours

import (
	"errors"
	"fmt"
	"strings"
)

type ProgramKind string

const (
	Qualification ProgramKind = "qualification"
	General       ProgramKind = "general"
)

type ProgramSpec struct {
	Title                 string      `json:"title"`
	Kind                  ProgramKind `json:"kind"`
	Audience              string      `json:"audience"`
	EntryLevel            string      `json:"entry_level"`
	Delivery              string      `json:"delivery"`
	Requested             Hours       `json:"requested"`
	IndependentInternship bool        `json:"independent_internship"`
	Attestation           string      `json:"attestation"`
	Requirements          string      `json:"requirements,omitempty"`
}

var ErrInvalidInput = errors.New("invalid program parameters")

func (s ProgramSpec) Validate() error {
	if strings.TrimSpace(s.Title) == "" {
		return fmt.Errorf("%w: program title is required", ErrInvalidInput)
	}
	if s.Kind != Qualification && s.Kind != General {
		return fmt.Errorf("%w: unknown program kind", ErrInvalidInput)
	}
	if s.Requested < 16 || s.Requested > 500 {
		return fmt.Errorf("%w: supported range is 16–500 hours", ErrInvalidInput)
	}
	if _, err := s.Requested.minutes(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if len(MissingQuestions(s)) != 0 {
		return fmt.Errorf("%w: required program parameters are missing", ErrInvalidInput)
	}
	return nil
}

func MissingQuestions(spec ProgramSpec) []string {
	var questions []string
	if strings.TrimSpace(spec.Title) == "" {
		question := "Как называется программа?"
		if spec.Kind != Qualification && spec.Kind != General {
			question = "Как называется программа и какой тип нужен: повышение квалификации или общеразвивающая?"
		}
		questions = append(questions, question)
	} else if spec.Kind != Qualification && spec.Kind != General {
		questions = append(questions, "Какой тип программы нужен: повышение квалификации или общеразвивающая?")
	}
	if spec.Requested <= 0 {
		questions = append(questions, "Какой объем программы в академических часах?")
	}
	if strings.TrimSpace(spec.Audience) == "" && strings.TrimSpace(spec.EntryLevel) == "" {
		questions = append(questions, "Кто слушатели и каков их начальный уровень?")
	} else if strings.TrimSpace(spec.Audience) == "" {
		questions = append(questions, "Кто будет слушателями программы?")
	} else if strings.TrimSpace(spec.EntryLevel) == "" {
		questions = append(questions, "Каков начальный уровень слушателей?")
	}
	if strings.TrimSpace(spec.Delivery) == "" {
		questions = append(questions, "Какая форма обучения и проведения занятий?")
	}
	if strings.TrimSpace(spec.Attestation) == "" {
		questions = append(questions, "Как пройдет итоговая аттестация?")
	}
	return questions
}
