package hours

// ValidationReport records only checks that the service can prove from its data.
// A methodist still has to evaluate educational content and source accuracy.
type ValidationReport struct {
	ProgramID                string             `json:"program_id"`
	Revision                 int                `json:"revision"`
	OutlineRevision          int                `json:"outline_revision"`
	ApprovedRevision         int                `json:"approved_revision"`
	Hours                    Distribution       `json:"hours"`
	ModuleCount              int                `json:"module_count"`
	TopicCount               int                `json:"topic_count"`
	OutcomeCount             int                `json:"outcome_count"`
	OutcomesWithTwoMethods   int                `json:"outcomes_with_two_methods"`
	AssessmentCount          int                `json:"assessment_count"`
	VerifiedSourceCount      int                `json:"verified_source_count"`
	UnverifiedSourceCount    int                `json:"unverified_source_count"`
	Issues                   []Issue            `json:"issues"`
	StructurallyValid        bool               `json:"structurally_valid"`
	MethodicalReviewRequired bool               `json:"methodical_review_required"`
	Coverage                 []CoverageRow      `json:"coverage"`
	Usage                    Usage              `json:"usage"`
	GenerationMetrics        []GenerationMetric `json:"generation_metrics,omitempty"`
	GenerationMode           string             `json:"generation_mode"`
	Regulatory               RegulatoryReport   `json:"regulatory"`
	MeasuredDurationMS       int64              `json:"measured_duration_ms"`
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
		Coverage: buildCoverage(p), Usage: p.Usage, GenerationMetrics: p.GenerationMetrics,
		GenerationMode: generationMode(p.GenerationMetrics), Regulatory: regulatoryReport(p),
	}
	for _, module := range p.Modules {
		r.TopicCount += len(module.Topics)
	}
	for _, row := range r.Coverage {
		if row.Complete {
			r.OutcomesWithTwoMethods++
		}
	}
	for _, metric := range p.GenerationMetrics {
		r.MeasuredDurationMS += metric.DurationMS
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
