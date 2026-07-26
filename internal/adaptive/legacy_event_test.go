package adaptive

import (
	"encoding/json"
	"errors"
	"fmt"
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
