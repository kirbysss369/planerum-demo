package hours

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const generatedSourceInstruction = ` Источники разрешены только из requirements: явно переданные HTTP(S)-ссылки или имена загруженных документов из строк «Референс ИМЯ (SHA-256 ...):». В location скопируйте точную ссылку или ИМЯ документа. ID темы, модуля и результата обучения не являются источниками. Если таких ссылок и документов нет, sources должен быть пустым массивом. Не добавляйте библиографию из памяти. Переданный источник остаётся unverified до проверки методистом.`

var sourceURLPattern = regexp.MustCompile(`https?://[^\s<>"\x00-\x20]+`)
var sourceDocumentPattern = regexp.MustCompile(`(?m)^Референс (.+) \(SHA-256 [0-9a-fA-F]{64}\):\s*$`)

func suppliedSourceLocations(spec ProgramSpec) []string {
	var locations []string
	seen := map[string]bool{}
	add := func(location string) {
		if location != "" && len([]rune(location)) <= 250 && !seen[location] {
			seen[location] = true
			locations = append(locations, location)
		}
	}
	for _, match := range sourceURLPattern.FindAllString(spec.Requirements, -1) {
		location := strings.TrimRight(match, ".,;:!?)]}")
		parsed, err := url.Parse(location)
		if err == nil && parsed.Hostname() != "" && parsed.User == nil {
			add(location)
		}
	}
	for _, match := range sourceDocumentPattern.FindAllStringSubmatch(spec.Requirements, -1) {
		add(strings.TrimSpace(match[1]))
	}
	return locations
}

// Grounding establishes where a citation came from, not whether it is true.
func groundedSources(sources []Source, spec ProgramSpec) []Source {
	allowed := map[string]bool{}
	for _, location := range suppliedSourceLocations(spec) {
		allowed[location] = true
	}
	var result []Source
	for _, source := range sources {
		source.Location = strings.TrimSpace(source.Location)
		if !allowed[source.Location] || strings.TrimSpace(source.Title) == "" {
			continue
		}
		digest := sha256.Sum256([]byte(source.Location))
		source.ID = fmt.Sprintf("source-%x", digest[:8])
		source.Status, source.Evidence = "unverified", ""
		result = append(result, source)
	}
	return mergeSources(nil, result)
}

// Keep existing citations (including manually verified evidence) when several
// material batches cite the same supplied document.
func mergeSources(existing, added []Source) []Source {
	result := append([]Source(nil), existing...)
	seen := map[string]bool{}
	for _, source := range result {
		seen[source.Location] = true
	}
	for _, source := range added {
		if !seen[source.Location] {
			result = append(result, source)
			seen[source.Location] = true
		}
	}
	return result
}
