package hours

import "slices"

// ValidationReport records only checks that the service can prove from its data.
// A methodist still has to evaluate educational content and source accuracy.
type ValidationReport struct {
	ProgramID                string       `json:"program_id"`
	Revision                 int          `json:"revision"`
	OutlineRevision          int          `json:"outline_revision"`
	ApprovedRevision         int          `json:"approved_revision"`
	Hours                    Distribution `json:"hours"`
	ModuleCount              int          `json:"module_count"`
	TopicCount               int          `json:"topic_count"`
	OutcomeCount             int          `json:"outcome_count"`
	OutcomesWithTwoMethods   int          `json:"outcomes_with_two_methods"`
	AssessmentCount          int          `json:"assessment_count"`
	VerifiedSourceCount      int          `json:"verified_source_count"`
	UnverifiedSourceCount    int          `json:"unverified_source_count"`
	Issues                   []Issue      `json:"issues"`
	StructurallyValid        bool         `json:"structurally_valid"`
	MethodicalReviewRequired bool         `json:"methodical_review_required"`
}

func BuildValidationReport(p Program) (ValidationReport, error) {
	d, err := p.Distribution()
	if err != nil {
		return ValidationReport{}, err
	}
	issues := p.ValidateComplete()
	r := ValidationReport{
		ProgramID: p.ID, Revision: p.Revision, OutlineRevision: p.OutlineRevision,
		ApprovedRevision: p.ApprovedRevision, Hours: d, ModuleCount: len(p.Modules),
		OutcomeCount: len(p.Outcomes), AssessmentCount: len(p.Assessments),
		Issues: issues, StructurallyValid: !hasErrors(issues), MethodicalReviewRequired: true,
	}
	for _, module := range p.Modules {
		r.TopicCount += len(module.Topics)
	}
	assessments := map[string]Assessment{}
	for _, a := range p.Assessments {
		assessments[a.ID] = a
	}
	for _, outcome := range p.Outcomes {
		methods := map[string]bool{}
		for _, id := range outcome.AssessmentIDs {
			if assessment, exists := assessments[id]; exists && slices.Contains(assessment.OutcomeIDs, outcome.ID) {
				methods[assessment.Kind] = true
			}
		}
		if len(methods) >= 2 {
			r.OutcomesWithTwoMethods++
		}
	}
	for _, source := range p.Sources {
		if source.Status == "verified" && source.Evidence != "" {
			r.VerifiedSourceCount++
		} else {
			r.UnverifiedSourceCount++
		}
	}
	return r, nil
}
