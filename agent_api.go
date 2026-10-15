package hours

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type AgentRequest struct {
	Action    string      `json:"action"`
	ProgramID string      `json:"program_id,omitempty"`
	JobID     string      `json:"job_id,omitempty"`
	Revision  int         `json:"revision,omitempty"`
	Spec      ProgramSpec `json:"spec,omitempty"`
	Program   Program     `json:"program,omitempty"`
}

type AgentResponse struct {
	Program   *Program          `json:"program,omitempty"`
	Job       *GenerationJob    `json:"job,omitempty"`
	Questions []string          `json:"questions,omitempty"`
	Issues    []Issue           `json:"issues,omitempty"`
	Artifacts map[string]string `json:"artifacts,omitempty"`
	Error     string            `json:"error,omitempty"`
}

type App struct {
	Service  Service
	Jobs     JobQueue
	Exporter Exporter
	Token    string
	Embed    bool
}

func (a App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/hours/validate", Handler())
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /{$}", a.home)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.Handle("POST /v1/agent", a.auth(http.HandlerFunc(a.agent)))
	mux.Handle("POST /v1/references", a.auth(http.HandlerFunc(a.references)))
	mux.Handle("GET /artifacts/{programID}/{revision}/{filename}", a.auth(http.HandlerFunc(a.artifact)))
	return mux
}

func (a App) artifact(w http.ResponseWriter, r *http.Request) {
	id, revision, name := r.PathValue("programID"), r.PathValue("revision"), r.PathValue("filename")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 12 || !slices.Contains(artifactNames, name) || !strings.HasPrefix(revision, "revision-") {
		http.NotFound(w, r)
		return
	}
	version, err := strconv.Atoi(strings.TrimPrefix(revision, "revision-"))
	if err != nil || version < 1 || revision != "revision-"+strconv.Itoa(version) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(a.Exporter.Root, id, revision, name)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func (a App) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Token != "" && r.Header.Get("Authorization") != "Bearer "+a.Token {
			writeAgent(w, http.StatusUnauthorized, AgentResponse{Error: "access token is required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a App) agent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request AgentRequest
	if err := decoder.Decode(&request); err != nil {
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: "invalid JSON request"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: "expected one JSON request"})
		return
	}
	ctx := r.Context()
	var p Program
	var err error
	switch request.Action {
	case "create", "answer":
		var questions []string
		p, questions, err = a.Service.Create(ctx, request.Spec)
		if len(questions) > 0 {
			writeAgent(w, http.StatusOK, AgentResponse{Questions: questions})
			return
		}
	case "status":
		p, err = a.Service.Store.Get(ctx, request.ProgramID)
	case "approve_outline":
		p, err = a.Service.Approve(ctx, request.ProgramID, request.Revision)
	case "generate":
		p, err = a.Service.Generate(ctx, request.ProgramID, request.Revision)
	case "generate_async":
		if a.Jobs == nil {
			writeAgent(w, http.StatusServiceUnavailable, AgentResponse{Error: "generation queue is not configured"})
			return
		}
		p, err = a.Service.Store.Get(ctx, request.ProgramID)
		if err != nil {
			writeAgent(w, http.StatusNotFound, AgentResponse{Error: err.Error()})
			return
		}
		if p.Revision != request.Revision {
			writeAgent(w, http.StatusConflict, AgentResponse{Error: ErrConflict.Error()})
			return
		}
		if p.ApprovedRevision != p.OutlineRevision || (p.State != Approved && p.State != Ready) {
			writeAgent(w, http.StatusUnprocessableEntity, AgentResponse{Error: ErrInvalidState.Error()})
			return
		}
		job, queueErr := a.Jobs.Enqueue(ctx, p.ID, p.Revision)
		if queueErr != nil {
			writeAgent(w, http.StatusInternalServerError, AgentResponse{Error: queueErr.Error()})
			return
		}
		writeAgent(w, http.StatusAccepted, AgentResponse{Program: &p, Job: &job})
		return
	case "job_status":
		if a.Jobs == nil {
			writeAgent(w, http.StatusServiceUnavailable, AgentResponse{Error: "generation queue is not configured"})
			return
		}
		job, queueErr := a.Jobs.Get(ctx, request.JobID)
		if queueErr != nil {
			status := http.StatusInternalServerError
			if errors.Is(queueErr, ErrNotFound) {
				status = http.StatusNotFound
			}
			writeAgent(w, status, AgentResponse{Error: queueErr.Error()})
			return
		}
		p, err = a.Service.Store.Get(ctx, job.ProgramID)
		if err != nil {
			writeAgent(w, http.StatusInternalServerError, AgentResponse{Error: err.Error()})
			return
		}
		writeAgent(w, http.StatusOK, AgentResponse{Program: &p, Job: &job, Issues: p.Issues})
		return
	case "revise":
		p, err = a.Service.Revise(ctx, request.ProgramID, request.Revision, request.Program)
	case "export":
		p, err = a.Service.Store.Get(ctx, request.ProgramID)
		if err == nil && p.Revision != request.Revision {
			err = ErrConflict
		}
		if err == nil {
			var files map[string]string
			files, err = a.Exporter.Export(ctx, p)
			if err == nil {
				writeAgent(w, http.StatusOK, AgentResponse{Program: &p, Issues: p.Issues, Artifacts: files})
				return
			}
		}
	default:
		writeAgent(w, http.StatusBadRequest, AgentResponse{Error: "unknown action"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, ErrConflict) {
			status = http.StatusConflict
		}
		if errors.Is(err, ErrInvalidState) {
			status = http.StatusUnprocessableEntity
		}
		if errors.Is(err, ErrInvalidInput) {
			status = http.StatusBadRequest
		}
		writeAgent(w, status, AgentResponse{Error: err.Error(), Issues: p.Issues})
		return
	}
	writeAgent(w, http.StatusOK, AgentResponse{Program: &p, Issues: p.Issues})
}

func writeAgent(w http.ResponseWriter, status int, response AgentResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
