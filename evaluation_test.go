package hours

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGenerationTraceRecordsConfigurationWithoutCredentials(t *testing.T) {
	model, err := NewCloudModelWithConfig("never-record-credential", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	model.Client = modelClient(`{"choices":[{"message":{"content":"{\"spec\":{\"title\":\"SQL\"}}"}}],"usage":{"prompt_tokens":20,"completion_tokens":5}}`)
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}
	p, err := svc.Intake(context.Background(), "", 0, "Курс SQL", ProgramSpec{})
	if err != nil {
		t.Fatal(err)
	}
	call := p.GenerationMetrics[0].Calls[0]
	if call.Mode != "real" || call.ModelID != GigaSchoolModelID || call.Endpoint != GigaSchoolBaseURL || len(call.InputHash) != 64 || len(call.OutputHash) != 64 || len(call.InstructionHash) != 64 || len(call.SchemaHash) != 64 || call.Usage != p.Usage {
		t.Fatalf("incomplete trace: %+v", call)
	}
	data, _ := json.Marshal(p.GenerationMetrics)
	if strings.Contains(string(data), model.Key) || strings.Contains(string(data), "Курс SQL") {
		t.Fatal("trace leaked a credential or raw input")
	}
	model.Client = &http.Client{Transport: modelRoundTrip(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
	})}
	failed, err := svc.Intake(context.Background(), p.ID, p.Revision, "Повторить", ProgramSpec{})
	if err == nil || failed.Goal != "" || failed.State != Draft || failed.GenerationMetrics[1].Calls[0].Succeeded || failed.GenerationMetrics[1].Calls[0].OutputHash != "" {
		t.Fatal("provider failure was hidden or replaced by demo")
	}
	format1, _ := gatewayResponseFormat(&ParsedSpec{})
	data1, _ := json.Marshal(format1)
	for i := 0; i < 10; i++ {
		format2, _ := gatewayResponseFormat(&ParsedSpec{})
		data2, _ := json.Marshal(format2)
		if string(data1) != string(data2) {
			t.Fatal("schema digest is not stable")
		}
	}
}

func TestEvaluationSeparatesModesAndCountsIndependentRuns(t *testing.T) {
	_, first := readyProgram(t)
	second := first
	second.ID = newID()
	second.GenerationMetrics = append([]GenerationMetric(nil), first.GenerationMetrics...)
	second.GenerationMetrics[0].DurationMS = 100
	third := first
	third.ID = newID()
	third.GenerationMetrics = nil
	result, err := EvaluatePrograms([]Program{first, second, third})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repeats) != 1 || result.Repeats[0].Runs != 2 || result.Repeats[0].Mode != "demo" || result.Repeats[0].StructuralPassRate != 1 || result.Runs[2].Mode != "unknown" || result.Runs[0].CoverageFraction != 1 {
		t.Fatalf("wrong evaluation: %+v", result)
	}
	if _, err := EvaluatePrograms([]Program{first, first}); err == nil {
		t.Fatal("same program counted as independent repeats")
	}
	metrics := append([]GenerationMetric(nil), first.GenerationMetrics...)
	metrics = append(metrics, GenerationMetric{Stage: "materials"})
	if generationMode(metrics) != "mixed" {
		t.Fatal("untraced history was reported as fully observed")
	}
	first.GenerationMetrics = []GenerationMetric{{Stage: "outline", DurationMS: 1, Succeeded: false}}
	failed, err := EvaluatePrograms([]Program{first})
	if err != nil {
		t.Fatal(err)
	}
	if *failed.Runs[0].OutlineWithinTarget {
		t.Fatal("failed attempt counted as meeting performance target")
	}
}

func TestTemporaryProviderFailureHasOneTracedRetry(t *testing.T) {
	for _, eventualSuccess := range []bool{false, true} {
		model, _ := NewCloudModelWithConfig("test-key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 1024})
		calls := 0
		model.Client = &http.Client{Transport: modelRoundTrip(func(_ *http.Request) (*http.Response, error) {
			calls++
			if eventualSuccess && calls == 2 {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"spec\":{\"title\":\"SQL\"}}"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`))}, nil
			}
			return &http.Response{StatusCode: 502, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
		})}
		svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}
		p, err := svc.Intake(context.Background(), "", 0, "Курс SQL", ProgramSpec{})
		if calls != 2 || (err == nil) != eventualSuccess || len(p.GenerationMetrics[0].Calls) != 2 || !p.GenerationMetrics[0].Calls[0].UsageUnavailable {
			t.Fatalf("retry state=%+v err=%v calls=%d", p.GenerationMetrics, err, calls)
		}
		if eventualSuccess && p.Usage != (Usage{PromptTokens: 5, CompletionTokens: 2}) {
			t.Fatal("retry lost known usage")
		}
		if !eventualSuccess && agentErrorStatus(err) != 503 {
			t.Fatal("provider outage reported as application error")
		}
	}
}

func TestProviderRetryDoesNotRepeatInvalidOrUncertainReplies(t *testing.T) {
	for _, tc := range []struct {
		status           int
		retryAfter, body string
	}{{400, "", "invalid"}, {503, "120", "unavailable"}, {200, "", `{"choices":[{"message":{"content":"invalid"}}],"usage":{"prompt_tokens":5,"completion_tokens":2}}`}} {
		model, _ := NewCloudModelWithConfig("test-key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 1024})
		calls := 0
		model.Client = &http.Client{Transport: modelRoundTrip(func(_ *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": []string{tc.retryAfter}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: model}}
		_, err := svc.Intake(context.Background(), "", 0, "Курс SQL", ProgramSpec{})
		if err == nil || calls != 1 {
			t.Fatalf("uncertain or invalid request repeated: status=%d calls=%d err=%v", tc.status, calls, err)
		}
	}
}
