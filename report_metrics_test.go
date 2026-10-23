package hours

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type measuredModel struct{}

func (measuredModel) JSON(ctx context.Context, instruction string, input, output any) (Usage, error) {
	time.Sleep(2 * time.Millisecond)
	_, err := (DemoModel{}).JSON(ctx, instruction, input, output)
	return Usage{11, 3}, err
}

func TestReportContainsMeasuredStagesAndReadableCoverage(t *testing.T) {
	ctx := context.Background()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: measuredModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Generate(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Store.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := BuildValidationReport(p)
	if err != nil || !r.StructurallyValid || len(r.GenerationMetrics) != 2 || r.GenerationMetrics[0].Stage != "outline" || r.GenerationMetrics[1].Stage != "materials" || r.MeasuredDurationMS < 4 || r.Usage != (Usage{33, 9}) {
		t.Fatalf("measurements: %+v %v", r, err)
	}
	if len(r.Coverage) != len(p.Outcomes) || !r.Coverage[0].Complete || r.Coverage[0].Topics[0].Text == "" || r.Coverage[0].Tests[0].Text == "" || r.Coverage[0].Practice[0].Text == "" {
		t.Fatalf("coverage: %+v", r.Coverage)
	}
	data, _ := json.Marshal(p)
	var broken Program
	if err := json.Unmarshal(data, &broken); err != nil {
		t.Fatal(err)
	}
	broken.Assessments[0].OutcomeIDs = nil
	r, err = BuildValidationReport(broken)
	if err != nil || r.StructurallyValid || r.Coverage[0].Complete {
		t.Fatalf("broken link accepted: %+v %v", r, err)
	}
}

func TestFailedMaterialAttemptKeepsMeasurementsAndUsage(t *testing.T) {
	ctx := context.Background()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: failingMaterialsModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Generate(ctx, p.ID, p.Revision); err == nil {
		t.Fatal("expected generation failure")
	}
	p, err = svc.Store.Get(ctx, p.ID)
	if err != nil || len(p.GenerationMetrics) != 2 || p.GenerationMetrics[1].Succeeded || p.GenerationMetrics[1].Usage != (Usage{10, 5}) || p.Usage != (Usage{10, 5}) {
		t.Fatalf("failed attempt not recorded: %+v %v", p, err)
	}
}
