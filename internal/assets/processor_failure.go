package assets

// ProcessorFailedCode is the error_code a processing failure carries: the file is not going
// to become usable and re-running would fail the same way.
//
// It used to be one of two, chosen by processorFailureCode(err): a failure caused by a
// document row left mid-delete (documents.ErrDocumentDeleteInFlight) was recorded as
// delete_in_flight instead, because re-uploading — the move processor_failed invites —
// cannot clear a block that is on the source rather than on the bytes. That distinction
// went with the catalog write path: nothing in this process creates a document row any
// more, so nothing can leave one mid-delete, and a second code no statement can produce is
// a branch that only looks like a promise.
const ProcessorFailedCode = "processor_failed"
