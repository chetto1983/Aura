package adaptive

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestCohortStoreSaveRejectsUnverifiedCohorts(t *testing.T) {
	store := NewCohortStore(nil, nil)

	tests := []struct {
		name   string
		cohort *FocalCohort
	}{
		{name: "nil", cohort: nil},
		{name: "zero value", cohort: &FocalCohort{}},
		{
			name: "forged identity",
			cohort: &FocalCohort{
				id:              uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
				sha256:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				predicateSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
				spec:            focalCohortTestSpec(),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.Save(t.Context(), test.cohort)
			if !errors.Is(err, ErrUnverifiedCohort) {
				t.Fatalf("Save() error = %v, want ErrUnverifiedCohort", err)
			}
		})
	}
}

func TestCohortStoreIsNilSafe(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	request := FocalEnrollmentRequest{
		OwnerID:                  cohort.Scope().OwnerID,
		CohortID:                 cohort.ID(),
		RequestID:                uuid.Must(uuid.NewV7()),
		EvaluationConversationID: uuid.Must(uuid.NewV7()),
		Domain:                   cohort.Domain(),
		Point:                    cohort.Predicate().Point,
		PointOrdinal:             cohort.Predicate().Ordinal,
	}

	var nilStore *CohortStore
	if err := nilStore.Save(t.Context(), cohort); err == nil {
		t.Fatal("nil CohortStore.Save() error = nil")
	}
	if _, err := nilStore.Load(t.Context(), request.OwnerID, request.CohortID); err == nil {
		t.Fatal("nil CohortStore.Load() error = nil")
	}
	if _, err := nilStore.Enroll(t.Context(), request, nil); err == nil {
		t.Fatal("nil CohortStore.Enroll() error = nil")
	}

	store := NewCohortStore(nil, nil)
	if err := store.Save(t.Context(), cohort); err == nil {
		t.Fatal("CohortStore without dependencies Save() error = nil")
	}
	if _, err := store.Load(t.Context(), request.OwnerID, request.CohortID); err == nil {
		t.Fatal("CohortStore without dependencies Load() error = nil")
	}
	if _, err := store.Enroll(t.Context(), request, nil); err == nil {
		t.Fatal("CohortStore without dependencies Enroll() error = nil")
	}
}
