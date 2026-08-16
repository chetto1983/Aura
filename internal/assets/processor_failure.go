package assets

import (
	"errors"

	"github.com/chetto1983/aura/internal/documents"
)

// ProcessorFailedCode is the error_code a processing failure carries when nothing more
// specific is known: the file is not going to become searchable and re-running would fail
// the same way.
const ProcessorFailedCode = "processor_failed"

// DeleteInFlightCode marks the ONE processing failure that is not the file's fault.
//
// documents.ErrDocumentDeleteInFlight means another document with this same source is
// still being deleted, which is a race that resolves on its own in seconds. Recorded as
// processor_failed it was indistinguishable from an unsupported format or a corrupt
// upload: same status, same code, and the person's only move was to upload the file again
// with nothing telling them that waiting would have done. A distinct code is what lets the
// cockpit say "retry", a support answer be right, and a future re-enqueue key off
// something better than a substring of an error message.
const DeleteInFlightCode = "delete_in_flight"

// processorFailureCode classifies a processing failure for the asset row.
//
// errors.Is, not a string match: the error is wrapped twice on the way up here
// ("catalog document: …" in documents.Service, then the processor's own return), and only
// the sentinel survives that intact.
func processorFailureCode(err error) string {
	if errors.Is(err, documents.ErrDocumentDeleteInFlight) {
		return DeleteInFlightCode
	}
	return ProcessorFailedCode
}
