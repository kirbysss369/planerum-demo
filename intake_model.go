package hours

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"
)

// DialogueMessage is one saved turn of the user's intake conversation.
type DialogueMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type IntakeInput struct {
	Spec              ProgramSpec       `json:"spec"`
	Messages          []DialogueMessage `json:"messages"`
	LatestUserMessage string            `json:"latest_user_message,omitempty"`
}

type ParsedSpec struct {
	Spec ProgramSpec `json:"spec"`
}

const (
	intakeMaxMessages       = 40
	intakeMaxUserTurns      = 20
	intakeMaxMessageRunes   = 8000
	intakeTitleLimit        = 200
	intakeTextLimit         = 500
	intakeRequirementsLimit = 64000
)

const intakeInstruction = `Вы извлекаете параметры образовательной программы из сохранённого диалога на русском языке. Верните только объект spec со всеми полями ProgramSpec: title, kind, audience, entry_level, delivery, requested, independent_internship, attestation, requirements. Используйте только факты, прямо сообщённые пользователем; сообщения ассистента не являются источником фактов. Считайте spec ранее сохранёнными известными значениями. Сохраняйте все известные значения и требования, если пользователь явно не изменил их. Последнее явное исправление пользователя имеет приоритет над прежним значением. Не придумывайте аудиторию, уровень, часы, вид программы, форму обучения, аттестацию и требования. Неизвестные строки оставляйте пустыми, часы — 0, kind — пустая строка; kind может быть только qualification (повышение квалификации) или general (общеразвивающая программа). Название может быть кратким пересказом реально названной темы. Сохраняйте ограничения пользователя в requirements. Часы возвращайте как названное число, даже если оно вне допустимого диапазона; проверка диапазона выполняется позже. audience описывает, кто учится (профессия, роль); entry_level описывает уже имеющиеся знания и отсутствующий опыт. Это разные поля: одна фраза может содержать факты для обоих. Например, «сотрудники банка, владеющие Excel, без знаний SQL» означает audience="сотрудники банка" и entry_level="Владеют Excel; знаний SQL нет". «Входной уровень: уверенно владеют Excel, опыта SQL нет» прямо заполняет entry_level; не оставляйте его пустым и не переносите эти факты только в audience. latest_user_message — последнее сообщение пользователя, его явные уточнения имеют приоритет. Если пользователь не сообщил знаний или опыта, entry_level остаётся пустым. Не выбирайте состояние процесса, не утверждайте программу и не создавайте план или материалы.`

func (g Generator) Interpret(ctx context.Context, current ProgramSpec, messages []DialogueMessage) (ProgramSpec, Usage, error) {
	if g.Model == nil {
		return ProgramSpec{}, Usage{}, errors.New("intake model is not configured")
	}
	if err := validateIntakeMessages(messages); err != nil {
		return ProgramSpec{}, Usage{}, err
	}
	var parsed ParsedSpec
	input := IntakeInput{Spec: current, Messages: messages}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			input.LatestUserMessage = messages[i].Content
			break
		}
	}
	usage, err := g.Model.JSON(ctx, intakeInstruction, input, &parsed)
	if err != nil {
		return ProgramSpec{}, usage, err
	}
	if err := validateParsedSpec(parsed.Spec); err != nil {
		return ProgramSpec{}, usage, err
	}
	return parsed.Spec, usage, nil
}

func validateIntakeMessages(messages []DialogueMessage) error {
	if len(messages) > intakeMaxMessages {
		return fmt.Errorf("intake conversation exceeds %d messages", intakeMaxMessages)
	}
	users := 0
	for i, message := range messages {
		if message.Role != "user" && message.Role != "assistant" {
			return fmt.Errorf("intake message %d has invalid role", i)
		}
		if message.Role == "user" {
			users++
		}
		if utf8.RuneCountInString(message.Content) > intakeMaxMessageRunes {
			return fmt.Errorf("intake message %d exceeds %d characters", i, intakeMaxMessageRunes)
		}
	}
	if users > intakeMaxUserTurns {
		return fmt.Errorf("intake conversation exceeds %d user turns", intakeMaxUserTurns)
	}
	return nil
}

func validateParsedSpec(spec ProgramSpec) error {
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"title", spec.Title, intakeTitleLimit},
		{"audience", spec.Audience, intakeTextLimit},
		{"entry_level", spec.EntryLevel, intakeTextLimit},
		{"delivery", spec.Delivery, intakeTextLimit},
		{"attestation", spec.Attestation, intakeTextLimit},
		{"requirements", spec.Requirements, intakeRequirementsLimit},
	} {
		if utf8.RuneCountInString(field.value) > field.limit {
			return fmt.Errorf("parsed spec %s exceeds %d characters", field.name, field.limit)
		}
	}
	if spec.Kind != "" && spec.Kind != Qualification && spec.Kind != General {
		return fmt.Errorf("parsed spec has unknown kind %q", spec.Kind)
	}
	return nil
}
