package documents

import (
	"encoding/json"
	"fmt"
	"io"
)

// DoclingErrorClass is a stable failure category for retry and observability policy.
type DoclingErrorClass string

const (
	// DoclingErrorConfig means the local client configuration is invalid.
	DoclingErrorConfig DoclingErrorClass = "config"
	// DoclingErrorPrivacy means a network target failed private-address policy.
	DoclingErrorPrivacy DoclingErrorClass = "privacy"
	// DoclingErrorRequestTooLarge means an upload exceeded its declared bound.
	DoclingErrorRequestTooLarge DoclingErrorClass = "request_too_large"
	// DoclingErrorResponseTooLarge means a response exceeded its read bound.
	DoclingErrorResponseTooLarge DoclingErrorClass = "response_too_large"
	// DoclingErrorInvalidTaskID means a task identifier was unsafe for a path.
	DoclingErrorInvalidTaskID DoclingErrorClass = "invalid_task_id"
	// DoclingErrorAuthentication means Docling rejected the API key.
	DoclingErrorAuthentication DoclingErrorClass = "authentication"
	// DoclingErrorPolicy means a permanent admission or conversion-policy failure.
	DoclingErrorPolicy DoclingErrorClass = "policy"
	// DoclingErrorCapacity means the bounded Docling queue is full.
	DoclingErrorCapacity DoclingErrorClass = "capacity"
	// DoclingErrorSourceUnavailable means the admitted source was unavailable.
	DoclingErrorSourceUnavailable DoclingErrorClass = "source_unavailable"
	// DoclingErrorTargetUnavailable means the result target was unavailable.
	DoclingErrorTargetUnavailable DoclingErrorClass = "target_unavailable"
	// DoclingErrorTimeout means a request or conversion exhausted its deadline.
	DoclingErrorTimeout DoclingErrorClass = "timeout"
	// DoclingErrorInternal means Docling reported an internal failure.
	DoclingErrorInternal DoclingErrorClass = "internal"
	// DoclingErrorBackendFailure means a document backend failed.
	DoclingErrorBackendFailure DoclingErrorClass = "backend_failure"
	// DoclingErrorInferenceFailure means a local inference stage failed.
	DoclingErrorInferenceFailure DoclingErrorClass = "inference_failure"
	// DoclingErrorUnavailable means the private appliance is temporarily unavailable.
	DoclingErrorUnavailable DoclingErrorClass = "unavailable"
	// DoclingErrorProtocol means the pinned v1 wire contract was violated.
	DoclingErrorProtocol DoclingErrorClass = "protocol"
	// DoclingErrorPartialConversion accompanies a usable partial result.
	DoclingErrorPartialConversion DoclingErrorClass = "partial_conversion"
)

// DoclingError is deliberately content-free: Error never includes a URL,
// credential, filename, document content, or upstream diagnostic body.
type DoclingError struct {
	Operation  string
	Class      DoclingErrorClass
	Retryable  bool
	HTTPStatus int
}

// Error implements error without exposing request or response data.
func (e *DoclingError) Error() string {
	if e == nil {
		return "docling error"
	}
	return fmt.Sprintf("docling %s: %s", e.Operation, e.Class)
}

// DoclingFile is one bounded upload for the v1 hybrid chunk endpoint.
type DoclingFile struct {
	Name        string
	ContentType string
	Size        int64
	Reader      io.Reader
}

// DoclingTaskMeta is the non-sensitive task progress returned by Docling Serve.
type DoclingTaskMeta struct {
	NumDocs               int `json:"num_docs"`
	NumProcessed          int `json:"num_processed"`
	NumSucceeded          int `json:"num_succeeded"`
	NumPartiallySucceeded int `json:"num_partially_succeeded"`
	NumFailed             int `json:"num_failed"`
}

// DoclingFailure is the safe task-level failure classification.
type DoclingFailure struct {
	Category  DoclingErrorClass
	Retryable bool
}

// DoclingTaskStatus describes the asynchronous v1 task lifecycle.
type DoclingTaskStatus struct {
	TaskID       string
	TaskType     string
	Status       string
	TaskPosition *int
	Meta         *DoclingTaskMeta
	Failure      *DoclingFailure
}

// Terminal reports whether no further status transition is expected.
func (s DoclingTaskStatus) Terminal() bool {
	switch s.Status {
	case "success", "partial_success", "failure", "skipped":
		return true
	default:
		return false
	}
}

// DoclingChunk is a Docling hybrid chunk plus its source provenance.
type DoclingChunk struct {
	Filename    string          `json:"filename"`
	ChunkIndex  int             `json:"chunk_index"`
	Text        string          `json:"text"`
	RawText     *string         `json:"raw_text"`
	NumTokens   *int            `json:"num_tokens"`
	Headings    []string        `json:"headings"`
	Captions    []string        `json:"captions"`
	DocItems    []string        `json:"doc_items"`
	PageNumbers []int           `json:"page_numbers"`
	Metadata    json.RawMessage `json:"metadata"`
}

// DoclingDocumentError is a structured document/page conversion diagnostic.
type DoclingDocumentError struct {
	ComponentType string `json:"component_type"`
	ModuleName    string `json:"module_name"`
	ErrorMessage  string `json:"error_message"`
	Category      string `json:"category"`
	PageNumber    *int   `json:"page_no"`
}

// DoclingResult contains hybrid chunks and the requested converted Docling JSON.
type DoclingResult struct {
	Chunks         []DoclingChunk
	DocumentJSON   json.RawMessage
	DocumentStatus string
	DocumentErrors []DoclingDocumentError
	ProcessingTime float64
	Partial        bool
}
