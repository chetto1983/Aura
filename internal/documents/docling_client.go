package documents

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/chetto1983/aura/internal/config"
)

var doclingTaskIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// DoclingStandardConversionProfile is the exact option set sent to Docling Serve, and it
// is folded into the conversion stage fingerprint rather than merely documented: changing
// any flag here changes the extracted text, so cached conversions must not be reused
// across a change to this string.
const DoclingStandardConversionProfile = "standard-v1:ocr=true:tables=true:abort=false:image=placeholder:heading=false:enrichment=false:hybrid-merge=true:raw-text=true:markdown-tables=true"

// DoclingClientOption customizes the Docling v1 boundary.
type DoclingClientOption func(*DoclingClient)

// WithDoclingHTTPClient injects an HTTP client, primarily for isolated tests.
// Production callers should retain the private-address transport.
func WithDoclingHTTPClient(client *http.Client) DoclingClientOption {
	return func(docling *DoclingClient) {
		if client != nil {
			docling.httpClient = client
		}
	}
}

// DoclingClient implements Aura's sole Docling Serve v1.29.0 API profile.
type DoclingClient struct {
	baseURL        *url.URL
	apiKey         string
	httpClient     *http.Client
	overallTimeout time.Duration
	requestTimeout time.Duration
	maxRequest     int64
	chunkTokens    int
	tokenizer      string
	pollPeriod     time.Duration
}

// NewDoclingClient builds a fail-closed client for the pinned Docling appliance.
func NewDoclingClient(cfg config.DocumentConfig, options ...DoclingClientOption) (*DoclingClient, error) {
	if strings.TrimSpace(cfg.DoclingBaseURL) == "" || strings.TrimSpace(cfg.DoclingAPIKey) == "" {
		return nil, &DoclingError{Operation: "configure", Class: DoclingErrorConfig}
	}
	if err := cfg.Validate(); err != nil {
		return nil, &DoclingError{Operation: "configure", Class: DoclingErrorConfig}
	}
	baseURL, err := url.Parse(cfg.DoclingBaseURL)
	if err != nil {
		return nil, &DoclingError{Operation: "configure", Class: DoclingErrorConfig}
	}
	client := &DoclingClient{
		baseURL:        baseURL,
		apiKey:         cfg.DoclingAPIKey,
		httpClient:     newPrivateDoclingHTTPClient(nil),
		overallTimeout: cfg.ConvertTimeout,
		requestTimeout: min(cfg.ConvertTimeout, doclingRequestTimeout),
		maxRequest:     cfg.MaxRequestBytes,
		chunkTokens:    cfg.ChunkMaxTokens,
		tokenizer:      cfg.ChunkTokenizer,
		pollPeriod:     doclingDefaultPollPeriod,
	}
	for _, option := range options {
		if option != nil {
			option(client)
		}
	}
	return client, nil
}

// ChunkFile submits, polls, and retrieves one hybrid-chunked document.
func (c *DoclingClient) ChunkFile(ctx context.Context, file DoclingFile) (DoclingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.overallTimeout)
	defer cancel()
	task, err := c.SubmitFile(ctx, file)
	if err != nil {
		return DoclingResult{}, err
	}
	status, err := c.Wait(ctx, task.TaskID)
	if err != nil {
		return DoclingResult{}, err
	}
	result, err := c.Result(ctx, task.TaskID)
	if err != nil {
		return DoclingResult{}, err
	}
	if status.Status == "partial_success" || result.Partial {
		result.Partial = true
		return result, &DoclingError{Operation: "convert", Class: DoclingErrorPartialConversion}
	}
	return result, nil
}

