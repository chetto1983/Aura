// jsonfmt.go implements the D-07 JSON export adapter: a pure function of
// Snapshot, wrapping encoding/json.Marshal. The TYPE's shape — not this
// function — is what makes serialization safe: Snapshot has no field able to
// hold a tool argument, a tool result, a filesystem path, or the owner's
// identity id, so JSON() cannot emit one no matter how it is called.
package share

import (
	"encoding/json"
	"fmt"
)

// JSON marshals the Snapshot with its wire tags (the OQ4 contract plan
// 37F-05 mirrors in TypeScript). It takes no parameter beyond the receiver —
// that signature IS the D-07 guarantee: MD, JSON, and the public page model
// all derive from the SAME redacted Snapshot, so a future redaction fix
// cannot miss this surface by construction. encoding/json.Marshal is
// deterministic for a Snapshot value (struct fields marshal in declaration
// order; no map-typed field exists anywhere in the Snapshot family), so two
// calls on an unchanged Snapshot always yield identical bytes.
func (s Snapshot) JSON() ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("share: marshal snapshot: %w", err)
	}
	return data, nil
}
