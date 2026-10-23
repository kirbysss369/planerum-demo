package hours

import (
	"context"
	"fmt"
	"testing"
)

func TestRebalanceOutlineHoursPreservesStructure(t *testing.T) {
	out := GeneratedOutline{
		FinalAttestation: 4,
		Modules: []Module{{ID: "m1", Topics: []Topic{
			{ID: "t1", Activities: []Activity{{ID: "a1", Kind: "lecture", Hours: 10}, {ID: "a2", Kind: "practice", Hours: 10}}},
			{ID: "t2", Activities: []Activity{{ID: "a3", Kind: "lecture", Hours: 10}, {ID: "a4", Kind: "practice", Hours: 10}}},
		}}},
	}
	if !rebalanceOutlineHours(&out, 36) {
		t.Fatal("expected model hours to be rebalanced")
	}
	var total Hours = out.FinalAttestation
	for _, topic := range out.Modules[0].Topics {
		if len(topic.Activities) != 2 {
			t.Fatal("activities were lost")
		}
		for _, activity := range topic.Activities {
			if activity.Hours <= 0 || activity.ID == "" {
				t.Fatalf("invalid activity after rebalance: %+v", activity)
			}
			total += activity.Hours
		}
	}
	if total != 36 {
		t.Fatalf("total = %v, want 36", total)
	}
	if rebalanceOutlineHours(&out, 36) {
		t.Fatal("already balanced hours changed again")
	}
}

func TestRebalanceOutlineHoursRejectsEmptyTopics(t *testing.T) {
	out := GeneratedOutline{FinalAttestation: 4, Modules: []Module{{Topics: []Topic{{}}}}}
	if rebalanceOutlineHours(&out, 72) {
		t.Fatal("empty topic should not be silently repaired")
	}
}

func TestRebalanceOutlineHoursKeepsDenseActivities(t *testing.T) {
	activities := make([]Activity, 80)
	for i := range activities {
		activities[i] = Activity{ID: "activity", Kind: "practice", Hours: 1}
	}
	out := GeneratedOutline{FinalAttestation: 4, Modules: []Module{{Topics: []Topic{{Activities: activities}}}}}
	if !rebalanceOutlineHours(&out, 72) {
		t.Fatal("expected dense activities to be rebalanced")
	}
	got := out.Modules[0].Topics[0].Activities
	if len(got) != 80 {
		t.Fatalf("kept %d of 80 activities", len(got))
	}
	var total Hours = out.FinalAttestation
	for _, activity := range got {
		if activity.Hours <= 0 {
			t.Fatalf("activity lost its hours: %+v", activity)
		}
		total += activity.Hours
	}
	if total != 72 {
		t.Fatalf("total = %v, want 72", total)
	}
}

func TestRebalanceOutlineHoursRepairsZeroHourActivity(t *testing.T) {
	topic := Topic{Activities: []Activity{
		{ID: "a1", Kind: "lecture", Hours: 0},
		{ID: "a2", Kind: "practice", Hours: 32},
	}}
	out := GeneratedOutline{FinalAttestation: 4, Modules: []Module{{Topics: []Topic{topic}}}}
	if !rebalanceOutlineHours(&out, 36) {
		t.Fatal("zero-hour activity was not rebalanced")
	}
	activities := out.Modules[0].Topics[0].Activities
	if len(activities) != 2 || activities[0].Hours <= 0 || activities[1].Hours <= 0 || activities[0].Hours+activities[1].Hours+out.FinalAttestation != 36 {
		t.Fatalf("invalid activity hours after rebalance: %+v", activities)
	}
}

func TestOutlineRejectsZeroHourActivity(t *testing.T) {
	out, _, err := (Generator{Model: DemoModel{}}).Outline(context.Background(), testSpec())
	if err != nil {
		t.Fatal(err)
	}
	out.Modules[0].Topics[0].Activities[0].Hours = 0
	p := Program{Spec: testSpec(), Goal: out.Goal, Outcomes: out.Outcomes, Modules: out.Modules, FinalAttestation: out.FinalAttestation}
	if !hasErrors(p.ValidateOutline()) {
		t.Fatal("zero-hour activity passed outline validation")
	}
}

