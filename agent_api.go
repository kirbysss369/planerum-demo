package hours

import (
	"context"
	"errors"
	"net/http"
)

type AgentRequest struct {
	Action    string          `json:"action"`
	ProgramID string          `json:"program_id,omitempty"`
	JobID     string          `json:"job_id,omitempty"`
	Revision  int             `json:"revision,omitempty"`
	Message   string          `json:"message,omitempty"`
	Spec      ProgramSpec     `json:"spec,omitempty"`
	Program   Program         `json:"program,omitempty"`
	Binding   StandardBinding `json:"binding,omitempty"`
}

type AgentResponse struct {
	Program    *Program          `json:"program,omitempty"`
	Job        *GenerationJob    `json:"job,omitempty"`
	Questions  []string          `json:"questions,omitempty"`
	Issues     []Issue           `json:"issues,omitempty"`
	Artifacts  map[string]string `json:"artifacts,omitempty"`
	Error      string            `json:"error,omitempty"`
	Coverage   []CoverageRow     `json:"coverage,omitempty"`
	Regulatory *RegulatoryReport `json:"regulatory,omitempty"`
}

func (a App) agent(w http.ResponseWriter, r *http.Request) {
	var request AgentRequest
	if err := decodeRequest(w, r, 2<<20, &request); err != nil {
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: "invalid JSON request"})
		return
	}
	response, status, err := a.dispatch(r.Context(), request)
	if response.Program != nil && response.Program.State != Draft {
		response.Coverage = buildCoverage(*response.Program)
		report := regulatoryReport(*response.Program)
		response.Regulatory = &report
	}
	if err != nil {
		response.Error, status = err.Error(), agentErrorStatus(err)
	}
	writeAgent(w, status, response)
}

func (a App) dispatch(ctx context.Context, request AgentRequest) (AgentResponse, int, error) {
	var p Program
	var questions []string
	var err error
	switch request.Action {
	case "adapt":
		p, err = a.Service.Adapt(ctx, request.ProgramID, request.Revision, request.Message)
	case "intake":
		p, err = a.Service.Intake(ctx, request.ProgramID, request.Revision, request.Message, request.Spec)
	case "answer":
		if request.ProgramID != "" || request.Message != "" {
			p, err = a.Service.Intake(ctx, request.ProgramID, request.Revision, request.Message, request.Spec)
		} else {
			p, questions, err = a.Service.Create(ctx, request.Spec)
		}
	case "create":
		if request.ProgramID != "" {
			p, questions, err = a.Service.CreateFromDraft(ctx, request.ProgramID, request.Revision, request.Spec)
		} else {
			p, questions, err = a.Service.Create(ctx, request.Spec)
		}
	case "status":
		p, err = a.Service.Store.Get(ctx, request.ProgramID)
	case "approve_outline":
		p, err = a.Service.Approve(ctx, request.ProgramID, request.Revision)
	case "generate":
		p, err = a.Service.Generate(ctx, request.ProgramID, request.Revision)
	case "generate_async":
		return a.enqueueGeneration(ctx, request)
	case "job_status":
		return a.generationStatus(ctx, request.JobID)
	case "bind_standard":
		p, err = a.Service.BindStandard(ctx, request.ProgramID, request.Revision, request.Binding)
	case "revise":
		p, err = a.Service.Revise(ctx, request.ProgramID, request.Revision, request.Program)
	case "export":
		p, err = a.Service.loadRevision(ctx, request.ProgramID, request.Revision)
		if err == nil {
			files, exportErr := a.Exporter.Export(ctx, p)
			return AgentResponse{Program: &p, Issues: p.Issues, Artifacts: files}, http.StatusOK, exportErr
		}
	default:
		return AgentResponse{}, 0, ErrInvalidInput
	}
	response := AgentResponse{Questions: questions}
	if p.ID != "" {
		response.Program, response.Issues = &p, p.Issues
		if p.State == Draft {
			response.Questions = p.Questions
		}
	}
	return response, http.StatusOK, err
}

func agentErrorStatus(err error) int {
	var provider *ModelProviderError
	if errors.As(err, &provider) {
		if provider.Temporary() {
			return http.StatusServiceUnavailable
		}
		return http.StatusBadGateway
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrInvalidState), errors.Is(err, ErrGenerationValidation):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrInvalidInput):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeAgent(w http.ResponseWriter, status int, response AgentResponse) {
	writeJSON(w, status, response)
}
