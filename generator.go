package hours

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type GeneratedOutline struct {
	Goal             string    `json:"goal"`
	Outcomes         []Outcome `json:"outcomes"`
	Modules          []Module  `json:"modules"`
	FinalAttestation Hours     `json:"final_attestation_hours"`
	Sources          []Source  `json:"sources"`
	HoursRebalanced  bool      `json:"-"`
}

type GeneratedMaterials struct {
	Assessments []Assessment `json:"assessments"`
	Lectures    []Lecture    `json:"lectures"`
	Sources     []Source     `json:"sources"`
}

type AssessmentInput struct {
	Spec           ProgramSpec `json:"spec"`
	Goal           string      `json:"goal"`
	Outcome        Outcome     `json:"outcome"`
	Topics         []Topic     `json:"topics"`
	StandardSource *Source     `json:"standard_source,omitempty"`
}

type LectureInput struct {
	Spec            ProgramSpec `json:"spec"`
	Goal            string      `json:"goal"`
	Topic           Topic       `json:"topic"`
	Outcomes        []Outcome   `json:"outcomes"`
	StandardSources []Source    `json:"standard_sources,omitempty"`
}

type GeneratedAssessmentBatch struct {
	Assessments []Assessment `json:"assessments"`
	Sources     []Source     `json:"sources"`
}

type GeneratedLectureBatch struct {
	Lectures []Lecture `json:"lectures"`
	Sources  []Source  `json:"sources"`
}

type Generator struct{ Model Model }

func (g Generator) Outline(ctx context.Context, spec ProgramSpec) (GeneratedOutline, Usage, error) {
	var out GeneratedOutline
	if g.Model == nil {
		return out, Usage{}, errors.New("generation model is not configured")
	}
	instruction := `Вы методист. Подготовьте каркас образовательной программы на русском языке. Верните поля goal, outcomes, modules, final_attestation_hours, sources. Для каждого результата outcomes заполните уникальный id, text, knowledge, skills, experience и topic_ids. knowledge, skills и experience — массивы строк, topic_ids — непустой массив ID существующих тем: каждый результат связан хотя бы с одной темой. Итоговая аттестация не заменяет связь результата с темой. Создайте не менее трёх результатов обучения. Массив modules содержит только модули с непустыми темами, а outcomes — только результаты обучения; не копируйте результаты в modules. У каждого модуля есть уникальный id, title и topics; у каждой темы — уникальный id, title и activities; у каждого вида работы — уникальный id, name, kind и hours. kind: lecture, practice, laboratory или self_study. Не добавляйте отдельные часы модуля и темы: часы задаются только в activities. Сумма всех activities.hours и final_attestation_hours должна быть ровно равна запрошенному объёму. Не выдумывайте нормативные источники и коды: без подтверждения верните sources как пустой массив. Реквизиты приказа, ФИО и подписи не заполняйте.`
	instruction += outlineHourPlan(spec.Requested) + generatedSourceInstruction
	var usage Usage
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		out = GeneratedOutline{}
		var used Usage
		used, err = g.Model.JSON(ctx, instruction, spec, &out)
		usage.Add(used)
		if err != nil {
			break
		}
		out.HoursRebalanced = rebalanceOutlineHours(&out, spec.Requested)
		rawSources := out.Sources
		out.Sources = groundedSources(rawSources, spec)
		for i := range out.Outcomes {
			for _, raw := range rawSources {
				if raw.ID != "" && out.Outcomes[i].SourceID == raw.ID {
					for _, source := range out.Sources {
						if source.Location == strings.TrimSpace(raw.Location) {
							out.Outcomes[i].SourceID = source.ID
							break
						}
					}
				}
			}
		}
		p := Program{Spec: spec, Goal: out.Goal, Outcomes: out.Outcomes, Modules: out.Modules, FinalAttestation: out.FinalAttestation}
		var failures []Issue
		for _, issue := range p.ValidateOutline() {
			if issue.Severity == "error" {
				failures = append(failures, issue)
			}
		}
		if len(failures) == 0 || attempt == 1 {
			break
		}
		feedback, _ := json.Marshal(struct {
			Outline GeneratedOutline `json:"previous_outline"`
			Issues  []Issue          `json:"validation_issues"`
		}{out, failures})
		instruction += " Предыдущий каркас не прошёл структурную проверку. Верните исправленный полный каркас по исходному запросу, устранив указанные ошибки. Данные проверки:\n" + string(feedback)
	}

	return out, usage, err
}

