package hours

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

type StandardBinding struct {
	OutcomeID string `json:"outcome_id"`
	Code      string `json:"code"`
	Title     string `json:"title"`
	Location  string `json:"location"`
	Evidence  string `json:"evidence"`
	Confirmed bool   `json:"confirmed"`
}

// BindStandard records a methodist's source confirmation and revokes approval
// because a changed professional competency needs a new outline review.
func (s Service) BindStandard(ctx context.Context, id string, revision int, b StandardBinding) (Program, error) {
	p, err := s.loadRevision(ctx, id, revision)
	if err != nil {
		return p, err
	}
	if p.State == Draft {
		return p, ErrInvalidState
	}
	for _, field := range []struct {
		value string
		limit int
	}{{b.OutcomeID, 64}, {b.Code, 64}, {b.Title, 150}, {b.Location, 250}, {b.Evidence, 4000}} {
		if strings.TrimSpace(field.value) == "" || utf8.RuneCountInString(field.value) > field.limit {
			return p, fmt.Errorf("%w: заполните реквизиты источника и фрагмент в допустимых пределах", ErrInvalidInput)
		}
	}
	if !b.Confirmed || !containsStandardCode(b.Evidence, b.Code) {
		return p, fmt.Errorf("%w: подтвердите проверку источника; фрагмент должен содержать точный код трудовой функции", ErrInvalidInput)
	}
	locationOK := false
	for _, location := range suppliedSourceLocations(p.Spec) {
		locationOK = locationOK || b.Location == location
	}
	if u, parseErr := url.Parse(b.Location); parseErr == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil {
		locationOK = true
	}
	if !locationOK {
		return p, fmt.Errorf("%w: укажите HTTP(S)-адрес источника или имя загруженного референса", ErrInvalidInput)
	}
	digest := sha256.Sum256([]byte(b.Location))
	sourceID := fmt.Sprintf("source-%x", digest[:8])
	found := false
	proposed := p
	proposed.Outcomes = append([]Outcome(nil), p.Outcomes...)
	proposed.Sources = append([]Source(nil), p.Sources...)
	for i := range proposed.Outcomes {
		if proposed.Outcomes[i].ID == b.OutcomeID {
			proposed.Outcomes[i].StandardCode, proposed.Outcomes[i].SourceID = b.Code, sourceID
			found = true
			break
		}
	}
	if !found {
		return p, fmt.Errorf("%w: результат обучения не найден", ErrInvalidInput)
	}
	source := Source{ID: sourceID, Title: b.Title, Location: b.Location, Evidence: b.Evidence, Status: "verified"}
	replaced := false
	for i, old := range proposed.Sources {
		if old.Location == b.Location {
			source.ID = old.ID
			for j := range proposed.Outcomes {
				if proposed.Outcomes[j].ID == b.OutcomeID {
					proposed.Outcomes[j].SourceID = old.ID
				}
			}
			if old.Evidence != "" && !strings.Contains(old.Evidence, b.Evidence) {
				source.Evidence = old.Evidence + "\n" + b.Evidence
			}
			if utf8.RuneCountInString(source.Evidence) > 12000 {
				return p, fmt.Errorf("%w: фрагменты источника превышают 12000 символов", ErrInvalidInput)
			}
			proposed.Sources[i] = source
			replaced = true
			break
		}
	}
	if !replaced {
		proposed.Sources = append(proposed.Sources, source)
	}
	if hasErrors(validateStandardLinks(proposed)) {
		return p, fmt.Errorf("%w: привязка нарушает существующие ссылки на источники", ErrInvalidInput)
	}
	return s.Revise(ctx, id, revision, proposed)
}
