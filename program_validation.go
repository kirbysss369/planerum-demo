package hours

import (
	"fmt"
	"slices"
	"strings"
)

func (p Program) Distribution() (Distribution, error) {
	d := Distribution{Requested: p.Spec.Requested, FinalAttestation: p.FinalAttestation, IndependentInternship: p.Spec.IndependentInternship}
	for _, m := range p.Modules {
		for _, t := range m.Topics {
			for _, a := range t.Activities {
				switch a.Kind {
				case "lecture":
					d.Lectures += a.Hours
				case "practice":
					d.Practice += a.Hours
				case "laboratory":
					d.Laboratory += a.Hours
				case "self_study":
					d.SelfStudy += a.Hours
				default:
					return d, fmt.Errorf("activity %s has unknown kind %q", a.ID, a.Kind)
				}
			}
		}
	}
	return d, nil
}

// ValidateOutline checks the deterministic rules before methodist approval.
func (p Program) ValidateOutline() []Issue {
	var issues []Issue
	add := func(path, message string) { issues = append(issues, Issue{"error", path, message}) }
	if err := p.Spec.Validate(); err != nil {
		add("spec", err.Error())
	}
	if strings.TrimSpace(p.Spec.Title) == "" || strings.TrimSpace(p.Spec.Audience) == "" || strings.TrimSpace(p.Goal) == "" {
		add("spec", "title, audience and goal are required")
	}
	if p.Spec.Kind != Qualification && p.Spec.Kind != General {
		add("spec.kind", "unknown program kind")
	}
	if p.Spec.Requested < 16 || p.Spec.Requested > 500 {
		add("spec.requested", "supported range is 16–500 hours")
	}
	if len(p.Modules) == 0 || len(p.Outcomes) == 0 {
		add("outline", "modules and learning outcomes are required")
	}
	d, err := p.Distribution()
	if err != nil {
		add("modules", err.Error())
	} else if err := d.Validate(); err != nil {
		add("hours", err.Error())
	}
	topicIDs := map[string]bool{}
	ids := map[string]bool{}
	for _, m := range p.Modules {
		if m.ID == "" || ids[m.ID] {
			add("modules", "module IDs must be unique and nonempty")
		}
		if strings.TrimSpace(m.Title) == "" {
			add("modules."+m.ID, "module title is required")
		}
		ids[m.ID] = true
		if len(m.Topics) == 0 {
			add("modules."+m.ID, "module needs a topic")
		}
		for _, t := range m.Topics {
			if t.ID == "" || ids[t.ID] {
				add("topics", "topic IDs must be unique and nonempty")
			}
			if strings.TrimSpace(t.Title) == "" {
				add("topics."+t.ID, "topic title is required")
			}
			ids[t.ID] = true
			topicIDs[t.ID] = true
			if len(t.Activities) == 0 {
				add("topics."+t.ID, "topic needs an activity")
			}
			for _, a := range t.Activities {
				if a.ID == "" || ids[a.ID] {
					add("activities", "activity IDs must be unique and nonempty")
				}
				if strings.TrimSpace(a.Name) == "" {
					add("activities."+a.ID, "activity name is required")
				}
				ids[a.ID] = true
				if _, err := a.Hours.minutes(); err != nil {
					add("activities."+a.ID, err.Error())
				} else if a.Hours == 0 {
					add("activities."+a.ID, "activity hours must be positive")
				}
			}
		}
	}
	for _, o := range p.Outcomes {
		if o.ID == "" || ids[o.ID] {
			add("outcomes", "outcome IDs must be unique and nonempty")
		}
		ids[o.ID] = true
		if strings.TrimSpace(o.Text) == "" || len(o.TopicIDs) == 0 {
			add("outcomes."+o.ID, "outcome needs text and a topic")
		}
		if !nonemptyTexts(o.Knowledge) || !nonemptyTexts(o.Skills) || !nonemptyTexts(o.Experience) {
			add("outcomes."+o.ID, "outcome needs knowledge, skills and practical abilities")
		}
		for _, id := range o.TopicIDs {
			if !topicIDs[id] {
				add("outcomes."+o.ID, "unknown topic "+id)
			}
		}
	}
	issues = append(issues, validateStandardLinks(p)...)
	if p.HoursRebalanced {
		issues = append(issues, Issue{Severity: "warning", Path: "hours", Message: "Часы распределены алгоритмом по весам модели; проверьте их перед утверждением"})
	}
	return issues
}