type imbalancedOutlineModel struct{}

func (imbalancedOutlineModel) JSON(ctx context.Context, instruction string, input any, output any) (Usage, error) {
	usage, err := (DemoModel{}).JSON(ctx, instruction, input, output)
	if outline, ok := output.(*GeneratedOutline); ok && err == nil {
		outline.Modules[0].Topics[0].Activities[0].Hours += 2
	}
	return usage, err
}

func TestRebalanceWarningSurvivesApprovalAndRevision(t *testing.T) {
	ctx := context.Background()
	svc := Service{Store: NewMemoryStore(), Generator: Generator{Model: imbalancedOutlineModel{}}}
	p, _, err := svc.Create(ctx, testSpec())
	if err != nil || !p.HoursRebalanced || !hasWarning(p.Issues) {
		t.Fatalf("create warning: %+v, %v", p.Issues, err)
	}
	p, err = svc.Approve(ctx, p.ID, p.Revision)
	if err != nil || !hasWarning(p.Issues) {
		t.Fatalf("approval warning: %+v, %v", p.Issues, err)
	}
	proposed := p
	proposed.Sources = append([]Source(nil), p.Sources...)
	proposed.Sources = append(proposed.Sources, Source{ID: "extra", Title: "Для проверки", Status: "unverified"})
	p, err = svc.Revise(ctx, p.ID, p.Revision, proposed)
	if err != nil || !hasWarning(p.Issues) || !p.HoursRebalanced {
		t.Fatalf("revision warning: %+v, %v", p.Issues, err)
	}
}

func hasWarning(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == "warning" && issue.Path == "hours" {
			return true
		}
	}
	return false
}

func TestRebalancePreservesPrescribedTopicBudgets(t *testing.T) {
	for _, hours := range []Hours{16, 24, 72, 256} {
		t.Run(fmt.Sprint(hours), func(t *testing.T) {
			final, budgets := plannedOutlineHours(hours)
			out := GeneratedOutline{FinalAttestation: final}
			for i := 0; i < len(budgets); i += 2 {
				out.Modules = append(out.Modules, Module{Topics: []Topic{
					{Activities: []Activity{{Kind: "lecture", Hours: 2}, {Kind: "practice", Hours: 2}}},
					{Activities: []Activity{{Kind: "lecture", Hours: 2}, {Kind: "practice", Hours: 2}}},
				}})
			}
			rebalanceOutlineHours(&out, hours)
			var total Hours = out.FinalAttestation
			for mi, m := range out.Modules {
				for ti, topic := range m.Topics {
					var sum Hours
					for _, a := range topic.Activities {
						sum += a.Hours
						if a.Hours <= 0 {
							t.Fatal("lost activity")
						}
					}
					if sum != budgets[mi*2+ti] {
						t.Fatalf("topic %d=%v, want %v", mi*2+ti, sum, budgets[mi*2+ti])
					}
					total += sum
				}
			}
			if total != hours || rebalanceOutlineHours(&out, hours) {
				t.Fatalf("unstable budget: %v", total)
			}
		})
	}
}

func TestRebalanceChecksTopicBudgetsEvenWhenTotalIsCorrect(t *testing.T) {
	out := GeneratedOutline{FinalAttestation: 2, Modules: []Module{
		{Topics: []Topic{{Activities: []Activity{{Hours: 4}}}, {Activities: []Activity{{Hours: 4}}}}},
		{Topics: []Topic{{Activities: []Activity{{Hours: 3}}}, {Activities: []Activity{{Hours: 3}}}}},
	}}
	if !rebalanceOutlineHours(&out, 16) {
		t.Fatal("incorrect 8+6 module split accepted")
	}
	for _, m := range out.Modules {
		if m.Topics[0].Activities[0].Hours != 3 || m.Topics[1].Activities[0].Hours != 4 {
			t.Fatalf("bad 7-hour module: %+v", m)
		}
	}
}
