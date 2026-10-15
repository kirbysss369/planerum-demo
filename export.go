package hours

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

type Exporter struct {
	Root   string
	Python string
	Script string
}

var artifactNames = []string{"program.json", "program.docx", "lectures.pptx", "tests.xml", "validation-report.json"}

func artifactPaths(p Program) map[string]string {
	paths := map[string]string{}
	for _, name := range artifactNames {
		paths[name] = "/artifacts/" + p.ID + "/revision-" + strconv.Itoa(p.Revision) + "/" + name
	}
	return paths
}

func completeExport(dir string) bool {
	for _, name := range artifactNames {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	return true
}

func (e Exporter) Export(ctx context.Context, p Program) (map[string]string, error) {
	if p.State != Ready || p.ApprovedRevision != p.OutlineRevision || hasErrors(p.ValidateComplete()) {
		return nil, ErrInvalidState
	}
	if e.Root == "" || e.Script == "" {
		return nil, errors.New("export path is not configured")
	}
	if id, err := hex.DecodeString(p.ID); err != nil || len(id) != 12 || p.Revision < 1 {
		return nil, errors.New("invalid program identity")
	}
	parent := filepath.Join(e.Root, p.ID)
	final := filepath.Join(parent, "revision-"+strconv.Itoa(p.Revision))
	if completeExport(final) {
		return artifactPaths(p), nil
	}
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(parent, ".export-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	jsonPath := filepath.Join(dir, "program.json")
	if err := os.WriteFile(jsonPath, data, 0o640); err != nil {
		return nil, err
	}
	report, err := BuildValidationReport(p)
	if err != nil {
		return nil, err
	}
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "validation-report.json"), reportData, 0o640); err != nil {
		return nil, err
	}
	if err := writeMoodle(filepath.Join(dir, "tests.xml"), p.Assessments); err != nil {
		return nil, err
	}
	python := e.Python
	if python == "" {
		python = "python3"
	}
	command := exec.CommandContext(ctx, python, e.Script, jsonPath, dir)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("office export: %w: %s", err, output)
	}
	if !completeExport(dir) {
		return nil, errors.New("office export returned an incomplete bundle")
	}
	if err := os.Rename(dir, final); err != nil && !completeExport(final) {
		return nil, err
	}
	return artifactPaths(p), nil
}

type moodleQuiz struct {
	XMLName   xml.Name         `xml:"quiz"`
	Questions []moodleQuestion `xml:"question"`
}
type moodleText struct {
	Text   string `xml:"text"`
	Format string `xml:"format,attr,omitempty"`
}
type moodleQuestion struct {
	Type         string         `xml:"type,attr"`
	Name         moodleText     `xml:"name"`
	QuestionText moodleText     `xml:"questiontext"`
	Answers      []moodleAnswer `xml:"answer,omitempty"`
	GraderInfo   *moodleText    `xml:"graderinfo,omitempty"`
}
type moodleAnswer struct {
	Fraction string `xml:"fraction,attr"`
	Text     string `xml:"text"`
}

func writeMoodle(path string, assessments []Assessment) error {
	quiz := moodleQuiz{}
	for _, a := range assessments {
		question := moodleQuestion{Name: moodleText{Text: a.ID}, QuestionText: moodleText{Text: a.Question, Format: "plain_text"}}
		if a.Kind == "test" && a.Answer != "" {
			question.Type = "shortanswer"
			question.Answers = []moodleAnswer{{Fraction: "100", Text: a.Answer}}
		} else {
			question.Type = "essay"
			question.GraderInfo = &moodleText{Text: a.Rubric, Format: "plain_text"}
		}
		quiz.Questions = append(quiz.Questions, question)
	}
	data, err := xml.MarshalIndent(quiz, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(xml.Header), data...), 0o640)
}