// SubmitFile starts one asynchronous hybrid chunking task.
func (c *DoclingClient) SubmitFile(ctx context.Context, file DoclingFile) (DoclingTaskStatus, error) {
	filename, err := c.validateFile(file)
	if err != nil {
		return DoclingTaskStatus{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	reader, writer := io.Pipe()
	multipartWriter := multipart.NewWriter(writer)
	request, err := c.newRequest(requestCtx, http.MethodPost, "/v1/chunk/hybrid/file/async", reader)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return DoclingTaskStatus{}, err
	}
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	writeResult := make(chan error, 1)
	go func() {
		writeErr := c.writeMultipart(multipartWriter, file, filename)
		_ = writer.CloseWithError(writeErr)
		writeResult <- writeErr
	}()

	response, requestErr := c.httpClient.Do(request)
	_ = reader.Close()
	writeErr := <-writeResult
	if response != nil {
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode/100 != 2 {
			return DoclingTaskStatus{}, doclingHTTPError("submit", response.StatusCode)
		}
	}
	if writeErr != nil {
		return DoclingTaskStatus{}, writeErr
	}
	if requestErr != nil {
		return DoclingTaskStatus{}, classifyDoclingRequestError("submit", requestErr)
	}
	if response == nil {
		return DoclingTaskStatus{}, &DoclingError{Operation: "submit", Class: DoclingErrorProtocol}
	}
	var wire doclingTaskStatusWire
	if err := decodeDoclingJSON(response, doclingStatusBodyLimit, &wire); err != nil {
		return DoclingTaskStatus{}, err
	}
	return validateDoclingTaskStatus("submit", "", wire)
}

// Poll reads one asynchronous task status without server-side long polling.
func (c *DoclingClient) Poll(ctx context.Context, taskID string) (DoclingTaskStatus, error) {
	if !doclingTaskIDPattern.MatchString(taskID) {
		return DoclingTaskStatus{}, &DoclingError{Operation: "poll", Class: DoclingErrorInvalidTaskID}
	}
	query := url.Values{"wait": []string{"0"}}
	var wire doclingTaskStatusWire
	if err := c.getJSON(ctx, "/v1/status/poll/"+taskID+"?"+query.Encode(), doclingStatusBodyLimit, &wire); err != nil {
		return DoclingTaskStatus{}, err
	}
	return validateDoclingTaskStatus("poll", taskID, wire)
}

// Wait polls until the task reaches a terminal state or its bounded budget ends.
func (c *DoclingClient) Wait(ctx context.Context, taskID string) (DoclingTaskStatus, error) {
	maxPolls := int(c.overallTimeout/c.pollPeriod) + 2
	for range maxPolls {
		status, err := c.Poll(ctx, taskID)
		if err != nil {
			return DoclingTaskStatus{}, err
		}
		if status.Terminal() {
			switch status.Status {
			case "success", "partial_success":
				return status, nil
			case "failure":
				return status, failureError("poll", status.Failure)
			default:
				return status, &DoclingError{Operation: "poll", Class: DoclingErrorPolicy}
			}
		}
		timer := time.NewTimer(c.pollPeriod)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return DoclingTaskStatus{}, classifyDoclingRequestError("poll", ctx.Err())
		case <-timer.C:
		}
	}
	return DoclingTaskStatus{}, &DoclingError{Operation: "poll", Class: DoclingErrorTimeout, Retryable: true}
}

// Result fetches a completed task result exactly once.
func (c *DoclingClient) Result(ctx context.Context, taskID string) (DoclingResult, error) {
	if !doclingTaskIDPattern.MatchString(taskID) {
		return DoclingResult{}, &DoclingError{Operation: "result", Class: DoclingErrorInvalidTaskID}
	}
	var wire doclingResultWire
	if err := c.getJSON(ctx, "/v1/result/"+taskID, doclingResultBodyLimit, &wire); err != nil {
		return DoclingResult{}, err
	}
	if wire.Kind == "TaskFailureResult" {
		return DoclingResult{}, failureError("result", safeFailure(wire.Failure))
	}
	if wire.Kind != "" || wire.Failure != nil || len(wire.Documents) != 1 {
		return DoclingResult{}, &DoclingError{Operation: "result", Class: DoclingErrorProtocol}
	}
	document := wire.Documents[0]
	jsonContent := strings.TrimSpace(string(document.Content.JSONContent))
	if document.Kind != "ExportResult" || (document.Status != "success" && document.Status != "partial_success") ||
		jsonContent == "" || jsonContent == "null" || jsonContent[0] != '{' {
		return DoclingResult{}, &DoclingError{Operation: "result", Class: DoclingErrorProtocol}
	}
	for _, chunk := range wire.Chunks {
		if chunk.ChunkIndex < 0 || chunk.Filename == "" {
			return DoclingResult{}, &DoclingError{Operation: "result", Class: DoclingErrorProtocol}
		}
	}
	return DoclingResult{
		Chunks:         wire.Chunks,
		DocumentJSON:   append(json.RawMessage(nil), document.Content.JSONContent...),
		DocumentStatus: document.Status,
		DocumentErrors: document.Errors,
		ProcessingTime: wire.ProcessingTime,
		Partial:        document.Status == "partial_success",
	}, nil
}

