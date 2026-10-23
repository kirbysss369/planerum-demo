package hours

import (
	"slices"
	"strings"
)

type CoverageItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// CoverageRow exposes structural links; completeness does not certify content quality.
type CoverageRow struct {
	OutcomeID   string         `json:"outcome_id"`
	OutcomeText string         `json:"outcome_text"`
	Topics      []CoverageItem `json:"topics"`
	Tests       []CoverageItem `json:"tests"`
	Practice    []CoverageItem `json:"practice"`
	Complete    bool           `json:"complete"`
}

func buildCoverage(p Program) []CoverageRow {
	topics, topicCounts := map[string]Topic{}, map[string]int{}
	assessments, assessmentCounts := map[string]Assessment{}, map[string]int{}
	outcomeCounts := map[string]int{}
	for _, module := range p.Modules {
		for _, topic := range module.Topics {
			topics[topic.ID] = topic
			topicCounts[topic.ID]++
		}
	}
	for _, assessment := range p.Assessments {
		assessments[assessment.ID] = assessment
		assessmentCounts[assessment.ID]++
	}
	for _, outcome := range p.Outcomes {
		outcomeCounts[outcome.ID]++
	}
	rows := make([]CoverageRow, 0, len(p.Outcomes))
	for _, outcome := range p.Outcomes {
		row := CoverageRow{OutcomeID: outcome.ID, OutcomeText: outcome.Text, Topics: []CoverageItem{}, Tests: []CoverageItem{}, Practice: []CoverageItem{}}
		valid := outcome.ID != "" && outcomeCounts[outcome.ID] == 1 && strings.TrimSpace(outcome.Text) != ""
		seen := map[string]bool{}
		for _, id := range outcome.TopicIDs {
			topic, ok := topics[id]
			if !ok || topicCounts[id] != 1 {
				valid = false
				continue
			}
			if !seen[id] {
				row.Topics = append(row.Topics, CoverageItem{id, topic.Title})
				seen[id] = true
			}
		}
		seen = map[string]bool{}
		for _, id := range outcome.AssessmentIDs {
			assessment, ok := assessments[id]
			if !ok || assessmentCounts[id] != 1 || !slices.Contains(assessment.OutcomeIDs, outcome.ID) {
				valid = false
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			if strings.TrimSpace(assessment.Question) == "" {
				valid = false
				continue
			}
			switch assessment.Kind {
			case "test":
				if strings.TrimSpace(assessment.Answer) == "" {
					valid = false
					continue
				}
				row.Tests = append(row.Tests, CoverageItem{id, assessment.Question})
			case "practice":
				if strings.TrimSpace(assessment.Rubric) == "" {
					valid = false
					continue
				}
				row.Practice = append(row.Practice, CoverageItem{id, assessment.Question})
			default:
				valid = false
			}
		}
		for _, assessment := range p.Assessments {
			if slices.Contains(assessment.OutcomeIDs, outcome.ID) && !slices.Contains(outcome.AssessmentIDs, assessment.ID) {
				valid = false
			}
		}
		row.Complete = valid && len(row.Topics) > 0 && len(row.Tests) > 0 && len(row.Practice) > 0
		rows = append(rows, row)
	}
	return rows
}
