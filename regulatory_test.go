package hours

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStandardBindingRequiresHumanEvidenceAndInvalidatesApproval(t *testing.T) {
	svc, p := readyProgram(t)
	binding := StandardBinding{OutcomeID: p.Outcomes[0].ID, Code: "A/01.6", Title: "Учебный референс профстандарта", Location: "https://example.org/standard", Evidence: "Трудовая функция A/01.6: анализ требований.", Confirmed: true}
	for _, bad := range []StandardBinding{
		{OutcomeID: binding.OutcomeID, Code: binding.Code, Title: binding.Title, Location: binding.Location, Evidence: binding.Evidence},
		{OutcomeID: binding.OutcomeID, Code: binding.Code, Title: binding.Title, Location: binding.Location, Evidence: "Функция A/01.60", Confirmed: true},
		{OutcomeID: binding.OutcomeID, Code: binding.Code, Title: binding.Title, Location: "file:///private/reference", Evidence: binding.Evidence, Confirmed: true},
	} {
		if _, err := svc.BindStandard(context.Background(), p.ID, p.Revision, bad); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid binding accepted: %v", err)
		}
		unchanged, _ := svc.Store.Get(context.Background(), p.ID)
		if !reflect.DeepEqual(unchanged, p) {
			t.Fatal("rejected binding changed program")
		}
	}
	updated, err := svc.BindStandard(context.Background(), p.ID, p.Revision, binding)
	if err != nil || updated.State != OutlineReady || updated.ApprovedRevision != 0 || len(updated.Assessments) != 0 || regulatoryReport(updated).ConfirmedOutcomes != 1 {
		t.Fatalf("binding=%+v err=%v", updated, err)
	}
	if hasErrors(updated.ValidateOutline()) {
		t.Fatal(updated.ValidateOutline())
	}
	if _, err := svc.BindStandard(context.Background(), p.ID, p.Revision, binding); !errors.Is(err, ErrConflict) {
		t.Fatal("stale confirmation accepted")
	}
	updated.Outcomes[0].StandardCode = "B/01.6"
	if !hasErrors(updated.ValidateOutline()) {
		t.Fatal("invented function code accepted")
	}
}

func TestStandardBindingsPreserveOtherEvidenceForSameDocument(t *testing.T) {
	svc, p := readyProgram(t)
	second := p.Outcomes[0]
	second.ID = "outcome-2"
	p.Outcomes = append(p.Outcomes, second)
	p, err := svc.Revise(context.Background(), p.ID, p.Revision, p)
	if err != nil {
		t.Fatal(err)
	}
	for i, code := range []string{"A/01.6", "A/02.6"} {
		p, err = svc.BindStandard(context.Background(), p.ID, p.Revision, StandardBinding{OutcomeID: p.Outcomes[i].ID, Code: code, Title: "Учебный референс", Location: "https://example.org/standard", Evidence: "Код " + code + ": описание функции.", Confirmed: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(p.Sources) != 1 || regulatoryReport(p).ConfirmedOutcomes != 2 || hasErrors(p.ValidateOutline()) {
		t.Fatalf("lost source evidence: %+v", p.Sources)
	}
}

func TestRegulatoryBasisDistinguishesTemplates(t *testing.T) {
	for _, kind := range []ProgramKind{Qualification, General} {
		p := Program{Spec: ProgramSpec{Kind: kind}, Outcomes: []Outcome{{ID: "o"}}}
		report := regulatoryReport(p)
		if len(report.Basis) != 2 || !report.ReviewRequired || report.StandardRequired != (kind == Qualification) {
			t.Fatalf("incorrect regulatory report: %+v", report)
		}
		if kind == Qualification && report.Basis[1].Title != "Приказ Минобрнауки России от 24.03.2025 № 266" {
			t.Fatal("obsolete order used as current basis")
		}
	}
}

func TestStandardEvidenceChangesRequireNewApproval(t *testing.T) {
	svc, p := readyProgram(t)
	binding := StandardBinding{OutcomeID: p.Outcomes[0].ID, Code: "A/01.6", Title: "Учебный референс", Location: "https://example.org/standard", Evidence: "Код A/01.6: анализ требований.", Confirmed: true}
	p, err := svc.BindStandard(context.Background(), p.ID, p.Revision, binding)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Approve(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Generate(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	binding.Evidence = "Код A/01.6: уточнённый состав действий."
	p, err = svc.BindStandard(context.Background(), p.ID, p.Revision, binding)
	if err != nil || p.State != OutlineReady || p.ApprovedRevision != 0 || len(p.Assessments) != 0 {
		t.Fatalf("changed evidence kept approval: %+v %v", p, err)
	}
}