func (c *DoclingClient) validateFile(file DoclingFile) (string, error) {
	if file.Reader == nil || file.Size <= 0 {
		return "", &DoclingError{Operation: "submit", Class: DoclingErrorPolicy}
	}
	if file.Size > c.maxRequest {
		return "", &DoclingError{Operation: "submit", Class: DoclingErrorRequestTooLarge}
	}
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(file.Name), "\\", "/"))
	if name == "" || name == "." || name == "/" {
		return "", &DoclingError{Operation: "submit", Class: DoclingErrorPolicy}
	}
	for _, character := range name {
		if unicode.IsControl(character) {
			return "", &DoclingError{Operation: "submit", Class: DoclingErrorPolicy}
		}
	}
	return name, nil
}

func (c *DoclingClient) writeMultipart(writer *multipart.Writer, file DoclingFile, filename string) error {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name": "files", "filename": filename,
	}))
	header.Set("Content-Type", contentTypeOrDefault(file.ContentType))
	part, err := writer.CreatePart(header)
	if err != nil {
		return &DoclingError{Operation: "submit", Class: DoclingErrorUnavailable, Retryable: true}
	}
	limited := &io.LimitedReader{R: file.Reader, N: file.Size + 1}
	written, err := io.Copy(part, limited)
	if err != nil {
		return &DoclingError{Operation: "submit", Class: DoclingErrorUnavailable, Retryable: true}
	}
	if written > file.Size {
		return &DoclingError{Operation: "submit", Class: DoclingErrorRequestTooLarge}
	}
	if written != file.Size {
		return &DoclingError{Operation: "submit", Class: DoclingErrorPolicy}
	}
	for _, field := range c.multipartFields() {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return &DoclingError{Operation: "submit", Class: DoclingErrorUnavailable, Retryable: true}
		}
	}
	if err := writer.Close(); err != nil {
		return &DoclingError{Operation: "submit", Class: DoclingErrorUnavailable, Retryable: true}
	}
	return nil
}

func (c *DoclingClient) multipartFields() [][2]string {
	return [][2]string{
		{"include_converted_doc", "true"},
		{"target_type", "inbody"},
		{"convert_pipeline", "standard"},
		{"convert_document_timeout", positiveSeconds(c.overallTimeout)},
		{"convert_abort_on_error", "false"},
		{"convert_image_export_mode", "placeholder"},
		{"convert_do_ocr", "true"},
		{"convert_do_table_structure", "true"},
		{"convert_do_pdf_heading_hierarchy", "false"},
		{"convert_include_images", "false"},
		{"convert_include_page_images", "false"},
		{"convert_do_code_enrichment", "false"},
		{"convert_do_formula_enrichment", "false"},
		{"convert_do_picture_classification", "false"},
		{"convert_do_chart_extraction", "false"},
		{"convert_do_picture_description", "false"},
		{"chunking_max_tokens", strconv.Itoa(c.chunkTokens)},
		{"chunking_tokenizer", c.tokenizer},
		{"chunking_merge_peers", "true"},
		{"chunking_include_raw_text", "true"},
		{"chunking_use_markdown_tables", "true"},
	}
}

func (c *DoclingClient) newRequest(ctx context.Context, method, pathValue string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.baseURL.String(), "/")+pathValue, body)
	if err != nil {
		return nil, &DoclingError{Operation: "request", Class: DoclingErrorConfig}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", c.apiKey)
	return request, nil
}