// plannedOutlineHours is shared by the model instruction and arithmetic checks.
func plannedOutlineHours(requested Hours) (Hours, []Hours) {
	hours := int(requested)
	if Hours(hours) != requested || hours < 16 {
		return 0, nil
	}
	attestation := 4
	if hours < 32 {
		attestation = 2
	}
	moduleCount := (hours + 9) / 18
	if moduleCount < 2 {
		moduleCount = 2
	}
	if moduleCount > 8 {
		moduleCount = 8
	}
	remaining := hours - attestation
	var topics []Hours
	for i := 0; i < moduleCount; i++ {
		moduleHours := remaining / moduleCount
		if i < remaining%moduleCount {
			moduleHours++
		}
		topics = append(topics, Hours(moduleHours/2), Hours(moduleHours-moduleHours/2))
	}
	return Hours(attestation), topics
}

func outlineHourPlan(requested Hours) string {
	final, budgets := plannedOutlineHours(requested)
	if len(budgets) == 0 {
		return ""
	}
	var modules, topics []string
	for i := 0; i < len(budgets); i += 2 {
		modules = append(modules, fmt.Sprintf("m%d=%v", i/2+1, budgets[i]+budgets[i+1]))
		for j := i; j < i+2; j++ {
			topics = append(topics, fmt.Sprintf("t%d=%v", j+1, budgets[j]))
		}
	}
	return fmt.Sprintf(" Обязательный план часов: final_attestation_hours=%v; ровно %d модулей (%s); в каждом модуле ровно две темы (%s). Часы темы — сумма её activities.hours. Для темы из 3 часов допустимо, например, lecture=1 и practice=2, но не 2+2. Для темы из 4 часов допустимо 2+2. Не переносите часы между темами или модулями. Проверьте каждую сумму перед ответом.", final, len(budgets)/2, strings.Join(modules, ", "), strings.Join(topics, ", "))
}

