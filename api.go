package hours

import (
	"encoding/json"
	"io"
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var distribution *Distribution
	if err := decoder.Decode(&distribution); err != nil {
		writeValidation(w, http.StatusBadRequest, validationResponse{Error: "invalid JSON distribution"})
		return
	}
	if distribution == nil {
		writeValidation(w, http.StatusBadRequest, validationResponse{Error: "expected a JSON distribution object"})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeValidation(w, http.StatusBadRequest, validationResponse{Error: "expected one JSON distribution"})
		return
	}
	if err := distribution.Validate(); err != nil {
		writeValidation(w, http.StatusUnprocessableEntity, validationResponse{Error: err.Error()})
		return
	}
	writeValidation(w, http.StatusOK, validationResponse{Valid: true})
}

func writeValidation(w http.ResponseWriter, status int, response validationResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
