package hours

import (
	"net/http"
)

// Handler exposes the hours validator over HTTP.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/hours/validate", validateHours)
	return mux
}

type validationResponse struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func validateHours(w http.ResponseWriter, r *http.Request) {
	var distribution *Distribution
	if err := decodeRequest(w, r, 64<<10, &distribution); err != nil || distribution == nil {
		writeValidation(w, http.StatusBadRequest, validationResponse{Error: "invalid JSON distribution"})
		return
	}
	if err := distribution.Validate(); err != nil {
		writeValidation(w, http.StatusUnprocessableEntity, validationResponse{Error: err.Error()})
		return
	}
	writeValidation(w, http.StatusOK, validationResponse{Valid: true})
}

func writeValidation(w http.ResponseWriter, status int, response validationResponse) {
	writeJSON(w, status, response)
}
