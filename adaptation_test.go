package hours

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAdaptationCreatesIndependentDraftAndPreservesSource(t *testing.T) {
	for _, fail := range []bool{false, true} {
		svc, source := readyProgram(t)
		svc.Generator.Model = scriptedIntakeModel{call: func(_ string, input, output any) (Usage, error) {
			in := input.(IntakeInput)
			if strings.Contains(in.Spec.Requirements, source.Goal) {
				t.Fatal("intake must not echo the full source outline")
			}
			if fail {
				return Usage{PromptTokens: 8}, errors.New("provider failure")
			}
			spec := in.Spec
			spec.Audience = "Школьники"
			spec.Requested = 16
			output.(*ParsedSpec).Spec = spec
			return Usage{PromptTokens: 8, CompletionTokens: 3}, nil
		}}
		draft, err := svc.Adapt(context.Background(), source.ID, source.Revision, "Адаптируй для школьников на 16 часов")
		if (err != nil) != fail || draft.ID == source.ID || draft.Origin == nil || draft.Origin.ProgramID != source.ID || draft.Origin.Revision != source.Revision || draft.Origin.Reference == nil || draft.Origin.Reference.Goal != source.Goal || draft.State != Draft || draft.ApprovedRevision != 0 || len(draft.Assessments) != 0 || draft.Usage.PromptTokens != 8 {
			t.Fatalf("adaptation=%+v err=%v", draft, err)
		}
		if !fail && (draft.Spec.Audience != "Школьники" || draft.Spec.Requested != 16) {
			t.Fatal("new conditions were not applied")
		}
		saved, _ := svc.Store.Get(context.Background(), draft.ID)
		if !reflect.DeepEqual(saved, draft) {
			t.Fatal("adaptation result was not saved")
		}
		original, _ := svc.Store.Get(context.Background(), source.ID)
		if !reflect.DeepEqual(source, original) {
			t.Fatal("source program changed")
		}
		if _, err := svc.Adapt(context.Background(), source.ID, source.Revision-1, "Сменить аудиторию"); !errors.Is(err, ErrConflict) {
			t.Fatal("stale source accepted")
		}
	}
}
