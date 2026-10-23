package hours

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const modelTemperature = 0.2

const jsonInstructionSuffix = "\nОтветьте только одним объектом JSON без Markdown."

const (
	CloudBaseURL            = "https://foundation-models.api.cloud.ru/v1"
	GigaSchoolBaseURL       = "https://hub.gigaschool.ru/api/v1/llm/v1"
	GigaSchoolModelID       = "GigaChat/GigaChat-3-Pro"
	DefaultCloudTokens      = 8192
	DefaultGigaSchoolTokens = 4096
	MaxResponseTokens       = 8192
)

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

type ModelProviderError struct {
	Status     int
	RetryAfter time.Duration
}

func (e *ModelProviderError) Temporary() bool {
	return e.Status == 429 || e.Status == 502 || e.Status == 503 || e.Status == 504
}
func (e *ModelProviderError) Error() string {
	if e.Temporary() {
		return fmt.Sprintf("Сервис модели временно недоступен (HTTP %d); повторите позже", e.Status)
	}
	return fmt.Sprintf("Ошибка сервиса модели: HTTP %d", e.Status)
}

func providerStatusError(response *http.Response) error {
	delay := 500 * time.Millisecond
	if value := response.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
			if seconds > 2 {
				delay = 3 * time.Second
			} else {
				delay = time.Duration(seconds) * time.Second
			}
		} else if date, err := http.ParseTime(value); err == nil {
			delay = time.Until(date)
			if delay < 0 {
				delay = 0
			}
		}
	}
	return &ModelProviderError{Status: response.StatusCode, RetryAfter: delay}
}

type CloudModel struct {
	Key            string
	ModelID        string
	Allowed        map[string]bool
	Client         *http.Client
	baseURL        string
	responseTokens int
}

func NewCloudModel(key, modelID string, allowed []string) (*CloudModel, error) {
	return NewCloudModelWithConfig(key, modelID, allowed, CloudModelConfig{BaseURL: CloudBaseURL, ResponseTokens: DefaultCloudTokens})
}

type CloudModelConfig struct {
	BaseURL        string
	ResponseTokens int
}

func NewCloudModelWithConfig(key, modelID string, allowed []string, config CloudModelConfig) (*CloudModel, error) {
	if key == "" || modelID == "" {
		return nil, errors.New("model API key and model ID are required")
	}
	if config.BaseURL != CloudBaseURL && config.BaseURL != GigaSchoolBaseURL {
		return nil, errors.New("model base URL is not an approved endpoint")
	}
	if config.ResponseTokens < 1 || config.ResponseTokens > MaxResponseTokens {
		return nil, fmt.Errorf("response token limit must be between 1 and %d", MaxResponseTokens)
	}
	m := &CloudModel{Key: key, ModelID: modelID, Allowed: map[string]bool{}, Client: &http.Client{Timeout: 2 * time.Minute}, baseURL: config.BaseURL, responseTokens: config.ResponseTokens}
	for _, id := range allowed {
		if id := strings.TrimSpace(id); id != "" {
			m.Allowed[id] = true
		}
	}
	if !m.Allowed[modelID] {
		return nil, errors.New("model ID is not in the explicit allowlist")
	}
	if !approvedModel(config.BaseURL, modelID) {
		return nil, errors.New("model ID is not approved for the selected endpoint")
	}
	return m, nil
}

func approvedModel(baseURL, modelID string) bool {
	switch baseURL {
	case GigaSchoolBaseURL:
		return modelID == GigaSchoolModelID
	case CloudBaseURL:
		low := strings.ToLower(modelID)
		return strings.Contains(low, "qwen") || strings.Contains(low, "deepseek")
	default:
		return false
	}
}

func (m *CloudModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	var usage Usage
	if !m.Allowed[m.ModelID] || m.Key == "" || !approvedModel(m.baseURL, m.ModelID) || m.responseTokens < 1 || m.responseTokens > MaxResponseTokens {
		return usage, errors.New("model is not authorized")
	}
	data, err := json.Marshal(input)
	if err != nil {
		return usage, err
	}
	requestBody := map[string]any{
		"model":       m.ModelID,
		"messages":    []map[string]string{{"role": "system", "content": instruction + jsonInstructionSuffix}, {"role": "user", "content": string(data)}},
		"temperature": modelTemperature,
	}
	if m.baseURL == GigaSchoolBaseURL {
		requestBody["max_tokens"] = m.responseTokens
		if responseFormat, ok := gatewayResponseFormatForInput(input, output); ok {
			requestBody["response_format"] = responseFormat
		}
	} else {
		requestBody["max_completion_tokens"] = m.responseTokens
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return usage, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return usage, err
	}
	req.Header.Set("Authorization", "Bearer "+m.Key)
	req.Header.Set("Content-Type", "application/json")
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	cloudClient := *client
	cloudClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := cloudClient.Do(req)
	if err != nil {
		return usage, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return usage, providerStatusError(resp)
	}
	const maxResponseBytes = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return usage, fmt.Errorf("read model response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return usage, errors.New("model response exceeds size limit")
	}
	var wire struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return usage, fmt.Errorf("invalid model response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return wire.Usage, errors.New("model returned no choices")
	}
	choice := wire.Choices[0]
	if choice.FinishReason == "length" {
		return wire.Usage, errors.New("model response was cut off by the token limit")
	}
	content := strings.TrimSpace(choice.Message.Content)
	if !strings.HasPrefix(content, "{") {
		return wire.Usage, errors.New("model returned no JSON object")
	}
	if err := json.Unmarshal([]byte(content), output); err != nil {
		return wire.Usage, fmt.Errorf("model returned invalid JSON: %w", err)
	}
	return wire.Usage, nil
}
