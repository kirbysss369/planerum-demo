package hours

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type RunEvaluation struct {
	UsageIncomplete           bool     `json:"usage_incomplete"`
	ProgramID                 string   `json:"program_id"`
	Revision                  int      `json:"revision"`
	InputHash                 string   `json:"input_sha256"`
	Mode                      string   `json:"mode"`
	Models                    []string `json:"models"`
	Configurations            []string `json:"configurations"`
	StructurallyValid         bool     `json:"structurally_valid"`
	CoverageFraction          float64  `json:"coverage_fraction"`
	ConfirmedStandardFraction float64  `json:"confirmed_standard_fraction"`
	MeasuredDurationMS        int64    `json:"measured_duration_ms"`
	TotalTokens               int      `json:"total_tokens"`
	TracedCalls               int      `json:"traced_calls"`
	OutlineWithinTarget       *bool    `json:"outline_within_180_seconds,omitempty"`
	MaterialsWithinTarget     *bool    `json:"materials_72h_within_1200_seconds,omitempty"`
}

type RepeatEvaluation struct {
	InputHash          string  `json:"input_sha256"`
	Mode               string  `json:"mode"`
	Runs               int     `json:"runs"`
	StructuralPassRate float64 `json:"structural_pass_rate"`
	DurationMeanMS     float64 `json:"duration_mean_ms"`
	DurationStdDevMS   float64 `json:"duration_stddev_ms"`
	TokensMin          int     `json:"tokens_min"`
	TokensMax          int     `json:"tokens_max"`
}

type Evaluation struct {
	Runs    []RunEvaluation    `json:"runs"`
	Repeats []RepeatEvaluation `json:"repeats"`
	Note    string             `json:"note"`
}

// EvaluatePrograms compares measured runs without making model calls. Groups
// separate input specifications, execution modes and model configurations.
func EvaluatePrograms(programs []Program) (Evaluation, error) {
	result := Evaluation{Note: "Оценка структурная, не методическая. Демо и реальные вызовы разделены. Повторяемость измерений не гарантирует дословного совпадения содержания. Старые записи без трассировки имеют режим unknown; подтверждение источников выполняет человек."}
	groups := map[string][]RunEvaluation{}
	seen := map[string]bool{}
	for _, p := range programs {
		if p.ID == "" || seen[p.ID] {
			return result, fmt.Errorf("%w: требуются разные идентификаторы независимых прогонов", ErrInvalidInput)
		}
		seen[p.ID] = true
		report, err := BuildValidationReport(p)
		if err != nil {
			return result, err
		}
		modelSpec, inputErr := outlineSpec(p)
		if inputErr != nil {
			return result, inputErr
		}
		input, _ := json.Marshal(modelSpec)
		run := RunEvaluation{ProgramID: p.ID, Revision: p.Revision, InputHash: digest(input), Mode: report.GenerationMode, StructurallyValid: report.StructurallyValid && p.State == Ready && p.ApprovedRevision == p.OutlineRevision, MeasuredDurationMS: report.MeasuredDurationMS, TotalTokens: p.Usage.PromptTokens + p.Usage.CompletionTokens}
		if len(p.Outcomes) > 0 {
			run.CoverageFraction = float64(report.OutcomesWithTwoMethods) / float64(len(p.Outcomes))
			run.ConfirmedStandardFraction = float64(report.Regulatory.ConfirmedOutcomes) / float64(len(p.Outcomes))
		}
		models, configs := map[string]bool{}, map[string]bool{}
		var outlineMS, materialMS int64
		hasOutline, hasMaterials := false, false
		outlineSucceeded, materialsSucceeded := false, false
		for _, metric := range p.GenerationMetrics {
			if metric.Stage == "outline" {
				outlineMS += metric.DurationMS
				hasOutline = true
				outlineSucceeded = outlineSucceeded || metric.Succeeded
			}
			if metric.Stage == "materials" {
				materialMS += metric.DurationMS
				hasMaterials = true
				materialsSucceeded = materialsSucceeded || metric.Succeeded
			}
			for _, call := range metric.Calls {
				run.TracedCalls++
				if call.UsageUnavailable || (!call.Succeeded && call.Mode == "real" && call.Usage == (Usage{})) {
					run.UsageIncomplete = true
				}
				models[call.ModelID] = true
				// The actual instruction/schema digests are retained in each exported call.
				data, _ := json.Marshal(struct {
					Mode, Model, Endpoint string
					Contract, Schema      string
					Temperature           float64
					Tokens                int
				}{call.Mode, call.ModelID, call.Endpoint, call.ContractVersion, call.SchemaHash, call.Temperature, call.ResponseTokens})
				configs[digest(data)] = true
			}
		}
		if hasOutline {
			met := outlineSucceeded && outlineMS <= 180000
			run.OutlineWithinTarget = &met
		}
		if hasMaterials && p.Spec.Requested == 72 {
			met := materialsSucceeded && p.State == Ready && materialMS <= 1200000
			run.MaterialsWithinTarget = &met
		}
		for model := range models {
			run.Models = append(run.Models, model)
		}
		sort.Strings(run.Models)
		for config := range configs {
			run.Configurations = append(run.Configurations, config)
		}
		sort.Strings(run.Configurations)
		key := run.InputHash + ":" + run.Mode + ":" + strings.Join(run.Configurations, ",")
		if run.Mode == "real" || run.Mode == "demo" {
			groups[key] = append(groups[key], run)
		}
		result.Runs = append(result.Runs, run)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		runs := groups[key]
		if len(runs) < 2 {
			continue
		}
		repeat := RepeatEvaluation{InputHash: runs[0].InputHash, Mode: runs[0].Mode, Runs: len(runs), TokensMin: runs[0].TotalTokens}
		for _, run := range runs {
			if run.StructurallyValid {
				repeat.StructuralPassRate++
			}
			repeat.DurationMeanMS += float64(run.MeasuredDurationMS)
			if run.TotalTokens < repeat.TokensMin {
				repeat.TokensMin = run.TotalTokens
			}
			if run.TotalTokens > repeat.TokensMax {
				repeat.TokensMax = run.TotalTokens
			}
		}
		repeat.StructuralPassRate /= float64(len(runs))
		repeat.DurationMeanMS /= float64(len(runs))
		for _, run := range runs {
			repeat.DurationStdDevMS += math.Pow(float64(run.MeasuredDurationMS)-repeat.DurationMeanMS, 2)
		}
		repeat.DurationStdDevMS = math.Sqrt(repeat.DurationStdDevMS / float64(len(runs)))
		result.Repeats = append(result.Repeats, repeat)
	}
	return result, nil
}
