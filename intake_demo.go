package hours

import (
	"regexp"
	"strconv"
	"strings"
)

// Demo intake recognizes a few explicit Russian forms for offline flow checks.
// It is deterministic and does not attempt general language understanding.
var (
	demoHours       = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(?:академических\s+)?(?:час(?:ов|а)?|ч\.?)`)
	demoTitle       = regexp.MustCompile(`(?i)(?:по\s+|на\s+тему\s+)([^,.;!?]+)`)
	demoAudience    = regexp.MustCompile(`(?i)для\s+([^,.;!?]+)`)
	demoLevel       = regexp.MustCompile(`(?i)(?:уровень|входной\s+уровень|подготовка)\s*[:—-]\s*([^,.;!?]+)`)
	demoAttestation = regexp.MustCompile(`(?i)(?:итоговая\s+)?аттестация\s*[:—-]\s*([^,.;!?]+)`)
	demoConstraint  = regexp.MustCompile(`(?i)(?:без\s+|обязательно\s+|с\s+уч[её]том\s+)[^,.;!?]+`)
	demoHoursIntro  = regexp.MustCompile(`(?i)\s+на\s+\d`)
)

func demoInterpret(input IntakeInput) ProgramSpec {
	spec := input.Spec
	for _, message := range input.Messages {
		if message.Role != "user" {
			continue
		}
		text := strings.TrimSpace(message.Content)
		lower := strings.ToLower(text)
		if match := demoTitle.FindStringSubmatch(text); len(match) > 1 {
			title := trimDemoTail(match[1])
			if title != "" {
				spec.Title = title
			}
		}
		if strings.Contains(lower, "повышени") && strings.Contains(lower, "квалификац") {
			spec.Kind = Qualification
		}
		if strings.Contains(lower, "общеразвивающ") || strings.Contains(lower, "общая программа") {
			spec.Kind = General
		}
		if match := demoAudience.FindStringSubmatch(text); len(match) > 1 {
			value := trimDemoTail(match[1])
			if value != "" {
				spec.Audience = value
			}
		}
		if match := demoLevel.FindStringSubmatch(text); len(match) > 1 {
			spec.EntryLevel = trimDemoTail(match[1])
		} else if strings.Contains(lower, "с нуля") {
			spec.EntryLevel = "с нуля"
		}
		if strings.Contains(lower, "очно-заоч") || strings.Contains(lower, "очная-заочная") {
			spec.Delivery = "очно-заочно"
		} else if strings.Contains(lower, "дистанцион") || strings.Contains(lower, "онлайн") {
			spec.Delivery = "дистанционно"
		} else if strings.Contains(lower, "заочно") || strings.Contains(lower, "заочная") {
			spec.Delivery = "заочно"
		} else if strings.Contains(lower, "очно") || strings.Contains(lower, "очная") || strings.Contains(lower, "очное") {
			spec.Delivery = "очно"
		}
		for _, match := range demoHours.FindAllStringSubmatchIndex(text, -1) {
			prefix := strings.ToLower(text[max(0, match[0]-35):match[0]])
			if strings.Contains(prefix, "аттестаци") {
				continue
			}
			hours, err := strconv.ParseFloat(strings.ReplaceAll(text[match[2]:match[3]], ",", "."), 64)
			if err == nil {
				spec.Requested = Hours(hours)
			}
		}
		if strings.Contains(lower, "без самостоятельной стажировки") || strings.Contains(lower, "стажировка не нужна") {
			spec.IndependentInternship = false
		} else if strings.Contains(lower, "самостоятельная стажировка") || strings.Contains(lower, "самостоятельную стажировку") {
			spec.IndependentInternship = true
		}
		if match := demoAttestation.FindStringSubmatch(text); len(match) > 1 {
			spec.Attestation = trimDemoTail(match[1])
		} else if strings.Contains(lower, "защита проекта") {
			spec.Attestation = "защита проекта"
		} else if strings.Contains(lower, "зачёт") || strings.Contains(lower, "зачет") {
			spec.Attestation = "зачёт"
		} else if strings.Contains(lower, "экзамен") {
			spec.Attestation = "экзамен"
		}
		for _, constraint := range demoConstraint.FindAllString(text, -1) {
			constraint = strings.TrimSpace(constraint)
			if constraint != "" && !strings.Contains(strings.ToLower(spec.Requirements), strings.ToLower(constraint)) {
				if spec.Requirements != "" {
					spec.Requirements += "; "
				}
				spec.Requirements += constraint
			}
		}
	}
	return spec
}

func trimDemoTail(value string) string {
	value = strings.TrimSpace(value)
	for _, separator := range []string{" для ", " с нуля", " без ", " уровень", " очно", " заочно", " дистанционно", " аттестация"} {
		if index := strings.Index(strings.ToLower(value), separator); index >= 0 {
			value = value[:index]
		}
	}
	if index := demoHoursIntro.FindStringIndex(value); index != nil {
		value = value[:index[0]]
	}
	return strings.TrimSpace(value)
}