func (c *DoclingClient) getJSON(ctx context.Context, pathValue string, limit int64, out any) error {
	requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(
		requestCtx, http.MethodGet, strings.TrimRight(c.baseURL.String(), "/")+pathValue, nil,
	)
	if err != nil {
		return &DoclingError{Operation: "request", Class: DoclingErrorConfig}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Api-Key", c.apiKey)
	response, err := c.httpClient.Do(request)
	if err != nil {
		return classifyDoclingRequestError("request", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		return doclingHTTPError("request", response.StatusCode)
	}
	return decodeDoclingJSON(response, limit, out)
}

type doclingFailureWire struct {
	Category  string            `json:"category"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Phase     string            `json:"phase"`
	Details   map[string]string `json:"details"`
}

type doclingTaskStatusWire struct {
	TaskID       string              `json:"task_id"`
	TaskType     string              `json:"task_type"`
	TaskStatus   string              `json:"task_status"`
	TaskPosition *int                `json:"task_position"`
	TaskMeta     *DoclingTaskMeta    `json:"task_meta"`
	ErrorMessage *string             `json:"error_message"`
	Failure      *doclingFailureWire `json:"failure"`
}

type doclingExportContentWire struct {
	Filename        string          `json:"filename"`
	MarkdownContent *string         `json:"md_content"`
	JSONContent     json.RawMessage `json:"json_content"`
	HTMLContent     *string         `json:"html_content"`
	TextContent     *string         `json:"text_content"`
	DocTagsContent  *string         `json:"doctags_content"`
	DocLangContent  *string         `json:"doclang_content"`
}

type doclingExportWire struct {
	Kind       string                   `json:"kind"`
	Content    doclingExportContentWire `json:"content"`
	Status     string                   `json:"status"`
	Errors     []DoclingDocumentError   `json:"errors"`
	Timings    json.RawMessage          `json:"timings"`
	Confidence json.RawMessage          `json:"confidence"`
}

type doclingResultWire struct {
	Kind           string              `json:"kind"`
	Failure        *doclingFailureWire `json:"failure"`
	Chunks         []DoclingChunk      `json:"chunks"`
	Documents      []doclingExportWire `json:"documents"`
	ProcessingTime float64             `json:"processing_time"`
}

func validateDoclingTaskStatus(operation, expectedTaskID string, wire doclingTaskStatusWire) (DoclingTaskStatus, error) {
	if !doclingTaskIDPattern.MatchString(wire.TaskID) ||
		(expectedTaskID != "" && wire.TaskID != expectedTaskID) || wire.TaskType != "chunk" {
		return DoclingTaskStatus{}, &DoclingError{Operation: operation, Class: DoclingErrorProtocol}
	}
	switch wire.TaskStatus {
	case "pending", "started", "failure", "success", "partial_success", "skipped":
	default:
		return DoclingTaskStatus{}, &DoclingError{Operation: operation, Class: DoclingErrorProtocol}
	}
	if (wire.TaskStatus == "failure") != (wire.Failure != nil) ||
		(wire.TaskPosition != nil && *wire.TaskPosition < 0) || !validDoclingTaskMeta(wire.TaskMeta) {
		return DoclingTaskStatus{}, &DoclingError{Operation: operation, Class: DoclingErrorProtocol}
	}
	return DoclingTaskStatus{
		TaskID: wire.TaskID, TaskType: wire.TaskType, Status: wire.TaskStatus,
		TaskPosition: wire.TaskPosition, Meta: wire.TaskMeta, Failure: safeFailure(wire.Failure),
	}, nil
}

func validDoclingTaskMeta(meta *DoclingTaskMeta) bool {
	if meta == nil {
		return true
	}
	return meta.NumDocs >= 0 && meta.NumProcessed >= 0 && meta.NumSucceeded >= 0 &&
		meta.NumPartiallySucceeded >= 0 && meta.NumFailed >= 0 &&
		meta.NumProcessed <= meta.NumDocs &&
		meta.NumSucceeded+meta.NumPartiallySucceeded+meta.NumFailed <= meta.NumProcessed
}

func safeFailure(failure *doclingFailureWire) *DoclingFailure {
	if failure == nil {
		return nil
	}
	return &DoclingFailure{Category: DoclingErrorClass(failure.Category), Retryable: failure.Retryable}
}

func failureError(operation string, failure *DoclingFailure) error {
	if failure == nil {
		return &DoclingError{Operation: operation, Class: DoclingErrorInternal, Retryable: true}
	}
	class := failure.Category
	switch class {
	case DoclingErrorPolicy, DoclingErrorCapacity, DoclingErrorSourceUnavailable,
		DoclingErrorTargetUnavailable, DoclingErrorTimeout, DoclingErrorInternal,
		DoclingErrorBackendFailure, DoclingErrorInferenceFailure:
	default:
		class = DoclingErrorInternal
	}
	retryable := failure.Retryable
	if class == DoclingErrorPolicy {
		retryable = false
	}
	return &DoclingError{Operation: operation, Class: class, Retryable: retryable}
}
