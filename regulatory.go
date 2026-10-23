package hours

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

type RegulatoryDocument struct {
	Title    string `json:"title"`
	Location string `json:"location"`
	Scope    string `json:"scope"`
}

type RegulatoryReport struct {
	Basis             []RegulatoryDocument `json:"basis"`
	CatalogDate       string               `json:"catalog_date"`
	LinkedOutcomes    int                  `json:"linked_outcomes"`
	ConfirmedOutcomes int                  `json:"confirmed_outcomes"`
	StandardRequired  bool                 `json:"standard_required"`
	ReviewRequired    bool                 `json:"review_required"`
	Note              string               `json:"note"`
}

func regulatoryReport(p Program) RegulatoryReport {
	law := RegulatoryDocument{Title: "Федеральный закон от 29.12.2012 № 273-ФЗ «Об образовании в Российской Федерации»", Location: "https://publication.pravo.gov.ru/Document/View/0001201212300007"}
	report := RegulatoryReport{CatalogDate: "2026-10-23", ReviewRequired: true}
	if p.Spec.Kind == Qualification {
		law.Scope = "Статья 76: дополнительные профессиональные программы"
		report.StandardRequired = true
		report.Basis = []RegulatoryDocument{law, {Title: "Приказ Минобрнауки России от 24.03.2025 № 266", Location: "https://publication.pravo.gov.ru/document/0001202504230011", Scope: "Порядок организации и осуществления образовательной деятельности по дополнительным профессиональным программам; применяется с 01.09.2025 вместо приказа № 499"}}
	} else {
		law.Scope = "Статья 75: дополнительное образование детей и взрослых"
		report.Basis = []RegulatoryDocument{law, {Title: "Приказ Минпросвещения России от 27.07.2022 № 629", Location: "https://publication.pravo.gov.ru/Document/View/0001202209270013", Scope: "Порядок организации и осуществления образовательной деятельности по дополнительным общеобразовательным программам"}}
	}
	sources := map[string]Source{}
	for _, source := range p.Sources {
		sources[source.ID] = source
	}
	for _, outcome := range p.Outcomes {
		if outcome.StandardCode != "" && outcome.SourceID != "" {
			report.LinkedOutcomes++
			if source, ok := sources[outcome.SourceID]; ok && source.Status == "verified" && containsStandardCode(source.Evidence, outcome.StandardCode) {
				report.ConfirmedOutcomes++
			}
		}
	}
	report.Note = "Перечень оснований не подтверждает юридическое соответствие программы. Методист проверяет актуальность редакций, применимость требований и содержание привязок к профстандартам. Подтверждение источника означает проверку человеком."
	return report
}

var laborFunctionCodePattern = regexp.MustCompile(`^[A-ZА-ЯЁ]/[0-9]{2}\.[0-9]{1,2}$`)

func containsStandardCode(evidence, code string) bool {
	if strings.TrimSpace(code) == "" || utf8.RuneCountInString(code) > 64 || !laborFunctionCodePattern.MatchString(code) {
		return false
	}
	// A/01.6 must not match A/01.60 or an identifier embedded in another code.
	return regexp.MustCompile(`(^|[^\p{L}\p{N}/.])` + regexp.QuoteMeta(code) + `($|[^\p{L}\p{N}/.])`).MatchString(evidence)
}

func validateStandardLinks(p Program) []Issue {
	var issues []Issue
	sources := map[string]Source{}
	for _, source := range p.Sources {
		if source.ID == "" || sources[source.ID].ID != "" {
			issues = append(issues, Issue{"error", "sources", "Идентификаторы источников должны быть непустыми и уникальными"})
		}
		sources[source.ID] = source
		if source.Status == "verified" && (strings.TrimSpace(source.Evidence) == "" || strings.TrimSpace(source.Title) == "" || strings.TrimSpace(source.Location) == "") {
			issues = append(issues, Issue{"error", "sources." + source.ID, "Подтверждённому источнику нужны название, адрес и подтверждающий фрагмент"})
		}
	}
	for _, outcome := range p.Outcomes {
		if outcome.StandardCode == "" && outcome.SourceID == "" {
			continue
		}
		path := "outcomes." + outcome.ID + ".standard_code"
		source, exists := sources[outcome.SourceID]
		if outcome.StandardCode == "" || !exists {
			issues = append(issues, Issue{"error", path, "Для кода трудовой функции укажите существующий источник"})
			continue
		}
		if !containsStandardCode(source.Evidence, outcome.StandardCode) {
			issues = append(issues, Issue{"error", path, "Код трудовой функции отсутствует в подтверждающем фрагменте источника"})
		}
		if source.Status != "verified" {
			issues = append(issues, Issue{"warning", path, "Привязку к профстандарту должен подтвердить методист"})
		}
	}
	if p.Spec.Kind == Qualification && regulatoryReport(p).ConfirmedOutcomes < len(p.Outcomes) {
		issues = append(issues, Issue{"warning", "regulatory.standards", "Не все результаты связаны с подтверждёнными трудовыми функциями профстандарта; дополните нормативные привязки"})
	}
	return issues
}

func standardSources(p Program) []Source {
	ids := map[string]bool{}
	for _, outcome := range p.Outcomes {
		if outcome.StandardCode != "" {
			ids[outcome.SourceID] = true
		}
	}
	var result []Source
	for _, source := range p.Sources {
		if ids[source.ID] {
			result = append(result, source)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
