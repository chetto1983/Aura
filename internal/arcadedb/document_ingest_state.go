package arcadedb

import (
	"context"
	"fmt"
	"strings"
)

// documentIngestStateType is the row services/ingest upserts once per identity, beside the
// passages it writes, so a reader can tell "not indexed yet" from "indexed and empty".
const documentIngestStateType = "IngestStatus"

// The two values CocoIndex's UpdateHandle.watch() reports. They are its vocabulary, not
// ours: running while an update is in flight, ready once the root component has caught up.
//
// Lower case because that is what it emits. Measured 2026-09-09 against the live pipeline
// after this shipped with the member NAMES: UpdateStatus.READY.value is "ready", so Ready()
// compared "ready" against "READY" and reported a caught-up ingest as still running.
const (
	IngestRunning = "running"
	IngestReady   = "ready"
)

// IngestState is how far the reconciler has got for one identity.
//
// Before it existed, an ingest that had finished looked exactly like one that had never
// started: measured 2026-09-09, every asset row sat at `accepted` or `processing` and not
// one had ever reached `searchable`, because Go stopped writing the lifecycle states when
// the CocoIndex pipeline took them over (internal/documents/catalog_status.go) and
// nothing consumed the status stream the sidecar already had.
type IngestState struct {
	Status     string `json:"status"`
	ObservedAt string `json:"observed_at,omitempty"`
	InProgress int64  `json:"in_progress"`
	Finished   int64  `json:"finished"`
	Errors     int64  `json:"errors"`
}

// Ready reports whether the reconciler has caught up. Case-insensitive: the value is
// another process's enum, and a caught-up ingest read as running is exactly the confusion
// this field exists to remove.
func (s IngestState) Ready() bool { return strings.EqualFold(s.Status, IngestReady) }

// IngestState reads the identity's reconciler state, or nil when the sidecar has never
// written one -- a library nothing has ever been ingested into, and a state this cannot
// invent. A missing type is that same case: the DDL runs on the sidecar's first start.
func (d *DocumentIndex) IngestState(ctx context.Context, identityID string) (*IngestState, error) {
	client, err := d.tenantClient(ctx, identityID)
	if err != nil {
		return nil, err
	}
	rows, err := client.Query(ctx,
		"SELECT status, observed_at, in_progress, finished, errors FROM "+
			documentIngestStateType+" WHERE identity_id = :identity_id",
		map[string]any{"identity_id": strings.TrimSpace(identityID)},
	)
	if err != nil {
		if missingIngestType(err, documentIngestStateType) {
			return nil, nil
		}
		return nil, fmt.Errorf("arcadedb: ingest state: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 1 {
		// The unique index on identity_id is what makes this impossible; saying so beats
		// picking one of two disagreeing answers at random.
		return nil, fmt.Errorf("arcadedb: %d ingest state rows for one identity", len(rows))
	}
	status, err := requiredString(rows[0], "status")
	if err != nil {
		return nil, fmt.Errorf("arcadedb: ingest state: %w", err)
	}
	observed, err := optionalString(rows[0], "observed_at")
	if err != nil {
		return nil, fmt.Errorf("arcadedb: ingest state: %w", err)
	}
	return &IngestState{
		Status:     status,
		ObservedAt: observed,
		InProgress: intField(rows[0]["in_progress"]),
		Finished:   intField(rows[0]["finished"]),
		Errors:     intField(rows[0]["errors"]),
	}, nil
}
