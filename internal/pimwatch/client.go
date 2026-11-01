package pimwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The two resources a watch reads.
const (
	ResourceMail     = "mail"
	ResourceCalendar = "calendar"
)

// Resources are watched in this order on each tick.
var Resources = []string{ResourceMail, ResourceCalendar}

// PIMServer is the name the PIM's OAuth grant is stored under, the one its /admin API takes too.
const PIMServer = "calendar"

const (
	// pimRequestTimeout is twice what the PIM says a page of 50 mails can take. A page that cannot
	// finish inside it is asked again unchanged, so too short a bound stops an account for good.
	pimRequestTimeout  = 60 * time.Second
	maxPIMResponseSize = 8 << 20
	// PageLimit is how many changes one call asks for. The PIM asks the provider once per sender
	// for writtenTo, so a smaller page keeps one request inside pimRequestTimeout.
	PageLimit = 50
	// MaxItemsPerCall is the contract's bound on POST /admin/items (spec §5.1).
	MaxItemsPerCall = 50
)

// FailureClass is how the watch treats a failed call (spec §5.6).
type FailureClass int

// A transient failure leaves the next tick to retry. A hard one needs the person. A resource the
// provider lacks is no failure at all.
const (
	FailureTransient FailureClass = iota
	FailureHard
	FailureUnsupported
)

// PIMError is a failed PIM call, named by a fixed code. It carries no response body and no URL,
// so it is safe in a log, a ledger and a cursor row.
type PIMError struct {
	Class FailureClass
	Code  string
}

func (e *PIMError) Error() string { return "pim: " + e.Code }

// ErrNotAuthorized says the identity's grant to the PIM is gone, as opposed to a token that could
// not be fetched right now. A TokenSource wraps it so that it is a hard failure.
var ErrNotAuthorized = errors.New("pim authorization required")

// TokenSource restores the identity's OAuth access token from the identity on ctx, the shape of
// agui.MCPAccessTokenProvider. A scheduled run has that identity from its task row.
type TokenSource interface {
	AccessToken(ctx context.Context, server string) (string, error)
}

// Client calls the PIM's token-gated /admin API. The token never reaches a log, and neither does a
// cursor: it travels in a POST body because it can hold a provider URL.
type Client struct {
	baseURL string
	tokens  TokenSource
	http    *http.Client
}

// NewClient builds the client, or nil when the PIM cannot be reached: no sidecar URL or no token
// source. Callers treat nil as "the watch cannot be enabled here".
func NewClient(baseURL string, tokens TokenSource) *Client {
	if strings.TrimSpace(baseURL) == "" || tokens == nil {
		return nil
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		tokens:  tokens,
		http:    &http.Client{Timeout: pimRequestTimeout},
	}
}

// EventCancelled is the status of a cancelled event. The PIM writes this or "confirmed".
const EventCancelled = "cancelled"

// EventTime is an event's start or end as the PIM writes it: an instant, or a bare date for an
// all-day event, which belongs to no zone. Null leaves it zero: a cancelled Google instance has no
// end. Anything else fails the page, loudly, as a contract the PIM broke.
type EventTime struct {
	time.Time
	AllDay bool
}

// UnmarshalJSON reads the two forms.
func (e *EventTime) UnmarshalJSON(raw []byte) error {
	var text *string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	if text == nil {
		return nil
	}
	if day, err := time.Parse(time.DateOnly, *text); err == nil {
		e.Time, e.AllDay = day, true
		return nil
	}
	at, err := time.Parse(time.RFC3339, *text)
	if err != nil {
		return err
	}
	e.Time = at
	return nil
}

