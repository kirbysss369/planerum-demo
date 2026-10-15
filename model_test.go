package hours

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudAdapterWithLocalServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if request["model"] != "Qwen/test-internal" {
			t.Errorf("model = %v", request["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"{\"ok\":true}"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer server.Close()
	model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
	if err != nil {
		t.Fatal(err)
	}
	model.baseURL = server.URL
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

func TestCloudAdapterKeepsUsageForInvalidReply(t *testing.T) {
	for _, body := range []string{
		`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		`{"choices":[{"message":{"content":"not JSON"}}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		model, err := NewCloudModel("test-key", "Qwen/test-internal", []string{"Qwen/test-internal"})
		if err != nil {
			server.Close()
			t.Fatal(err)
		}
		model.baseURL = server.URL
		var result map[string]any
		usage, err := model.JSON(context.Background(), "test instruction", nil, &result)
		server.Close()
		if err == nil || usage.PromptTokens != 12 || usage.CompletionTokens != 3 {
			t.Fatalf("invalid response lost usage: %+v, error: %v", usage, err)
		}
	}
}
