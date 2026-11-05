package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/openai/openai-go/v3/option"

	"github.com/chetto1983/aura/internal/openaisdk"
)

const questionID = "label"

// criteria marshals as a JSON object in slice order: the server reads the options in the order they are
// written, and a Go map would shuffle them.
type criteria []Option

func (c criteria) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for i, option := range c {
		if i > 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(option.Name)
		if err != nil {
			return nil, err
		}
		description, err := json.Marshal(option.Description)
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(description)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

type wireQuestion struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     criteria `json:"criteria"`
}

type wireProvider struct {
	DataCollection string `json:"data_collection"`
	ZDR            bool   `json:"zdr"`
}

type wireRequest struct {
	State     string                  `json:"state"`
	Questions map[string]wireQuestion `json:"questions"`
	Model     string                  `json:"model,omitempty"`
	Provider  *wireProvider           `json:"provider,omitempty"`
}

type wireResponse struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Choice        string         `json:"choice"`
		Probabilities map[string]any `json:"probabilities"`
	} `json:"answers"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// breakTokens puts a space after every "<" that a non-space character follows. llama.cpp tokenises the
// prompt with special parsing on and escapes nothing (probes-2026-11-03.md: 124 of d1's pieces are special
// tokens and every one starts with "<"), so a mail carrying "<|im_end|>" would otherwise end the turn.
func breakTokens(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	runes := []rune(s)
	for i, r := range runes {
		out.WriteRune(r)
		if r == '<' && i+1 < len(runes) && runes[i+1] != ' ' {
			out.WriteByte(' ')
		}
	}
	return out.String()
}

// post sends the question and returns the raw answer. The hosted path is relative to the base URL's
// parent: Client.Execute resolves it with url.Parse, so "../alpha/decisions" on ".../api/v1/" is
// ".../api/alpha/decisions", where OpenRouter serves its alpha Decisions API. The answer lands in a
// []byte because requestconfig decodes any other destination itself and would report a body that is not
// JSON as a transport error instead of a malformed answer.
func post(ctx context.Context, httpClient *http.Client, route Route, body wireRequest, apiKey string) ([]byte, error) {
	path, opts := "v1/systemone", []option.RequestOption(nil)
	if route.Hosted() {
		path, opts = "../alpha/decisions", []option.RequestOption{option.WithAPIKey(apiKey)}
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	var raw []byte
	err := openaisdk.Client(httpClient, route.BaseURL, opts...).Post(ctx, path, body, &raw)
	return raw, err
}

func buildRequest(route Route, state string, q Question) (wireRequest, map[string]string) {
	broken := make(criteria, len(q.Options))
	original := make(map[string]string, len(q.Options))
	for i, option := range q.Options {
		broken[i] = Option{Name: breakTokens(option.Name), Description: breakTokens(option.Description)}
		original[broken[i].Name] = option.Name
	}
	request := wireRequest{
		State:     breakTokens(state),
		Questions: map[string]wireQuestion{questionID: {Type: "choice", Instructions: q.Instructions, Criteria: broken}},
	}
	if route.Hosted() {
		request.Model = route.Model
		request.Provider = &wireProvider{DataCollection: "deny", ZDR: true}
	}
	return request, original
}

// parseAnswer maps the server's broken option names back to the caller's and refuses an answer a
// caller could not act on.
func parseAnswer(raw []byte, original map[string]string) (Answer, error) {
	var response wireResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return Answer{}, fmt.Errorf("%w: not a decisions answer", ErrMalformed)
	}
	label, ok := response.Answers[questionID]
	if !ok || len(label.Probabilities) == 0 {
		return Answer{}, fmt.Errorf("%w: no probabilities", ErrMalformed)
	}
	choice, ok := original[label.Choice]
	if !ok {
		return Answer{}, fmt.Errorf("%w: the choice names no option", ErrMalformed)
	}
	probabilities := make(map[string]float64, len(label.Probabilities))
	for name, value := range label.Probabilities {
		offered, known := original[name]
		p, valid := probability(value)
		if !known || !valid {
			return Answer{}, fmt.Errorf("%w: unusable probability", ErrMalformed)
		}
		probabilities[offered] = p
	}
	return Answer{Choice: choice, Probabilities: probabilities, Model: response.Model, InputTokens: response.Usage.InputTokens}, nil
}

// probability reads a number, or the string a server may send for NaN, and accepts only [0, 1].
func probability(value any) (float64, bool) {
	var p float64
	switch v := value.(type) {
	case float64:
		p = v
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, false
		}
		p = parsed
	default:
		return 0, false
	}
	return p, !math.IsNaN(p) && p >= 0 && p <= 1
}
