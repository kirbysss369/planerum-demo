package hours

import (
	"strings"
	"testing"
)

func TestGeneratedSourcesRequireSuppliedLocation(t *testing.T) {
	spec := testSpec()
	spec.Requirements = "Материалы: https://example.org/sql.\nРеференс Конспект SQL.docx (SHA-256 " + strings.Repeat("a", 64) + "):\nТекст документа"
	supplied := []Source{
		{ID: "t4", Title: "Тема SQL", Location: "t4", Status: "verified", Evidence: "invented"},
		{ID: "book", Title: "SQL", Location: "https://invented.example/sql"},
		{ID: "t1", Title: "Документация SQL", Location: "https://example.org/sql", Status: "verified", Evidence: "invented"},
		{ID: "t1", Title: "Конспект", Location: "Конспект SQL.docx"},
		{ID: "duplicate", Title: "Повтор", Location: "https://example.org/sql"},
	}
	got := groundedSources(supplied, spec)
	if len(got) != 2 || got[0].ID == got[1].ID {
		t.Fatalf("citations=%+v", got)
	}
	for _, source := range got {
		if source.Status != "unverified" || source.Evidence != "" || !strings.HasPrefix(source.ID, "source-") {
			t.Fatalf("invented provenance accepted: %+v", source)
		}
	}
	if got := groundedSources(supplied, ProgramSpec{}); len(got) != 0 {
		t.Fatalf("unsupplied sources accepted: %+v", got)
	}
	verified := got[0]
	verified.Status = "verified"
	verified.Evidence = "Проверено методистом"
	merged := mergeSources([]Source{verified}, got)
	if len(merged) != 2 || merged[0] != verified {
		t.Fatalf("existing evidence changed: %+v", merged)
	}
}

func TestSourceSchemaGroundingAcrossGenerationStages(t *testing.T) {
	for _, withReference := range []bool{false, true} {
		spec := testSpec()
		if withReference {
			spec.Requirements = "Ссылка: https://example.org/manual"
		}
		for _, pair := range []struct{ input, output any }{
			{spec, &GeneratedOutline{}}, {AssessmentInput{Spec: spec}, &GeneratedAssessmentBatch{}}, {LectureInput{Spec: spec}, &GeneratedLectureBatch{}},
		} {
			format, _ := gatewayResponseFormatForInput(pair.input, pair.output)
			root := format["json_schema"].(map[string]any)["schema"].(map[string]any)
			sources := root["properties"].(map[string]any)["sources"].(map[string]any)
			if !withReference {
				if sources["maxItems"] != 0 {
					t.Fatalf("no-reference schema allows citations: %T", pair.output)
				}
			} else {
				location := sources["items"].(map[string]any)["properties"].(map[string]any)["location"].(map[string]any)
				enum := location["enum"].([]string)
				if len(enum) != 1 || enum[0] != "https://example.org/manual" {
					t.Fatalf("bad locations: %v", enum)
				}
			}
		}
	}
}