// In is the moment this time means to a person in loc: an all-day event starts at their midnight.
func (e EventTime) In(loc *time.Location) time.Time {
	if !e.AllDay {
		return e.Time.In(loc)
	}
	y, m, d := e.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// Event is what the PIM says about a calendar change, or about an event read by id.
type Event struct {
	Start           EventTime `json:"start"`
	End             EventTime `json:"end"`
	Status          string    `json:"status"`
	OrganizerIsSelf bool      `json:"organizerIsSelf"`
	MyResponse      string    `json:"myResponse"`
	Fingerprint     string    `json:"fingerprint"`
}

// Change is one item in a page. From is the address alone, what a rule reads, and FromName the
// display name beside it. The snippet is not decoded: nothing here reads it until a classifier
// exists.
type Change struct {
	Type       ChangeType `json:"type"`
	ID         string     `json:"id"`
	CalendarID string     `json:"calendarId"`
	OccurredAt time.Time  `json:"occurredAt"`
	From       string     `json:"from"`
	FromName   string     `json:"fromName"`
	Subject    string     `json:"subject"`
	Signals    Signals    `json:"signals"`
	Event      *Event     `json:"event"`
}

// ChangesPage answers POST /admin/changes (spec §5.1).
type ChangesPage struct {
	Changes       []Change `json:"changes"`
	NextCursor    string   `json:"nextCursor"`
	HasMore       bool     `json:"hasMore"`
	ResetRequired bool     `json:"resetRequired"`
}

// Changes reads one page after cursor. An empty cursor starts from now and returns no changes. A
// provider failure is an error, never an empty page.
func (c *Client) Changes(ctx context.Context, accountID, resource, cursor string, limit int) (ChangesPage, error) {
	var page ChangesPage
	body := map[string]any{"accountId": accountID, "resource": resource, "cursor": cursor, "limit": limit}
	if err := c.post(ctx, "/admin/changes", body, &page); err != nil {
		return ChangesPage{}, err
	}
	if page.NextCursor == "" {
		// An empty cursor means "start from now", so following one would lose whatever arrived
		// since this page. The contract always gives one.
		return ChangesPage{}, &PIMError{Class: FailureTransient, Code: "pim_empty_cursor"}
	}
	return page, nil
}

// ItemRef names one item to read.
type ItemRef struct {
	ID         string `json:"id"`
	CalendarID string `json:"calendarId,omitempty"`
}

// Item is what a read by id gives. Nothing here stores it. An event's time is in Event: OccurredAt
// is when it was last modified.
type Item struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendarId"`
	From       string    `json:"from"`
	FromName   string    `json:"fromName"`
	Subject    string    `json:"subject"`
	OccurredAt time.Time `json:"occurredAt"`
	Event      *Event    `json:"event"`
}

// ItemsResult answers POST /admin/items. Gone holds what no longer exists, so a deleted mail shows
// as gone instead of failing the read.
type ItemsResult struct {
	Items []Item    `json:"items"`
	Gone  []ItemRef `json:"gone"`
}

// Items reads items by id, calling the PIM as many times as the contract's bound requires.
func (c *Client) Items(ctx context.Context, accountID, resource string, refs []ItemRef) (ItemsResult, error) {
	var all ItemsResult
	for start := 0; start < len(refs); start += MaxItemsPerCall {
		batch := refs[start:min(start+MaxItemsPerCall, len(refs))]
		var part ItemsResult
		body := map[string]any{"accountId": accountID, "resource": resource, "items": batch}
		if err := c.post(ctx, "/admin/items", body, &part); err != nil {
			return ItemsResult{}, err
		}
		all.Items = append(all.Items, part.Items...)
		all.Gone = append(all.Gone, part.Gone...)
	}
	return all, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	token, err := c.tokens.AccessToken(ctx, PIMServer)
	if errors.Is(err, ErrNotAuthorized) {
		return &PIMError{Class: FailureHard, Code: "pim_not_authorized"}
	}
	if err != nil {
		return &PIMError{Class: FailureTransient, Code: "pim_token_unavailable"}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode pim request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build pim request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	// The host is the configured sidecar; the transport's own error names it and the URL, so
	// only a fixed code leaves this function.
	resp, err := c.http.Do(req) //nolint:gosec // G704: fixed sidecar host.
	if err != nil {
		return &PIMError{Class: FailureTransient, Code: "pim_unreachable"}
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPIMResponseSize))
	if err != nil {
		return &PIMError{Class: FailureTransient, Code: "pim_read_failed"}
	}
	if resp.StatusCode != http.StatusOK {
		return classifyStatus(resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &PIMError{Class: FailureTransient, Code: "pim_bad_response"}
	}
	return nil
}

// classifyStatus reads a failure's named code (spec §5.1 and the PIM's own refusals) and falls back
// on the status class. Only a code from this list survives: the body's text is the PIM's and may
// name a provider URL. A 500 is what the PIM answers for a bug or an exception it did not
// recognise, which the next tick may not repeat, and the hour's alarm covers one that stays.
func classifyStatus(status int, raw []byte) *PIMError {
	var named struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &named)
	switch named.Error {
	case "provider_transient":
		return &PIMError{Class: FailureTransient, Code: named.Error}
	case "provider_auth", "permission_denied", "account_not_found",
		"invalid_cursor", "invalid_account", "invalid_resource", "invalid_items", "too_many_items":
		return &PIMError{Class: FailureHard, Code: named.Error}
	case "unsupported_resource":
		return &PIMError{Class: FailureUnsupported, Code: named.Error}
	}
	if status >= http.StatusInternalServerError || status == http.StatusTooManyRequests {
		return &PIMError{Class: FailureTransient, Code: fmt.Sprintf("pim_http_%d", status)}
	}
	return &PIMError{Class: FailureHard, Code: fmt.Sprintf("pim_http_%d", status)}
}

// failureClass classifies an error from the client. Anything else, a context ended mid-call
// included, is transient: the next tick is the retry.
func failureClass(err error) (FailureClass, string) {
	if pimErr, ok := errors.AsType[*PIMError](err); ok {
		return pimErr.Class, pimErr.Code
	}
	return FailureTransient, "pim_call_failed"
}