func (g Generator) Materials(ctx context.Context, p Program) (GeneratedMaterials, Usage, error) {
	var out GeneratedMaterials
	if g.Model == nil {
		return out, Usage{}, errors.New("generation model is not configured")
	}
	var total Usage
	topics := map[string]Topic{}
	for _, module := range p.Modules {
		for _, topic := range module.Topics {
			topics[topic.ID] = topic
		}
	}
	for outcomeIndex, outcome := range p.Outcomes {
		input := AssessmentInput{Spec: p.Spec, Goal: p.Goal, Outcome: outcome}
		for _, source := range p.Sources {
			if source.ID == outcome.SourceID && source.Status == "verified" {
				copy := source
				input.StandardSource = &copy
				break
			}
		}
		for _, id := range outcome.TopicIDs {
			if topic, ok := topics[id]; ok {
				input.Topics = append(input.Topics, topic)
			}
		}
		var batch GeneratedAssessmentBatch
		instruction := `Вы методист. Для одного утвержденного результата обучения подготовьте ровно два русскоязычных оценочных средства: один тестовый вопрос kind=test с answer и одно практическое/семинарское задание kind=practice с rubric. Вопрос — до 350 символов, ответ и критерии — каждый до 500 символов. Не повторяйте формулировки и не включайте полный конспект курса. Укажите outcome_ids только этого результата. Верните sources только для реально использованных материалов. Не выдумывайте подтверждение источников: status unverified. Не меняйте темы, часы, результаты и реквизиты.`
		usage, err := g.Model.JSON(ctx, instruction+generatedSourceInstruction+" Если передан проверенный фрагмент профстандарта, опирайтесь на него при выборе содержания; не добавляйте неподтверждённых кодов.", input, &batch)
		total.Add(usage)
		if err != nil {
			return out, total, fmt.Errorf("assessments for outcome %s: %w", outcome.ID, err)
		}
		hasTest, hasPractice := false, false
		for _, assessment := range batch.Assessments {
			if strings.TrimSpace(assessment.Question) == "" {
				continue
			}
			hasTest = hasTest || assessment.Kind == "test" && strings.TrimSpace(assessment.Answer) != ""
			hasPractice = hasPractice || assessment.Kind == "practice" && strings.TrimSpace(assessment.Rubric) != ""
		}
		if !hasTest || !hasPractice {
			return out, total, fmt.Errorf("model returned incomplete assessments for outcome %s", outcome.ID)
		}
		for index := range batch.Assessments {
			batch.Assessments[index].ID = fmt.Sprintf("assessment-%d-%d", outcomeIndex+1, index+1)
			batch.Assessments[index].OutcomeIDs = []string{outcome.ID}
		}
		out.Assessments = append(out.Assessments, batch.Assessments...)
		out.Sources = append(out.Sources, groundedSources(batch.Sources, p.Spec)...)
	}
	for _, module := range p.Modules {
		for _, topic := range module.Topics {
			if !hasLectureHours(topic) {
				continue
			}
			input := LectureInput{Spec: p.Spec, Goal: p.Goal, Topic: topic}
			for _, outcome := range p.Outcomes {
				for _, id := range outcome.TopicIDs {
					if id == topic.ID {
						input.Outcomes = append(input.Outcomes, outcome)
						for _, source := range p.Sources {
							if source.ID == outcome.SourceID && source.Status == "verified" {
								input.StandardSources = mergeSources(input.StandardSources, []Source{source})
							}
						}
						break
					}
				}
			}
			var batch GeneratedLectureBatch
			instruction := `Вы методист. Для одной утвержденной темы с лекционными часами подготовьте один план лекции и тексты слайдов на русском языке. Верните lectures: ровно один объект с непустым plan до 900 символов, topic_id и массивом slides из трёх-пяти непустых строк до 350 символов каждая. План перечисляет последовательность объяснения; каждый слайд содержит конкретные тезисы или короткий пример для этой темы. Не включайте стенограмму лекции и не повторяйте весь курс. Верните sources только для реально использованных материалов. Не выдумывайте подтверждение источников: status unverified. Не меняйте тему, часы, результаты и реквизиты.`
			usage, err := g.Model.JSON(ctx, instruction+generatedSourceInstruction+" Если передан проверенный фрагмент профстандарта, опирайтесь на него при выборе содержания; не добавляйте неподтверждённых кодов.", input, &batch)
			total.Add(usage)
			if err != nil {
				return out, total, fmt.Errorf("lecture for topic %s: %w", topic.ID, err)
			}
			if len(batch.Lectures) != 1 || strings.TrimSpace(batch.Lectures[0].Plan) == "" || !nonemptyTexts(batch.Lectures[0].Slides) {
				return out, total, fmt.Errorf("model returned incomplete lecture for topic %s", topic.ID)
			}
			lecture := batch.Lectures[0]
			lecture.ID = "lecture-" + topic.ID
			lecture.TopicID = topic.ID
			out.Lectures = append(out.Lectures, lecture)
			out.Sources = append(out.Sources, groundedSources(batch.Sources, p.Spec)...)
		}
	}
	out.Sources = mergeSources(nil, out.Sources)
	return out, total, nil
}

func hasLectureHours(topic Topic) bool {
	for _, activity := range topic.Activities {
		if activity.Kind == "lecture" && activity.Hours > 0 {
			return true
		}
	}
	return false
}
