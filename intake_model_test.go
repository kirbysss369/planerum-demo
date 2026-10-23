package hours

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type intakeRoundTrip func(*http.Request) (*http.Response, error)

func (f intakeRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type scriptedIntakeModel struct {
	call func(string, any, any) (Usage, error)
}

func (m scriptedIntakeModel) JSON(_ context.Context, instruction string, input any, output any) (Usage, error) {
	return m.call(instruction, input, output)
}

func TestInterpretUsesSavedContextAndParsedSpecOnly(t *testing.T) {
	current := ProgramSpec{Title: "Сварка", Audience: "мастера", Requirements: "без станков"}
	messages := []DialogueMessage{{Role: "user", Content: "Нужна программа по сварке"}, {Role: "assistant", Content: "Сколько часов?"}, {Role: "user", Content: "72 часа"}}
	want := current
	want.Requested = 72
	model := scriptedIntakeModel{call: func(instruction string, input any, output any) (Usage, error) {
		if !strings.Contains(instruction, "Не придумывайте") || !strings.Contains(instruction, "Последнее явное исправление") || !strings.Contains(instruction, "Сохраняйте все известные значения") {
			t.Errorf("intake grounding instructions missing: %q", instruction)
		}
		got, ok := input.(IntakeInput)
		if !ok || !reflect.DeepEqual(got.Spec, current) || !reflect.DeepEqual(got.Messages, messages) || got.LatestUserMessage != "72 часа" {
			t.Errorf("wrong intake context: %#v", input)
		}
		parsed, ok := output.(*ParsedSpec)
		if !ok {
			t.Fatalf("wrong output type: %T", output)
		}
		parsed.Spec = want
		return Usage{PromptTokens: 9, CompletionTokens: 3}, nil
	}}
	got, usage, err := (Generator{Model: model}).Interpret(context.Background(), current, messages)
	if err != nil || !reflect.DeepEqual(got, want) || usage.PromptTokens != 9 || usage.CompletionTokens != 3 {
		t.Fatalf("spec=%+v usage=%+v err=%v", got, usage, err)
	}
}

func TestInterpretKeepsUsageOnModelAndValidationErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		spec     ProgramSpec
		modelErr error
	}{
		{"model error", ProgramSpec{}, errors.New("provider failed")},
		{"long title", ProgramSpec{Title: strings.Repeat("Я", intakeTitleLimit+1)}, nil},
		{"unknown kind", ProgramSpec{Kind: "invented"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := scriptedIntakeModel{call: func(_ string, _ any, output any) (Usage, error) {
				output.(*ParsedSpec).Spec = tc.spec
				return Usage{PromptTokens: 7, CompletionTokens: 2}, tc.modelErr
			}}
			_, usage, err := (Generator{Model: model}).Interpret(context.Background(), ProgramSpec{}, nil)
			if err == nil || usage != (Usage{PromptTokens: 7, CompletionTokens: 2}) {
				t.Fatalf("usage=%+v err=%v", usage, err)
			}
		})
	}
}

func TestInterpretAllowsUnknownsAndOutOfRangeHoursForService(t *testing.T) {
	model := scriptedIntakeModel{call: func(_ string, _ any, output any) (Usage, error) {
		output.(*ParsedSpec).Spec = ProgramSpec{Title: "Тема", Requested: 700}
		return Usage{}, nil
	}}
	spec, _, err := (Generator{Model: model}).Interpret(context.Background(), ProgramSpec{}, nil)
	if err != nil || spec.Requested != 700 || spec.Kind != "" || spec.Audience != "" {
		t.Fatalf("spec=%+v err=%v", spec, err)
	}
}

