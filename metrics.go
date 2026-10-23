package hours

import (
	"context"
	"time"
)

// GenerationMetric records one measured attempt, including unsuccessful calls.
// Historical programs may have usage without measurements for earlier stages.
type GenerationMetric struct {
	Stage      string      `json:"stage"`
	DurationMS int64       `json:"duration_ms"`
	Usage      Usage       `json:"usage"`
	Succeeded  bool        `json:"succeeded"`
	Calls      []ModelCall `json:"calls,omitempty"`
}

func (p *Program) recordGeneration(stage string, elapsed time.Duration, usage Usage, succeeded bool, calls ...ModelCall) {
	p.GenerationMetrics = append(p.GenerationMetrics, GenerationMetric{
		Stage: stage, DurationMS: elapsed.Milliseconds(), Usage: usage, Succeeded: succeeded, Calls: calls,
	})
	p.Usage.Add(usage)
}

// Preserve a completed model attempt even if the caller disconnected meanwhile.
func (s Service) persistGeneration(ctx context.Context, revision int, p Program) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.Store.Put(saveCtx, revision, p)
}
