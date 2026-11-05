// Package decide asks a decision model one choice question about a state, on llama.cpp's /v1/systemone
// (b11960 tools/server/README.md) or OpenRouter's Decisions API (api-reference/alphadecisions). It knows no
// domain. openai-go's typed Client.Decisions is OpenAI's own shape, so the request goes through client.Post.
package decide

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	openai "github.com/openai/openai-go/v3"
)

// Option is one answer the model may choose, with the text it reads to tell the options apart.
type Option struct{ Name, Description string }

// Question is what the model is asked about a state.
type Question struct {
	Instructions string
	Options      []Option // sent in this order
}

// Answer is the model's choice, with the probability it gave every option, keyed by the caller's names.
type Answer struct {
	Choice        string // the original name of the option chosen
	Probabilities map[string]float64
	Model         string
	InputTokens   int
}

// Route says where a question goes and how it is paid for.
type Route struct {
	BaseURL string
	Model   string // "" is the local sidecar at BaseURL
	Key     func(context.Context) (string, error)
}

// Off reports a route with no base and no model.
func (r Route) Off() bool { return r.BaseURL == "" && r.Model == "" }

// Hosted reports a route that names a model, which is OpenRouter's.
func (r Route) Hosted() bool { return r.Model != "" }

// Name is the model, or "local".
func (r Route) Name() string {
	if r.Hosted() {
		return r.Model
	}
	return "local"
}

var (
	// ErrNoRoute is a question put to a route that is off or has no base URL.
	ErrNoRoute = errors.New("decide: no route")
	// ErrNoCredential is a hosted route whose key is missing, empty or unreadable. Nothing was sent.
	ErrNoCredential = errors.New("decide: no credential for the hosted route")
	// ErrMalformed is an answer to act on nothing: no probabilities, a choice or probability that names
	// no offered option, or a probability outside [0, 1].
	ErrMalformed = errors.New("decide: malformed answer")

	errLoading = errors.New("decide: the local model is loading")
)

// Client sends questions on HTTP, or on http.DefaultClient when it is nil.
type Client struct{ HTTP *http.Client }

// Choose asks q about state. A passed deadline returns an error satisfying context.DeadlineExceeded.
func (c Client) Choose(ctx context.Context, route Route, state string, q Question) (Answer, error) {
	if route.Off() || route.BaseURL == "" {
		return Answer{}, ErrNoRoute
	}
	apiKey := ""
	if route.Hosted() {
		var err error
		if route.Key != nil {
			apiKey, err = route.Key(ctx)
		}
		if err != nil || apiKey == "" {
			return Answer{}, ErrNoCredential
		}
	}
	request, original := buildRequest(route, state, q)
	raw, err := post(ctx, c.HTTP, route, request, apiKey)
	if err != nil {
		if apiErr, ok := errors.AsType[*openai.Error](err); ok && !route.Hosted() && apiErr.StatusCode == http.StatusServiceUnavailable {
			return Answer{}, fmt.Errorf("%w (%w)", errLoading, err)
		}
		return Answer{}, err
	}
	return parseAnswer(raw, original)
}

// ErrorClass names an error for a person, with no text from the answer:
// "no_credential", "unreachable", "loading" (local 503), "rejected" (4xx), "upstream" (5xx), "malformed", "timeout".
func ErrorClass(err error) string {
	if err == nil {
		return ""
	}
	if apiErr, ok := errors.AsType[*openai.Error](err); ok {
		switch {
		case errors.Is(err, errLoading):
			return "loading"
		case apiErr.StatusCode >= 400 && apiErr.StatusCode < 500:
			return "rejected"
		default:
			return "upstream"
		}
	}
	switch {
	case errors.Is(err, ErrNoCredential):
		return "no_credential"
	case errors.Is(err, ErrMalformed):
		return "malformed"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "timeout"
	default:
		return "unreachable"
	}
}
