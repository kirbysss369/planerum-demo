package hours

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const CloudBaseURL = "https://foundation-models.api.cloud.ru/v1"

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (u *Usage) Add(v Usage) {
	u.PromptTokens += v.PromptTokens
	u.CompletionTokens += v.CompletionTokens
}

// Model is the only network-dependent boundary in the generation pipeline.
type Model interface {
	JSON(ctx context.Context, instruction string, input any, output any) (Usage, error)
}

type CloudModel struct {
	Key     string
	ModelID string
	Allowed map[string]bool
	Client  *http.Client
	// BaseURL is intentionally unexported; production calls only Cloud.ru.
	baseURL string
}

func NewCloudModel(key, modelID string, allowed []string) (*CloudModel, error) {
	if key == "" || modelID == "" {
		return nil, errors.New("Cloud.ru key and model ID are required")
	}
	m := &CloudModel{Key: key, ModelID: modelID, Allowed: map[string]bool{}, Client: &http.Client{Timeout: 2 * time.Minute}, baseURL: CloudBaseURL}
	for _, id := range allowed {
		m.Allowed[strings.TrimSpace(id)] = true
	}
	if !m.Allowed[modelID] {
		return nil, errors.New("model ID is not in the verified internal allowlist")
	}
	low := strings.ToLower(modelID)
	if !strings.Contains(low, "qwen") && !strings.Contains(low, "deepseek") {
		return nil, errors.New("only verified DeepSeek or Qwen models are allowed")
	}
	return m, nil
}

func (m *CloudModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	var usage Usage
	if !m.Allowed[m.ModelID] || m.Key == "" {
		return usage, errors.New("model is not authorized")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return usage, err
	}
	requestBody := map[string]any{
		"model":                 m.ModelID,
		"messages":              []map[string]string{{"role": "system", "content": instruction + "\nОтветьте только одним объектом JSON без Markdown."}, {"role": "user", "content": string(data)}},
		"temperature":           0.2,
		"max_completion_tokens": 8192,
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return usage, err
	}
	base := m.baseURL
	if base == "" {
		base = CloudBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return usage, err
	}
	req.Header.Set("Authorization", "Bearer "+m.Key)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return usage, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return usage, fmt.Errorf("Cloud.ru returned status %d", resp.StatusCode)
	}
	var wire struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&wire); err != nil {
		return usage, err
	}
	if len(wire.Choices) == 0 {
		return wire.Usage, errors.New("model returned no choices")
	}
	if err := json.Unmarshal([]byte(wire.Choices[0].Message.Content), output); err != nil {
		return wire.Usage, fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return wire.Usage, nil
}

type GeneratedOutline struct {
	Goal             string    `json:"goal"`
	Outcomes         []Outcome `json:"outcomes"`
	Modules          []Module  `json:"modules"`
	FinalAttestation Hours     `json:"final_attestation_hours"`
	Sources          []Source  `json:"sources"`
}

type GeneratedMaterials struct {
	Assessments []Assessment `json:"assessments"`
	Lectures    []Lecture    `json:"lectures"`
	Sources     []Source     `json:"sources"`
}

type AssessmentInput struct {
	Spec    ProgramSpec `json:"spec"`
	Goal    string      `json:"goal"`
	Outcome Outcome     `json:"outcome"`
	Topics  []Topic     `json:"topics"`
}

type LectureInput struct {
	Spec     ProgramSpec `json:"spec"`
	Goal     string      `json:"goal"`
	Topic    Topic       `json:"topic"`
	Outcomes []Outcome   `json:"outcomes"`
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
	instruction := `Вы методист. Подготовьте каркас образовательной программы на русском языке. Верните поля goal, outcomes, modules, final_attestation_hours, sources. Для каждого результата outcomes заполните text, knowledge, skills, experience и topic_ids: карта должна связывать знания, умения и практические навыки с темами. У каждого модуля есть topics, у темы activities с kind: lecture, practice, laboratory или self_study. Дайте постоянные уникальные ID. Сумма часов должна быть точной. Не выдумывайте нормативные источники и коды: если нет подтверждения, помечайте source status unverified и оставляйте код пустым. Реквизиты приказа, ФИО и подписи не заполняйте.`
	usage, err := g.Model.JSON(ctx, instruction, spec, &out)
	for i := range out.Sources {
		out.Sources[i].Status = "unverified"
		out.Sources[i].Evidence = ""
	}
	return out, usage, err
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
		for _, id := range outcome.TopicIDs {
			if topic, ok := topics[id]; ok {
				input.Topics = append(input.Topics, topic)
			}
		}
		var batch GeneratedAssessmentBatch
		instruction := `Вы методист. Для одного утвержденного результата обучения подготовьте русскоязычные оценочные средства. Верните assessments: минимум один тестовый вопрос kind=test с answer и одно практическое/семинарское задание kind=practice с rubric. Укажите outcome_ids только этого результата. Верните sources только для реально использованных материалов. Не выдумывайте подтверждение источников: status unverified. Не меняйте темы, часы, результаты и реквизиты.`
		usage, err := g.Model.JSON(ctx, instruction, input, &batch)
		total.Add(usage)
		if err != nil {
			return out, total, err
		}
		for index := range batch.Assessments {
			batch.Assessments[index].ID = fmt.Sprintf("assessment-%d-%d", outcomeIndex+1, index+1)
			batch.Assessments[index].OutcomeIDs = []string{outcome.ID}
		}
		out.Assessments = append(out.Assessments, batch.Assessments...)
		out.Sources = append(out.Sources, unverifiedSources(batch.Sources)...)
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
						break
					}
				}
			}
			var batch GeneratedLectureBatch
			instruction := `Вы методист. Для одной утвержденной темы с лекционными часами подготовьте один план лекции и тексты слайдов на русском языке. Верните lectures: один объект с plan, topic_id и массивом slides; верните sources только для реально использованных материалов. Не выдумывайте подтверждение источников: status unverified. Не меняйте тему, часы, результаты и реквизиты.`
			usage, err := g.Model.JSON(ctx, instruction, input, &batch)
			total.Add(usage)
			if err != nil {
				return out, total, err
			}
			if len(batch.Lectures) > 0 {
				lecture := batch.Lectures[0]
				lecture.ID = "lecture-" + topic.ID
				lecture.TopicID = topic.ID
				out.Lectures = append(out.Lectures, lecture)
			}
			out.Sources = append(out.Sources, unverifiedSources(batch.Sources)...)
		}
	}
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

func unverifiedSources(sources []Source) []Source {
	for i := range sources {
		sources[i].Status = "unverified"
		sources[i].Evidence = ""
	}
	return sources
}
