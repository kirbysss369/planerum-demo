package hours

import (
	"crypto/rand"
	"encoding/hex"
)

type ProgramState string

const (
	Draft        ProgramState = "draft"
	OutlineReady ProgramState = "outline_ready"
	Approved     ProgramState = "approved"
	Ready        ProgramState = "ready"
)

type Outcome struct {
	ID            string   `json:"id"`
	Text          string   `json:"text"`
	Knowledge     []string `json:"knowledge,omitempty"`
	Skills        []string `json:"skills,omitempty"`
	Experience    []string `json:"experience,omitempty"`
	TopicIDs      []string `json:"topic_ids"`
	AssessmentIDs []string `json:"assessment_ids,omitempty"`
	StandardCode  string   `json:"standard_code,omitempty"`
	SourceID      string   `json:"source_id,omitempty"`
}

type Activity struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Hours    Hours  `json:"hours"`
	Delivery string `json:"delivery,omitempty"`
}

type Topic struct {
	ID         string     `json:"id"`
	Title      string     `json:"title"`
	Summary    string     `json:"summary,omitempty"`
	Activities []Activity `json:"activities"`
}

type Module struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Topics []Topic `json:"topics"`
}

type Assessment struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Question   string   `json:"question"`
	Answer     string   `json:"answer,omitempty"`
	Rubric     string   `json:"rubric,omitempty"`
	OutcomeIDs []string `json:"outcome_ids"`
}

type Lecture struct {
	ID      string   `json:"id"`
	TopicID string   `json:"topic_id"`
	Plan    string   `json:"plan"`
	Slides  []string `json:"slides,omitempty"`
}

type Source struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location string `json:"location"`
	Evidence string `json:"evidence,omitempty"`
	Status   string `json:"status"` // verified or unverified
}

type Issue struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
}

type Program struct {
	Origin                *ProgramOrigin     `json:"origin,omitempty"`
	ID                    string             `json:"id"`
	Revision              int                `json:"revision"`
	OutlineRevision       int                `json:"outline_revision"`
	ApprovedRevision      int                `json:"approved_revision"`
	GeneratedFromRevision int                `json:"generated_from_revision,omitempty"`
	State                 ProgramState       `json:"state"`
	Spec                  ProgramSpec        `json:"spec"`
	Goal                  string             `json:"goal"`
	Outcomes              []Outcome          `json:"outcomes"`
	Modules               []Module           `json:"modules"`
	FinalAttestation      Hours              `json:"final_attestation_hours"`
	Assessments           []Assessment       `json:"assessments,omitempty"`
	Lectures              []Lecture          `json:"lectures,omitempty"`
	Sources               []Source           `json:"sources,omitempty"`
	Issues                []Issue            `json:"issues,omitempty"`
	Usage                 Usage              `json:"usage"`
	GenerationMetrics     []GenerationMetric `json:"generation_metrics,omitempty"`
	HoursRebalanced       bool               `json:"hours_rebalanced,omitempty"`
	Dialogue              []DialogueMessage  `json:"dialogue,omitempty"`
	Questions             []string           `json:"questions,omitempty"`
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