func TestIntakeGatewaySchemaRequiresAllSpecFields(t *testing.T) {
	format, ok := gatewayResponseFormat(&ParsedSpec{})
	if !ok || format["type"] != "json_schema" {
		t.Fatal("parsed spec has no strict gateway schema")
	}
	wrapper := format["json_schema"].(map[string]any)
	if wrapper["strict"] != true {
		t.Fatal("parsed spec schema is not strict")
	}
	root := wrapper["schema"].(map[string]any)
	if root["additionalProperties"] != false || !reflect.DeepEqual(root["required"], []string{"spec"}) {
		t.Fatalf("wrong root schema: %#v", root)
	}
	spec := root["properties"].(map[string]any)["spec"].(map[string]any)
	fields := spec["properties"].(map[string]any)
	if spec["additionalProperties"] != false || len(fields) != 9 || len(spec["required"].([]string)) != len(fields) {
		t.Fatalf("incomplete spec schema: %#v", spec)
	}
	for _, name := range []string{"title", "kind", "audience", "entry_level", "delivery", "requested", "independent_internship", "attestation", "requirements"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if got := fields["kind"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"", "qualification", "general"}) {
		t.Fatalf("unknown kind missing from enum: %#v", got)
	}
	if _, bounded := fields["requested"].(map[string]any)["maximum"]; bounded {
		t.Fatal("service hour limit leaked into intake schema")
	}
}

func TestIntakeGatewaySendsStrictSchemaOnWire(t *testing.T) {
	model, err := NewCloudModelWithConfig("test-key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	model.Client = &http.Client{Transport: intakeRoundTrip(func(request *http.Request) (*http.Response, error) {
		var body struct {
			ResponseFormat map[string]any `json:"response_format"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		wrapper := body.ResponseFormat["json_schema"].(map[string]any)
		if body.ResponseFormat["type"] != "json_schema" || wrapper["strict"] != true || wrapper["name"] != "parsed_program_spec" {
			t.Fatalf("wrong wire format: %#v", body.ResponseFormat)
		}
		root := wrapper["schema"].(map[string]any)
		spec := root["properties"].(map[string]any)["spec"].(map[string]any)
		if len(spec["required"].([]any)) != 9 || spec["additionalProperties"] != false {
			t.Fatalf("wrong wire spec schema: %#v", spec)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"spec\":{\"title\":\"\",\"kind\":\"\",\"audience\":\"\",\"entry_level\":\"\",\"delivery\":\"\",\"requested\":0,\"independent_internship\":false,\"attestation\":\"\",\"requirements\":\"\"}}"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)), Request: request}, nil
	})}
	_, usage, err := (Generator{Model: model}).Interpret(context.Background(), ProgramSpec{}, nil)
	if err != nil || usage.PromptTokens != 2 || usage.CompletionTokens != 1 {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
}

func TestDemoIntakeTwoTurnsPreservesAndCorrects(t *testing.T) {
	model := Generator{Model: DemoModel{}}
	first := []DialogueMessage{{Role: "user", Content: "Нужна программа повышения квалификации по сварке для мастеров, 72 часа, очно, уровень: с нуля, аттестация: экзамен, без станков."}}
	spec, _, err := model.Interpret(context.Background(), ProgramSpec{}, first)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Title != "сварке" || spec.Kind != Qualification || spec.Audience != "мастеров" || spec.EntryLevel != "с нуля" || spec.Requested != 72 || spec.Delivery != "очно" || spec.Attestation != "экзамен" || !strings.Contains(spec.Requirements, "без станков") {
		t.Fatalf("first turn: %+v", spec)
	}
	second := append(append([]DialogueMessage(nil), first...), DialogueMessage{Role: "assistant", Content: "Принято"}, DialogueMessage{Role: "user", Content: "Поправка: 36 часов, дистанционно. Экзамен оставляем."})
	updated, _, err := model.Interpret(context.Background(), spec, second)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Requested != 36 || updated.Delivery != "дистанционно" || updated.Kind != Qualification || updated.Audience != "мастеров" || updated.EntryLevel != "с нуля" || updated.Attestation != "экзамен" || !strings.Contains(updated.Requirements, "без станков") {
		t.Fatalf("second turn: %+v", updated)
	}
}

func TestDemoIntakeLeavesUnknownsAndKeepsTopicWords(t *testing.T) {
	messages := []DialogueMessage{{Role: "user", Content: "Нужна общеразвивающая программа по работе с данными на станках, заочная форма."}}
	spec, _, err := (Generator{Model: DemoModel{}}).Interpret(context.Background(), ProgramSpec{}, messages)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Title != "работе с данными на станках" || spec.Kind != General || spec.Delivery != "заочно" || spec.Audience != "" || spec.EntryLevel != "" || spec.Requested != 0 || spec.Attestation != "" {
		t.Fatalf("demo invented or lost facts: %+v", spec)
	}
}