// ValidateComplete adds assessment, source and lecture checks to the outline rules.
func (p Program) ValidateComplete() []Issue {
	issues := p.ValidateOutline()
	assessmentIDs := map[string]bool{}
	outcomeIDs := map[string]bool{}
	outcomeAssessmentLinks := map[string]map[string]bool{}
	for _, o := range p.Outcomes {
		outcomeIDs[o.ID] = true
		outcomeAssessmentLinks[o.ID] = map[string]bool{}
		for _, id := range o.AssessmentIDs {
			outcomeAssessmentLinks[o.ID][id] = true
		}
	}
	for _, a := range p.Assessments {
		if a.ID == "" || assessmentIDs[a.ID] {
			issues = append(issues, Issue{"error", "assessments", "assessment IDs must be unique and nonempty"})
		}
		assessmentIDs[a.ID] = true
		if len(a.OutcomeIDs) == 0 {
			issues = append(issues, Issue{"error", "assessments." + a.ID, "assessment needs an outcome link"})
		}
		if strings.TrimSpace(a.Question) == "" || (strings.TrimSpace(a.Answer) == "" && strings.TrimSpace(a.Rubric) == "") {
			issues = append(issues, Issue{"error", "assessments." + a.ID, "question and answer or rubric are required"})
		}
		if a.Kind != "test" && a.Kind != "practice" {
			issues = append(issues, Issue{"error", "assessments." + a.ID, "assessment kind must be test or practice"})
		}
		if a.Kind == "test" && strings.TrimSpace(a.Answer) == "" {
			issues = append(issues, Issue{"error", "assessments." + a.ID, "test needs an answer key"})
		}
		if a.Kind == "practice" && strings.TrimSpace(a.Rubric) == "" {
			issues = append(issues, Issue{"error", "assessments." + a.ID, "practice task needs a rubric"})
		}
		for _, id := range a.OutcomeIDs {
			if !outcomeIDs[id] {
				issues = append(issues, Issue{"error", "assessments." + a.ID, "unknown outcome " + id})
			} else if !outcomeAssessmentLinks[id][a.ID] {
				issues = append(issues, Issue{"error", "assessments." + a.ID, "outcome link is not reciprocal: " + id})
			}
		}
	}
	for _, o := range p.Outcomes {
		unique := map[string]bool{}
		kinds := map[string]bool{}
		for _, id := range o.AssessmentIDs {
			if !assessmentIDs[id] {
				issues = append(issues, Issue{"error", "outcomes." + o.ID, "unknown assessment " + id})
			}
			unique[id] = true
			for _, a := range p.Assessments {
				if a.ID == id {
					if !slices.Contains(a.OutcomeIDs, o.ID) {
						issues = append(issues, Issue{"error", "outcomes." + o.ID, "assessment link is not reciprocal: " + id})
					} else {
						kinds[a.Kind] = true
					}
				}
			}
		}
		if len(unique) < 2 || len(kinds) < 2 {
			issues = append(issues, Issue{"error", "outcomes." + o.ID, "at least two different assessments are required"})
		}
	}
	topicIDs := map[string]bool{}
	for _, m := range p.Modules {
		for _, t := range m.Topics {
			topicIDs[t.ID] = true
		}
	}
	lecturedTopics := map[string]bool{}
	lectureIDs := map[string]bool{}
	for _, l := range p.Lectures {
		if l.ID == "" || lectureIDs[l.ID] || lecturedTopics[l.TopicID] {
			issues = append(issues, Issue{"error", "lectures", "lecture IDs and topic links must be unique and nonempty"})
		}
		lectureIDs[l.ID] = true
		lecturedTopics[l.TopicID] = true
		if !topicIDs[l.TopicID] {
			issues = append(issues, Issue{"error", "lectures." + l.ID, "unknown topic"})
		}
		if strings.TrimSpace(l.Plan) == "" || !nonemptyTexts(l.Slides) {
			issues = append(issues, Issue{"error", "lectures." + l.ID, "lecture needs plan and slides"})
		}
	}
	for _, module := range p.Modules {
		for _, topic := range module.Topics {
			if hasLectureHours(topic) && !lecturedTopics[topic.ID] {
				issues = append(issues, Issue{"error", "lectures." + topic.ID, "lecture material is missing"})
			}
		}
	}
	for _, s := range p.Sources {
		if s.Status != "verified" || s.Evidence == "" {
			issues = append(issues, Issue{"warning", "sources." + s.ID, "source is not confirmed"})
		}
	}
	return issues
}

func nonemptyTexts(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func hasErrors(issues []Issue) bool {
	for _, issue := range issues {
		if issue.Severity == "error" {
			return true
		}
	}
	return false
}
