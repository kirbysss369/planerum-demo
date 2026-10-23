package hours

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReviewActionsAndExportIncludeNormativeEvidence(t *testing.T) {
	svc, p := readyProgram(t)
	app := App{Service: svc}
	post := func(request AgentRequest) (AgentResponse, int) {
		body, _ := json.Marshal(request)
		response := httptest.NewRecorder()
		app.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/agent", strings.NewReader(string(body))))
		var data AgentResponse
		if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		return data, response.Code
	}
	data, status := post(AgentRequest{Action: "bind_standard", ProgramID: p.ID, Revision: p.Revision, Binding: StandardBinding{OutcomeID: p.Outcomes[0].ID, Code: "A/01.6", Title: "Учебный референс профстандарта", Location: "https://example.org/standard", Evidence: "Функция A/01.6: анализ требований.", Confirmed: true}})
	if status != 200 || data.Regulatory == nil || data.Regulatory.ConfirmedOutcomes != 1 {
		t.Fatalf("binding API: %+v %d", data, status)
	}
	p = *data.Program
	p, err := svc.Approve(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	p, err = svc.Generate(context.Background(), p.ID, p.Revision)
	if err != nil {
		t.Fatal(err)
	}
	adapted, status := post(AgentRequest{Action: "adapt", ProgramID: p.ID, Revision: p.Revision, Message: "Поправка: 36 часов, дистанционно"})
	if status != 200 || adapted.Program == nil || adapted.Program.Origin == nil || adapted.Program.State != Draft || adapted.Program.Spec.Requested != 36 {
		t.Fatalf("adaptation API: %+v %d", adapted, status)
	}
	python := os.Getenv("OFFICE_PYTHON")
	if python == "" {
		t.Skip("set OFFICE_PYTHON for Office export integration")
	}
	root := t.TempDir()
	exporter := Exporter{Root: root, Python: python, Script: filepath.Join("office", "export.py")}
	if _, err := exporter.Export(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, p.ID, "revision-"+strconv.Itoa(p.Revision))
	reportData, err := os.ReadFile(filepath.Join(dir, "validation-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report ValidationReport
	json.Unmarshal(reportData, &report)
	if report.GenerationMode != "demo" || report.Regulatory.ConfirmedOutcomes != 1 || len(report.GenerationMetrics[len(report.GenerationMetrics)-1].Calls) == 0 {
		t.Fatalf("export lost evidence: %+v", report)
	}
	archive, err := zip.OpenReader(filepath.Join(dir, "program.docx"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name == "word/document.xml" {
			reader, _ := file.Open()
			defer reader.Close()
			decoder := xml.NewDecoder(reader)
			var text strings.Builder
			for {
				token, err := decoder.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if chars, ok := token.(xml.CharData); ok {
					text.Write(chars)
				}
			}
			for _, expected := range []string{"Нормативные основания подготовки", "273-ФЗ", "629", "Код трудовой функции: A/01.6", "Функция A/01.6: анализ требований."} {
				if !strings.Contains(text.String(), expected) {
					t.Fatalf("DOCX missing %q", expected)
				}
			}
		}
	}
}
