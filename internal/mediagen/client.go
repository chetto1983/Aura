package mediagen

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// Client executes image and video generation requests against a per-call
// OpenRouter base URL and identity API key. It carries no credential and no
// settings lookup of its own: the composition root resolves both, per call,
// before calling in.
type Client struct {
	http          *http.Client
	maxImageBytes int64
}

// NewClient never mutates httpClient: it stores a shallow copy (a nil
// argument copies a zero http.Client{}) whose CheckRedirect refuses any
// redirect whose scheme, host or port differs from the request that started
// the chain, so an identity's Authorization header never reaches another
// origin, subdomains included (ruling R13). Same-origin redirects keep the
// standard 10-redirect ceiling.
func NewClient(httpClient *http.Client, maxImageBytes int64) *Client {
	copied := http.Client{}
	if httpClient != nil {
		copied = *httpClient
	}
	copied.CheckRedirect = rejectCrossOriginRedirect
	return &Client{http: &copied, maxImageBytes: maxImageBytes}
}

func rejectCrossOriginRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("mediagen: stopped after 10 redirects")
	}
	origin := via[0].URL
	if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
		return fmt.Errorf("mediagen: refusing redirect from %s to a different origin", origin.Host)
	}
	return nil
}

// sdkClient builds the one SDK client this package uses (ruling R5), from
// explicit options only. openai.NewClient would first load OPENAI_API_KEY,
// OPENAI_ADMIN_KEY and custom headers from the environment and send them to
// the operator's base URL whenever no identity key overrides them; the SDK's
// opt-out marker is internal API (ruling R12). Only Options is populated, so
// callers use the generic Get/Post/Execute methods, or build a typed service
// from client.Options. Retries are off: a paid POST must never be sent twice.
func sdkClient(httpClient *http.Client, baseURL string, opts ...option.RequestOption) *openai.Client {
	return &openai.Client{Options: append([]option.RequestOption{
		option.WithBaseURL(strings.TrimRight(baseURL, "/") + "/"),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}, opts...)}
}

// classifyProviderError maps an OpenRouter HTTP error response to a coded
// mediagen.Error the caller can act on. Only the status codes OpenRouter's
// docs give a specific meaning get a specific code: neither the images nor
// the videos API reference documents a content-policy error shape (checked
// against openrouter.ai/docs/api/api-reference/images/generate-an-image and
// .../video-generation/*, 2026-09-14), so no case here returns
// content_blocked. Every other status becomes job_failed with the same
// redacted message.
// A transport-level failure (no HTTP response at all, e.g. a refused
// redirect or a dial error) passes through unclassified so ErrorCode's
// job_failed fallback applies without fabricating upstream text.
func classifyProviderError(err error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	code := "job_failed"
	switch apiErr.StatusCode {
	case http.StatusPaymentRequired:
		code = "no_credit"
	case http.StatusBadRequest:
		code = "model_rejected"
	}
	return &Error{Code: code, Message: redactedUpstreamMessage(apiErr)}
}

// paidCallError maps the failure of a request the provider charges for once it accepts it. An
// HTTP error response is the provider refusing it (classifyProviderError); anything else — no
// response, a dropped connection, a turn cancelled mid-request — leaves the outcome unknown.
func paidCallError(err error) error {
	if _, answered := errors.AsType[*openai.Error](err); answered {
		return classifyProviderError(err)
	}
	return outcomeUnknown(err)
}

// outcomeUnknown is a paid request whose result Aura could not learn or could not use: the
// provider may have accepted and billed it. Measured live on 2026-09-17: a video submit failed
// without a provider answer, the model saw only "Media generation failed." and sent the same
// request again.
func outcomeUnknown(cause error) error {
	return &Error{
		Code: "outcome_unknown",
		Message: "The provider gave no usable answer, so this request may have been accepted and billed. " +
			"Nothing was delivered. Do not submit it again: tell the operator what happened.",
		cause: cause,
	}
}

// redactedUpstreamMessage never returns apiErr.Error() or a Dump* method:
// both carry the full request (method, URL, and on DumpRequest(true) the
// body). Only the parsed "message" field is used, and it is bounded.
func redactedUpstreamMessage(apiErr *openai.Error) string {
	if msg := boundedMessage(apiErr.Message); msg != "" {
		return msg
	}
	return "The provider rejected this request."
}

// boundedMessage trims and caps upstream-provided text so a coded Error's
// Message never grows unbounded from provider-controlled input.
func boundedMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	const maxLen = 500
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "..."
	}
	return msg
}

// validProviderID rejects a provider job ID that isn't a single opaque path
// segment before it reaches path construction: empty, a dot-segment, or one
// carrying a path/query/fragment delimiter could otherwise redirect the
// request somewhere the provider never issued the job for.
func validProviderID(id string) (string, error) {
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#") {
		return "", fmt.Errorf("mediagen: invalid provider job id %q", id)
	}
	return id, nil
}

// ValidByteLimit rejects a maxBytes value that cannot bound anything: zero or
// negative accepts nothing, and math.MaxInt64 (a capped read looks one byte
// past maxBytes to see the limit crossed) would defeat the point of bounding at all. Every
// byte-bounded read in this package guards its limit with it, and NewWatcher
// panics on a limit it rejects, so the composition root checks the boot
// video ceiling with it before building a watcher.
func ValidByteLimit(maxBytes int64) error {
	if maxBytes <= 0 || maxBytes == math.MaxInt64 {
		return &Error{Code: "too_large", Message: "Invalid media byte limit."}
	}
	return nil
}

// readCapped reads a body bounded to maxBytes, detecting one that exceeds the
// limit without buffering it in full.
func readCapped(r io.Reader, maxBytes int64) ([]byte, error) {
	if err := ValidByteLimit(maxBytes); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(capped(io.NopCloser(r), maxBytes))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// cappedReader is http.MaxBytesReader's bound with this package's too_large
// refusal in place of *http.MaxBytesError, so a downloaded clip, a reference
// image and a restaged video all fail the same way past their limit. Callers
// check maxBytes with ValidByteLimit first.
type cappedReader struct{ io.ReadCloser }

func capped(rc io.ReadCloser, maxBytes int64) io.ReadCloser {
	return cappedReader{http.MaxBytesReader(nil, rc, maxBytes)}
}

func (c cappedReader) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if _, over := errors.AsType[*http.MaxBytesError](err); over {
		return n, mediaTooLarge()
	}
	return n, err
}

func mediaTooLarge() *Error {
	return &Error{Code: "too_large", Message: "Media exceeds the configured byte limit."}
}
