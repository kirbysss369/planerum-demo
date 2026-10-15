package hours

import (
	"context"
	"errors"
)

type DemoModel struct{}

func (DemoModel) JSON(_ context.Context, _ string, input any, output any) (Usage, error) {
	switch out := output.(type) {
	case *GeneratedOutline:
		spec, ok := input.(ProgramSpec)
		if !ok {
			return Usage{}, errors.New("demo outline needs a spec")
		}
		final := Hours(2)
		if spec.Kind == General {
			final = 6
		}
		if spec.Requested <= final {
			final = 0
		}
		activities := []Activity{}
		if spec.IndependentInternship {
			activities = append(activities, Activity{ID: "act-self", Name: "Самостоятельная работа (демо)", Kind: "self_study", Hours: spec.Requested - final})
		} else if spec.Requested == 24 && spec.Kind == General {
			activities = []Activity{{"act-lecture", "Теория (демо)", "lecture", 4, ""}, {"act-practice", "Практика (демо)", "practice", 4, ""}, {"act-self", "Самостоятельная работа (демо)", "self_study", 10, ""}}
		} else {
			work := spec.Requested - final
			lecture := Hours(int(work / 4))
			practice := Hours(int(work / 2))
			activities = []Activity{{"act-lecture", "Теория (демо)", "lecture", lecture, ""}, {"act-practice", "Практика (демо)", "practice", practice, ""}, {"act-self", "Самостоятельная работа (демо)", "self_study", work - lecture - practice, ""}}
		}
		*out = GeneratedOutline{
			Goal:             "ДЕМОНСТРАЦИОННЫЙ каркас для проверки технического процесса; содержание требует модели Cloud.ru.",
			Outcomes:         []Outcome{{ID: "outcome-1", Text: "Демонстрационный результат обучения", Knowledge: []string{"Демонстрационное знание"}, Skills: []string{"Демонстрационное умение"}, Experience: []string{"Демонстрационный навык"}, TopicIDs: []string{"topic-1"}}},
			Modules:          []Module{{ID: "module-1", Title: "Демонстрационный модуль", Topics: []Topic{{ID: "topic-1", Title: "Демонстрационная тема", Summary: "Данные для проверки процесса", Activities: activities}}}},
			FinalAttestation: final,
		}
	case *GeneratedMaterials:
		*out = GeneratedMaterials{
			Assessments: []Assessment{
				{ID: "test-1", Kind: "test", Question: "Демонстрационный вопрос 1?", Answer: "Ответ 1", OutcomeIDs: []string{"outcome-1"}},
				{ID: "task-1", Kind: "practice", Question: "Демонстрационное практическое задание", Rubric: "Результат представлен", OutcomeIDs: []string{"outcome-1"}},
			},
		}
		if p, ok := input.(Program); ok && !p.Spec.IndependentInternship {
			out.Lectures = []Lecture{{ID: "lecture-1", TopicID: "topic-1", Plan: "Демонстрационный план", Slides: []string{"Демонстрационный слайд"}}}
		}
	case *GeneratedAssessmentBatch:
		item, ok := input.(AssessmentInput)
		if !ok {
			return Usage{}, errors.New("demo assessment needs an outcome")
		}
		*out = GeneratedAssessmentBatch{Assessments: []Assessment{
			{ID: "test", Kind: "test", Question: "Демонстрационный вопрос?", Answer: "Демонстрационный ответ", OutcomeIDs: []string{item.Outcome.ID}},
			{ID: "practice", Kind: "practice", Question: "Демонстрационное практическое задание", Rubric: "Результат представлен", OutcomeIDs: []string{item.Outcome.ID}},
		}}
	case *GeneratedLectureBatch:
		item, ok := input.(LectureInput)
		if !ok {
			return Usage{}, errors.New("demo lecture needs a topic")
		}
		*out = GeneratedLectureBatch{Lectures: []Lecture{{ID: "lecture", TopicID: item.Topic.ID, Plan: "Демонстрационный план", Slides: []string{"Демонстрационный слайд"}}}}
	default:
		return Usage{}, errors.New("unsupported demo step")
	}
	return Usage{}, nil
}
