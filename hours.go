package hours

import (
	"fmt"
	"math"
)

// Hours is a decimal count of academic hours. Validation converts it to whole
// minutes before summing, so half-hour lessons are checked exactly.
type Hours float64

func (h Hours) minutes() (int64, error) {
	v := float64(h)
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > float64(math.MaxInt64)/60 {
		return 0, fmt.Errorf("hours must be nonnegative and finite")
	}
	m := math.Round(v * 60)
	if math.Abs(v*60-m) > 1e-7 {
		return 0, fmt.Errorf("hours must be a whole number of minutes")
	}
	return int64(m), nil
}

// Distribution records the requested volume and each distinct kind of work.
// FinalAttestation is counted once, separately from SelfStudy.
type Distribution struct {
	Requested             Hours `json:"requested"`
	Lectures              Hours `json:"lectures"`
	Practice              Hours `json:"practice"`
	Laboratory            Hours `json:"laboratory"`
	SelfStudy             Hours `json:"self_study"`
	FinalAttestation      Hours `json:"final_attestation"`
	IndependentInternship bool  `json:"independent_internship"`
}

// Validate checks nonnegative hours, the total, and the contact-work rule for
// an explicitly independent internship.
func (d Distribution) Validate() error {
	values := []struct {
		name  string
		hours Hours
	}{
		{"requested", d.Requested},
		{"lectures", d.Lectures},
		{"practice", d.Practice},
		{"laboratory", d.Laboratory},
		{"self-study", d.SelfStudy},
		{"final attestation", d.FinalAttestation},
	}
	minutes := make([]int64, len(values))
	for i, value := range values {
		var err error
		minutes[i], err = value.hours.minutes()
		if err != nil {
			return fmt.Errorf("%s: %w", value.name, err)
		}
	}

	if d.IndependentInternship && (d.Lectures != 0 || d.Practice != 0 || d.Laboratory != 0) {
		return fmt.Errorf("independent internship cannot include contact work")
	}

	remaining := minutes[0]
	for _, part := range minutes[1:] {
		if part > remaining {
			return fmt.Errorf("work and final attestation hours must equal requested hours")
		}
		remaining -= part
	}
	if remaining != 0 {
		return fmt.Errorf("work and final attestation hours must equal requested hours")
	}
	return nil
}
