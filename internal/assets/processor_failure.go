package assets

import (
	"errors"

	"github.com/chetto1983/aura/internal/documents"
)

// ProcessorFailedCode is the error_code a processing failure carries when nothing more
// specific is known: the file is not going to become searchable and re-running would fail
// the same way.
const ProcessorFailedCode = "processor_failed"

// DeleteInFlightCode marks the ONE processing failure that is not the file's fault: the
// source is held by a document row left mid-delete (documents.ErrDocumentDeleteInFlight,
// whose comment carries the measurement).
//
// Recorded as processor_failed it was indistinguishable from an unsupported format or a
// corrupt upload — same status, same code — and re-uploading, the move that code invites, is
// the one that cannot work: the block is on the source, not on the bytes. The error_message
// says so in words; this code is what a caller can branch on.
//
// The durable queue's retry is deliberately left alone. The worker already backs off
// exponentially with full jitter and dead-letters at MaxAttempts (documents/jobs_worker.go;
// 5, from assetProcessingIngestionJobRequest), which is the right behaviour if a delete
// workflow ever returns and costs four wasted attempts if it never does. Suppressing it
// would mean adding a permanent-failure channel to the worker whose only user is a state
// nothing can currently create.
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
