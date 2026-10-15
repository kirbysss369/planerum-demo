package hours

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateHoursAPI(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantValid  bool
	}{
		{"valid internship", `{"requested":256,"self_study":254,"final_attestation":2,"independent_internship":true}`, http.StatusOK, true},
		{"incorrect total", `{"requested":256,"self_study":253,"final_attestation":2}`, http.StatusUnprocessableEntity, false},
		{"contact work", `{"requested":256,"lectures":1,"self_study":253,"final_attestation":2,"independent_internship":true}`, http.StatusUnprocessableEntity, false},
		{"unknown field", `{"requested":256,"self_study":254,"final_attestation":2,"unknown":1}`, http.StatusBadRequest, false},
		{"null", `null`, http.StatusBadRequest, false},
		{"second object", `{} {}`, http.StatusBadRequest, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/v1/hours/validate", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			Handler().ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, tt.wantStatus, response.Body.String())
			}
			var result validationResponse
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if result.Valid != tt.wantValid || (result.Error != "") == tt.wantValid {
				t.Fatalf("response = %+v, want valid = %t", result, tt.wantValid)
			}
		})
	}
}
