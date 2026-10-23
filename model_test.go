package hours

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type modelRoundTrip func(*http.Request) (*http.Response, error)

func (f modelRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func modelClient(body string) *http.Client {
	return &http.Client{Transport: modelRoundTrip(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}

func TestCloudAdapterWithLocalTransport(t *testing.T) {
	model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
	if err != nil {
		t.Fatal(err)
	}
	model.Client = &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != CloudBaseURL+"/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request: %s %s", r.URL, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["model"] != "Qwen/test-internal" {
			t.Errorf("model = %v", request["model"])
		}
		if request["max_completion_tokens"] != float64(DefaultCloudTokens) || request["max_tokens"] != nil {
			t.Errorf("direct token fields = %v", request)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))}, nil
	})}
	var result struct {
		OK bool `json:"ok"`
	}
	usage, err := model.JSON(context.Background(), "test instruction", map[string]any{"x": 1}, &result)
	if err != nil || !result.OK || usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
		t.Fatalf("response: %+v %+v %v", result, usage, err)
	}
	if _, err := NewCloudModel("key", "external/model", []string{"external/model"}); err == nil {
		t.Fatal("non-allowlisted provider accepted")
	}
}

func TestGigaSchoolGatewayRequest(t *testing.T) {
	model, err := NewCloudModelWithConfig("test-key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 2048})
	if err != nil {
		t.Fatal(err)
	}
	model.Client = &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != GigaSchoolBaseURL+"/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" || r.Method != http.MethodPost {
			t.Errorf("gateway request: %s %s %s", r.Method, r.URL, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["model"] != GigaSchoolModelID || request["max_tokens"] != float64(2048) || request["max_completion_tokens"] != nil {
			t.Errorf("gateway fields = %v", request)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`))}, nil
	})}
	var result struct {
		OK bool `json:"ok"`
	}
	if _, err := model.JSON(context.Background(), "instruction", nil, &result); err != nil || !result.OK {
		t.Fatalf("gateway result: %+v, %v", result, err)
	}
}

func TestGigaSchoolOutlineUsesStructuredResponse(t *testing.T) {
	model, err := NewCloudModelWithConfig("test-key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	model.Client = &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Name   string `json:"name"`
					Strict bool   `json:"strict"`
					Schema struct {
						Properties map[string]json.RawMessage `json:"properties"`
					} `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request.ResponseFormat.Type != "json_schema" || request.ResponseFormat.JSONSchema.Name != "program_outline" || !request.ResponseFormat.JSONSchema.Strict {
			t.Errorf("gateway response format: %+v", request.ResponseFormat)
		}
		for _, field := range []string{"goal", "outcomes", "modules", "final_attestation_hours", "sources"} {
			if _, ok := request.ResponseFormat.JSONSchema.Schema.Properties[field]; !ok {
				t.Errorf("missing schema field %q", field)
			}
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{\"goal\":\"test\",\"outcomes\":[],\"modules\":[],\"final_attestation_hours\":0,\"sources\":[]}"}}]}`))}, nil
	})}
	var result GeneratedOutline
	if _, err := model.JSON(context.Background(), "instruction", nil, &result); err != nil || result.Goal != "test" {
		t.Fatalf("structured response: %+v, %v", result, err)
	}
}

func TestOutlineHourPlan72(t *testing.T) {
	plan := outlineHourPlan(72)
	for _, fragment := range []string{"final_attestation_hours=4", "m1=17", "m4=17", "t1=8", "t2=9", "t8=9"} {
		if !strings.Contains(plan, fragment) {
			t.Fatalf("hour plan missing %q: %s", fragment, plan)
		}
	}
}

func TestGatewayLectureSchemaRequiresCompleteBatch(t *testing.T) {
	format, ok := gatewayResponseFormat(&GeneratedLectureBatch{})
	if !ok {
		t.Fatal("lecture response format missing")
	}
	root := format["json_schema"].(map[string]any)["schema"].(map[string]any)
	lectures := root["properties"].(map[string]any)["lectures"].(map[string]any)
	if lectures["minItems"] != 1 || lectures["maxItems"] != 1 {
		t.Fatal("lecture batch must contain exactly one lecture")
	}
	fields := lectures["items"].(map[string]any)["properties"].(map[string]any)
	if fields["plan"].(map[string]any)["minLength"] != 1 {
		t.Fatal("empty lecture plan allowed")
	}
	slides := fields["slides"].(map[string]any)
	if slides["minItems"] != 3 || slides["maxItems"] != 5 || slides["items"].(map[string]any)["minLength"] != 1 || slides["items"].(map[string]any)["maxLength"] != 350 {
		t.Fatal("slide count or content length is unbounded")
	}
	if fields["plan"].(map[string]any)["maxLength"] != 900 {
		t.Fatal("lecture plan length is unbounded")
	}
}

func TestGatewayMaterialSchemasBoundOutputText(t *testing.T) {
	for _, output := range []any{&GeneratedAssessmentBatch{}, &GeneratedLectureBatch{}} {
		format, _ := gatewayResponseFormat(output)
		properties := format["json_schema"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
		sources := properties["sources"].(map[string]any)
		if sources["maxItems"] != 1 {
			t.Fatal("material source count is unbounded")
		}
		sourceFields := sources["items"].(map[string]any)["properties"].(map[string]any)
		for _, field := range []string{"id", "title", "location"} {
			if _, ok := sourceFields[field].(map[string]any)["maxLength"]; !ok {
				t.Fatalf("source %s length is unbounded", field)
			}
		}
		if batch, ok := properties["assessments"].(map[string]any); ok {
			fields := batch["items"].(map[string]any)["properties"].(map[string]any)
			if batch["minItems"] != 2 || batch["maxItems"] != 2 {
				t.Fatal("assessment batch must contain two items")
			}
			textLimit := 0
			for _, field := range []string{"question", "answer", "rubric"} {
				textLimit += fields[field].(map[string]any)["maxLength"].(int)
			}
			if textLimit*2 > 2700 {
				t.Fatal("assessment batch text exceeds the response budget")
			}
		}
	}
}

func TestCloudModelConfigurationRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL, modelID string
		allowed                []string
		tokens                 int
	}{
		{"unapproved host", "https://elsewhere.example/v1", GigaSchoolModelID, []string{GigaSchoolModelID}, 1024},
		{"gateway URL suffix", GigaSchoolBaseURL + "/", GigaSchoolModelID, []string{GigaSchoolModelID}, 1024},
		{"gateway model absent from allowlist", GigaSchoolBaseURL, GigaSchoolModelID, []string{"other"}, 1024},
		{"other GigaChat model", GigaSchoolBaseURL, "GigaChat/other", []string{"GigaChat/other"}, 1024},
		{"direct with gateway model", CloudBaseURL, GigaSchoolModelID, []string{GigaSchoolModelID}, 1024},
		{"zero tokens", GigaSchoolBaseURL, GigaSchoolModelID, []string{GigaSchoolModelID}, 0},
		{"too many tokens", GigaSchoolBaseURL, GigaSchoolModelID, []string{GigaSchoolModelID}, MaxResponseTokens + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewCloudModelWithConfig("key", tc.modelID, tc.allowed, CloudModelConfig{BaseURL: tc.baseURL, ResponseTokens: tc.tokens}); err == nil {
				t.Fatal("invalid model configuration accepted")
			}
		})
	}
}

func TestGatewayRejectsChangedModelBeforeRequest(t *testing.T) {
	model, err := NewCloudModelWithConfig("key", GigaSchoolModelID, []string{GigaSchoolModelID}, CloudModelConfig{BaseURL: GigaSchoolBaseURL, ResponseTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	model.ModelID = "GigaChat/other"
	model.Allowed[model.ModelID] = true
	model.Client = &http.Client{Transport: modelRoundTrip(func(_ *http.Request) (*http.Response, error) {
		t.Fatal("unapproved model reached transport")
		return nil, nil
	})}
	var result map[string]any
	if _, err := model.JSON(context.Background(), "instruction", nil, &result); err == nil {
		t.Fatal("changed model accepted")
	}
}

func TestCloudAdapterKeepsUsageForInvalidReply(t *testing.T) {
	for _, body := range []string{
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		`{"choices":[{"message":{"content":"not JSON"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		`{"choices":[{"message":{"content":"null"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		`{"choices":[{"message":{"content":"{\"ok\":true}"},"finish_reason":"length"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
	} {
		model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
		if err != nil {
			t.Fatal(err)
		}
		model.Client = modelClient(body)
		var result map[string]any
		usage, err := model.JSON(context.Background(), "test instruction", nil, &result)
		if err == nil || usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
			t.Fatalf("invalid response lost usage: %+v, error: %v", usage, err)
		}
	}
}

func TestCloudAdapterRejectsMalformedOrOversizedResponse(t *testing.T) {
	for name, body := range map[string]string{
		"trailing data": `{"choices":[{"message":{"content":"{}"}}]} {}`,
		"oversized":     `{"choices":[{"message":{"content":"{}"}}]}` + strings.Repeat(" ", 4<<20),
	} {
		t.Run(name, func(t *testing.T) {
			model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
			if err != nil {
				t.Fatal(err)
			}
			model.Client = modelClient(body)
			var result map[string]any
			if _, err := model.JSON(context.Background(), "instruction", nil, &result); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestCloudAdapterDoesNotFollowRedirect(t *testing.T) {
	model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	model.Client = &http.Client{Transport: modelRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusTemporaryRedirect,
			Header:     http.Header{"Location": []string{"https://elsewhere.example/chat/completions"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})}
	var result map[string]any
	if _, err := model.JSON(context.Background(), "instruction", nil, &result); err == nil {
		t.Fatal("redirect accepted")
	}
	if requests != 1 {
		t.Fatalf("followed redirect: %d requests", requests)
	}
}

type invalidBatchModel struct {
	assessment bool
	partial    bool
}

func (m invalidBatchModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	if m.assessment {
		if batch, ok := output.(*GeneratedAssessmentBatch); ok {
			if m.partial {
				batch.Assessments = []Assessment{{Kind: "test", Question: "Question", Answer: "Answer"}}
			}
			return Usage{PromptTokens: 2, CompletionTokens: 1}, nil
		}
	} else if batch, ok := output.(*GeneratedLectureBatch); ok {
		if m.partial {
			batch.Lectures = []Lecture{{Plan: "Plan"}}
		}
		return Usage{PromptTokens: 2, CompletionTokens: 1}, nil
	}
	return (DemoModel{}).JSON(ctx, instruction, input, output)
}

func TestGeneratorRejectsIncompleteMaterialBatchAndKeepsUsage(t *testing.T) {
	spec := testSpec()
	outline, _, err := (Generator{Model: DemoModel{}}).Outline(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	p := Program{Spec: spec, Goal: outline.Goal, Outcomes: outline.Outcomes, Modules: outline.Modules, FinalAttestation: outline.FinalAttestation}
	for _, model := range []invalidBatchModel{{assessment: true}, {assessment: true, partial: true}, {}, {partial: true}} {
		_, usage, err := (Generator{Model: model}).Materials(context.Background(), p)
		if err == nil || usage.PromptTokens < 2 || usage.CompletionTokens < 1 {
			t.Fatalf("model=%+v: usage=%+v, err=%v", model, usage, err)
		}
	}
}

func TestGatewayOutlineSchemaRequiresPlannedNonemptyModules(t *testing.T) {
	spec := testSpec()
	spec.Requested = 16
	format, _ := gatewayResponseFormatForInput(spec, &GeneratedOutline{})
	properties := format["json_schema"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)
	modules := properties["modules"].(map[string]any)
	if modules["minItems"] != 2 || modules["maxItems"] != 2 {
		t.Fatal("extra result-shaped modules allowed")
	}
	topics := modules["items"].(map[string]any)["properties"].(map[string]any)["topics"].(map[string]any)
	if topics["minItems"] != 2 || topics["maxItems"] != 2 {
		t.Fatal("empty module allowed")
	}
	activities := topics["items"].(map[string]any)["properties"].(map[string]any)["activities"].(map[string]any)
	if activities["minItems"] != 1 {
		t.Fatal("empty topic allowed")
	}
}

type repairOutlineModel struct {
	calls         int
	alwaysInvalid bool
}

func (m *repairOutlineModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	m.calls++
	usage, err := (DemoModel{}).JSON(ctx, instruction, input, output)
	if err != nil {
		return usage, err
	}
	out := output.(*GeneratedOutline)
	if m.calls == 1 || m.alwaysInvalid {
		out.Outcomes[0].TopicIDs = nil
	}
	if m.calls == 2 && !strings.Contains(instruction, "validation_issues") {
		panic("repair has no validation feedback")
	}
	return Usage{PromptTokens: 10, CompletionTokens: 5}, nil
}

func TestOutlineRepairsStructuralErrorsOnceAndAccountsForBothCalls(t *testing.T) {
	for _, alwaysInvalid := range []bool{false, true} {
		model := &repairOutlineModel{alwaysInvalid: alwaysInvalid}
		out, usage, err := (Generator{Model: model}).Outline(context.Background(), testSpec())
		if err != nil || model.calls != 2 || usage != (Usage{PromptTokens: 20, CompletionTokens: 10}) {
			t.Fatalf("calls=%d usage=%+v err=%v", model.calls, usage, err)
		}
		p := Program{Spec: testSpec(), Goal: out.Goal, Outcomes: out.Outcomes, Modules: out.Modules, FinalAttestation: out.FinalAttestation}
		if hasErrors(p.ValidateOutline()) != alwaysInvalid {
			t.Fatalf("invalid outline was hidden or repair failed: %+v", p.ValidateOutline())
		}
	}
}

// The gateway rejects uniqueItems with a grammar error rather than ignoring it.
func TestGatewayOutlineUsesSupportedArrayConstraints(t *testing.T) {
	format, _ := gatewayResponseFormatForInput(testSpec(), &GeneratedOutline{})
	var inspect func(any)
	inspect = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if _, exists := node["uniqueItems"]; exists {
				t.Fatal("gateway rejects uniqueItems")
			}
			for _, child := range node {
				inspect(child)
			}
		case []any:
			for _, child := range node {
				inspect(child)
			}
		}
	}
	inspect(format)
}
