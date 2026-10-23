package hours

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const generationContractVersion = "2026-10-23.1"

// ModelCall identifies the request and configuration without saving credentials
// or duplicating the user's reference documents in telemetry.
type ModelCall struct {
	ContractVersion  string  `json:"contract_version"`
	Temperature      float64 `json:"temperature,omitempty"`
	Mode             string  `json:"mode"`
	ModelID          string  `json:"model_id,omitempty"`
	Endpoint         string  `json:"endpoint,omitempty"`
	ResponseTokens   int     `json:"response_tokens,omitempty"`
	InstructionHash  string  `json:"instruction_sha256"`
	OutputHash       string  `json:"output_sha256,omitempty"`
	InputHash        string  `json:"input_sha256"`
	SchemaHash       string  `json:"schema_sha256,omitempty"`
	DurationMS       int64   `json:"duration_ms"`
	UsageUnavailable bool    `json:"usage_unavailable,omitempty"`
	Usage            Usage   `json:"usage"`
	Succeeded        bool    `json:"succeeded"`
}

type recordingModel struct {
	Model Model
	Calls []ModelCall
}

func digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func (g Generator) measured() (Generator, *recordingModel) {
	recorder := &recordingModel{Model: g.Model}
	if g.Model == nil {
		return g, recorder
	}
	return Generator{Model: recorder}, recorder
}

func (m *recordingModel) JSON(ctx context.Context, instruction string, input, output any) (Usage, error) {
	var total Usage
	for attempt := 0; attempt < 2; attempt++ {
		usage, err := m.attempt(ctx, instruction, input, output)
		total.Add(usage)
		var provider *ModelProviderError
		if err == nil || attempt == 1 || !errors.As(err, &provider) || !provider.Temporary() || provider.RetryAfter > 2*time.Second {
			return total, err
		}
		timer := time.NewTimer(provider.RetryAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return total, ctx.Err()
		case <-timer.C:
		}
	}
	return total, nil
}

func (m *recordingModel) attempt(ctx context.Context, instruction string, input, output any) (Usage, error) {
	call := ModelCall{ContractVersion: generationContractVersion, Mode: "unknown", InstructionHash: digest([]byte(instruction + jsonInstructionSuffix))}
	switch model := m.Model.(type) {
	case *CloudModel:
		call.Temperature = modelTemperature
		call.Mode, call.ModelID, call.Endpoint, call.ResponseTokens = "real", model.ModelID, model.baseURL, model.responseTokens
	case DemoModel:
		call.Mode, call.ModelID = "demo", "offline-fixture"
	}
	if data, err := json.Marshal(input); err == nil {
		call.InputHash = digest(data)
	}
	if call.Endpoint == GigaSchoolBaseURL {
		if format, ok := gatewayResponseFormatForInput(input, output); ok {
			data, _ := json.Marshal(format)
			call.SchemaHash = digest(data)
		}
	}
	started := time.Now()
	usage, err := m.Model.JSON(ctx, instruction, input, output)
	call.UsageUnavailable = err != nil && usage == (Usage{}) && call.Mode == "real"
	call.DurationMS, call.Usage, call.Succeeded = time.Since(started).Milliseconds(), usage, err == nil
	if err == nil {
		if data, marshalErr := json.Marshal(output); marshalErr == nil {
			call.OutputHash = digest(data)
		}
	}
	m.Calls = append(m.Calls, call)
	return usage, err
}

func generationMode(metrics []GenerationMetric) string {
	modes := map[string]bool{}
	for _, metric := range metrics {
		if len(metric.Calls) == 0 {
			modes["unknown"] = true
		}
		for _, call := range metric.Calls {
			modes[call.Mode] = true
		}
	}
	if len(modes) == 1 {
		for mode := range modes {
			return mode
		}
	}
	if len(modes) > 1 {
		return "mixed"
	}
	return "unknown"
}
