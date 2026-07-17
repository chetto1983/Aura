// jsonfmt.go implements the D-07 JSON export adapter: a pure function of
// Snapshot, wrapping encoding/json.Marshal. The TYPE's shape — not this
// function — is what makes serialization safe: Snapshot has no field able to
// hold a tool argument, a tool result, a filesystem path, or the owner's
// identity id, so JSON() cannot emit one no matter how it is called.
package share

import "errors"

// JSON marshals the Snapshot with its wire tags (the OQ4 contract plan
// 37F-05 mirrors in TypeScript). It takes no parameter beyond the receiver —
// that signature IS the D-07 guarantee: MD, JSON, and the public page model
// all derive from the SAME redacted Snapshot, so a future redaction fix
// cannot miss this surface by construction.
//
// RED-phase stub (task 1, TDD RED commit): the pre-commit `go vet`/build
// hook rejects a whole-module non-compiling commit, so this file ships here
// with its FINAL doc comments but an unimplemented body — every
// TestSnapshotJSON* case still fails on a real assertion (this explicit
// error), not a compile error. The GREEN commit replaces the body with
// json.Marshal.
func (s Snapshot) JSON() ([]byte, error) {
	return nil, errors.New("share: JSON not implemented")
}
