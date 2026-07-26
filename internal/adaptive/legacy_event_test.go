package adaptive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func newLegacyEvent(p eventParams) (Event, error) {
	if p.Kind == EventDelivery {
		return Event{}, errors.New("adaptive delivery events require the typed constructor")
	}
	canonical, err := canonicalPayload(p.Payload)
	if err != nil {
		return Event{}, err
	}
	var envelope struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(canonical, &envelope); err != nil {
		return Event{}, fmt.Errorf("decode adaptive payload envelope: %w", err)
	}
	if envelope.SchemaVersion != SchemaVersion1 {
		return Event{}, errors.New("legacy adaptive fixtures require schema_version 1.0")
	}
	p.Payload = canonical
	return newEvent(p)
}

// recordLegacyEvent is called only from build-tagged integration tests
// (store_integration_test.go, projector_race_integration_test.go and others under
// db_integration). golangci-lint analyses one build configuration at a time, so it cannot
// see those callers and reports the method as unused; enabling the tags only moves the
// false positive to symbols used under a different combination.
//
//nolint:unused // used by db_integration-tagged tests the linter does not compile
func (s *Store) recordLegacyEvent(ctx context.Context, event Event) (int64, error) {
	sequence, err := s.recordValidated(ctx, event)
	if err != nil {
		return 0, fmt.Errorf("record legacy adaptive event %s: %w", event.ID, err)
	}
	return sequence, nil
}

// recordLegacyEventTx has the same tag-only callers as recordLegacyEvent above.
//
//nolint:unused // used by db_integration-tagged tests the linter does not compile
func (s *Store) recordLegacyEventTx(
	ctx context.Context,
	q *sqlc.Queries,
	event Event,
) (sequence int64, duplicate bool, err error) {
	if s == nil {
		return 0, false, errors.New("legacy adaptive fixture requires a store")
	}
	if q == nil {
		return 0, false, errors.New("legacy adaptive fixture requires database queries")
	}
	return s.recordValidatedTx(ctx, q, event)
}
