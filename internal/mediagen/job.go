package mediagen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/redact"
)

// Job is one durable video generation job, owned by IdentityID and scoped to the
// conversation that submitted it. ToolCallID is the submitting call; the call that
// delivers the clip is recorded on the asset instead. CostUSD is nil until the provider
// reports a cost, and an explicit zero stays a value.
type Job struct {
	ID             string
	IdentityID     string
	ConversationID string
	ToolCallID     string
	ProviderJobID  string
	Model          string
	Request        json.RawMessage
	Status         Status
	Error          *Error
	AssetID        string
	CostUSD        *float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
	DeliveredAt    *time.Time
}

// JobStore is the durable job port the watcher and the video tool consume. Every method is
// scoped to an owner: a job of another identity reads as missing (pgx.ErrNoRows).
type JobStore interface {
	Insert(ctx context.Context, job Job) (Job, error)
	Get(ctx context.Context, ownerID, jobID string) (Job, error)
	Recoverable(ctx context.Context, ownerID string) ([]Job, error)
	Progress(ctx context.Context, ownerID, jobID string, status Status, cost *float64, failure *Error) (Job, error)
	Complete(ctx context.Context, ownerID, jobID, assetID string, cost *float64) (Job, error)
	ClaimDelivery(ctx context.Context, ownerID, jobID, conversationID, deliveryCallID string) (Job, bool, error)
}

// ErrJobNotActive reports an update refused because the owned job already reached a
// terminal status: a finished job never moves again.
var ErrJobNotActive = errors.New("mediagen: video job is no longer active")

func (s Status) active() bool {
	return s == StatusPending || s == StatusInProgress
}

// JobAudit is the reserved "_aura" object of a persisted job request: what the provider body
// cannot carry. The reference asset IDs replace the image data the body sent, and Origin lets
// a resume refuse to poll a job at a base URL other than the one it was submitted to, without
// storing a key.
type JobAudit struct {
	Origin            string   `json:"origin"`
	FirstFrameAssetID string   `json:"first_frame_asset_id,omitempty"`
	ReferenceAssetIDs []string `json:"reference_asset_ids,omitempty"`
}

type jobRequest struct {
	VideoRequest
	Aura JobAudit `json:"_aura"`
}

// JobRequest is the request a job persists: req as it was submitted, minus its frame and
// reference image data, with credentials and data URLs redacted from the prompt, plus audit
// with its origin reduced to SubmissionOrigin. req itself is not modified, so the SDK body
// built from it is byte-identical and never carries _aura.
func JobRequest(req VideoRequest, audit JobAudit) (json.RawMessage, error) {
	origin, err := SubmissionOrigin(audit.Origin)
	if err != nil {
		return nil, err
	}
	audit.Origin = origin
	req.Prompt = redactPrompt(req.Prompt)
	req.FrameImages, req.InputReferences = nil, nil
	return json.Marshal(jobRequest{VideoRequest: req, Aura: audit})
}

// Audit reads back the _aura object JobRequest recorded.
func (j Job) Audit() (JobAudit, error) {
	var request struct {
		Aura *JobAudit `json:"_aura"`
	}
	if err := json.Unmarshal(j.Request, &request); err != nil {
		return JobAudit{}, fmt.Errorf("mediagen: decode job %s request: %w", j.ID, err)
	}
	if request.Aura == nil || request.Aura.Origin == "" {
		return JobAudit{}, fmt.Errorf("mediagen: job %s request records no submission origin", j.ID)
	}
	return *request.Aura, nil
}

// SubmissionOrigin is the form of a base URL a job records and a resume compares: scheme,
// host and path. Userinfo, query and fragment can carry a credential, so they are dropped,
// and the error never echoes the input for the same reason.
func SubmissionOrigin(baseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return "", errors.New("mediagen: the submission base URL is not an absolute http(s) URL")
	}
	origin := url.URL{Scheme: parsed.Scheme, Host: strings.ToLower(parsed.Host), Path: strings.TrimRight(parsed.Path, "/")}
	return origin.String(), nil
}

// promptToken is one whitespace-delimited run. Redaction replaces whole tokens and keeps the
// whitespace between them, so a redacted prompt keeps its line breaks.
var promptToken = regexp.MustCompile(`\S+`)

func redactPrompt(prompt string) string {
	return redact.String(promptToken.ReplaceAllStringFunc(prompt, func(token string) string {
		if isDataURL(token) || urlCarriesUserData(token) {
			return redact.Placeholder
		}
		return token
	}))
}

// isDataURL finds an RFC 2397 data URL in token: "data:" followed by the comma that ends its
// media type. Prose such as "data:" alone has no comma.
func isDataURL(token string) bool {
	_, payload, found := strings.Cut(strings.ToLower(token), "data:")
	return found && strings.Contains(payload, ",")
}

// urlCarriesUserData reports a URL in token whose userinfo, query or fragment can hold a
// credential or a signature. A URL that does not parse cannot be checked, so it counts.
func urlCarriesUserData(token string) bool {
	beforeSeparator, _, found := strings.Cut(token, "://")
	if !found {
		return false
	}
	start := strings.LastIndexFunc(beforeSeparator, func(r rune) bool { return !isSchemeRune(r) }) + 1
	parsed, err := url.Parse(strings.TrimRight(token[start:], `"')>]},;.`))
	return err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != ""
}

// isSchemeRune is RFC 3986's scheme alphabet: letters, digits, "+", "-" and ".".
func isSchemeRune(r rune) bool {
	return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("+-.", r)
}
