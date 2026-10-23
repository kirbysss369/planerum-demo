package hours

import (
	"context"
	"net/http"
)

func (a App) enqueueGeneration(ctx context.Context, request AgentRequest) (AgentResponse, int, error) {
	if a.Jobs == nil {
		return AgentResponse{Error: "generation queue is not configured"}, http.StatusServiceUnavailable, nil
	}
	p, err := a.Service.generationInput(ctx, request.ProgramID, request.Revision)
	if err != nil {
		return AgentResponse{}, 0, err
	}
	job, err := a.Jobs.Enqueue(ctx, p.ID, p.Revision)
	if err != nil {
		return AgentResponse{}, 0, err
	}
	return AgentResponse{Program: &p, Job: &job}, http.StatusAccepted, nil
}

func (a App) generationStatus(ctx context.Context, id string) (AgentResponse, int, error) {
	if a.Jobs == nil {
		return AgentResponse{Error: "generation queue is not configured"}, http.StatusServiceUnavailable, nil
	}
	job, err := a.Jobs.Get(ctx, id)
	if err != nil {
		return AgentResponse{}, 0, err
	}
	p, err := a.Service.Store.Get(ctx, job.ProgramID)
	if err != nil {
		return AgentResponse{}, 0, err
	}
	return AgentResponse{Program: &p, Job: &job, Issues: p.Issues}, http.StatusOK, nil
}
