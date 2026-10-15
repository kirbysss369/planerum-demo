package hours

import "testing"

func TestDistributionValidate(t *testing.T) {
	tests := []struct {
		name    string
		hours   Distribution
		wantErr bool
	}{
		{
			name: "independent internship 256 hours",
			hours: Distribution{
				Requested:             256,
				SelfStudy:             254,
				FinalAttestation:      2,
				IndependentInternship: true,
			},
		},
		{
			name:    "incorrect total",
			hours:   Distribution{Requested: 256, SelfStudy: 253, FinalAttestation: 2},
			wantErr: true,
		},
		{
			name:    "negative hours",
			hours:   Distribution{Requested: 256, SelfStudy: 255, FinalAttestation: 2, Lectures: -1},
			wantErr: true,
		},
		{
			name: "contact work in independent internship",
			hours: Distribution{
				Requested:             256,
				Lectures:              1,
				SelfStudy:             253,
				FinalAttestation:      2,
				IndependentInternship: true,
			},
			wantErr: true,
		},
		{
			name:  "general program with half-hour lessons",
			hours: Distribution{Requested: 24, Lectures: 4, Practice: 4, SelfStudy: 10, FinalAttestation: 6},
		},
		{
			name:  "fractional hours cannot be silently rounded",
			hours: Distribution{Requested: 1, Lectures: 0.5, Practice: 0.5},
		},
		{
			name:    "sub-minute fraction",
			hours:   Distribution{Requested: 1, Lectures: 0.001, Practice: 0.999},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.hours.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, want error = %v", err, tt.wantErr)
			}
		})
	}
}
