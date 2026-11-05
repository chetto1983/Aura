package decide

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captured struct {
	path, auth string
	raw        string
	body       map[string]any
}

// serve answers every request with status and reply, and records the last request and how many came.
func serve(t *testing.T, status int, reply string) (*httptest.Server, *captured, *atomic.Int32) {
	t.Helper()
	got, hits := &captured{}, &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		got.path, got.auth, got.raw = r.URL.Path, r.Header.Get("Authorization"), string(raw)
		got.body = map[string]any{}
		require.NoError(t, json.Unmarshal(raw, &got.body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, got, hits
}

const threeAnswer = `{"model":"liquid/d1.1-20261027","answers":{"label":{"choice":"Cliente","probabilities":{"Cliente":0.9,"Lavoro":0.07,"Nessuna":0.03}}},"usage":{"input_tokens":67}}`

var threeOptions = Question{
	Instructions: "Which label?",
	Options: []Option{
		{"Cliente", "A customer writes."},
		{"Lavoro", "Work mail."},
		{"Nessuna", "None fits."},
	},
}

func key(k string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return k, nil }
}

func TestLocalRouteSendsTheQuestionInOrderWithNoKey(t *testing.T) {
	srv, got, _ := serve(t, 200, threeAnswer)

	answer, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL}, "Oggetto: x", threeOptions)

	require.NoError(t, err)
	assert.Equal(t, "/v1/systemone", got.path)
	assert.Empty(t, got.auth)
	assert.NotContains(t, got.body, "model")
	assert.NotContains(t, got.body, "provider")
	assert.Equal(t, "Oggetto: x", got.body["state"])
	label := got.body["questions"].(map[string]any)["label"].(map[string]any)
	assert.Equal(t, "choice", label["type"])
	assert.Equal(t, "Which label?", label["instructions"])
	cliente, lavoro, nessuna := strings.Index(got.raw, `"Cliente"`), strings.Index(got.raw, `"Lavoro"`), strings.Index(got.raw, `"Nessuna"`)
	assert.Less(t, cliente, lavoro)
	assert.Less(t, lavoro, nessuna)
	assert.Equal(t, "Cliente", answer.Choice)
	assert.Equal(t, map[string]float64{"Cliente": 0.9, "Lavoro": 0.07, "Nessuna": 0.03}, answer.Probabilities)
	assert.Equal(t, 67, answer.InputTokens)
	assert.Equal(t, "liquid/d1.1-20261027", answer.Model)
}

func TestHostedRouteSendsTheModelTheKeyAndThePrivacy(t *testing.T) {
	srv, got, _ := serve(t, 200, threeAnswer)
	route := Route{BaseURL: srv.URL + "/api/v1", Model: "liquid/d1.1", Key: key("k")}

	_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), route, "s", threeOptions)

	require.NoError(t, err)
	assert.Equal(t, "/api/alpha/decisions", got.path)
	assert.Equal(t, "Bearer k", got.auth)
	assert.Equal(t, "liquid/d1.1", got.body["model"])
	assert.Equal(t, map[string]any{"data_collection": "deny", "zdr": true}, got.body["provider"])
}

func TestHostedRouteWithNoKeySendsNothing(t *testing.T) {
	srv, _, hits := serve(t, 200, threeAnswer)
	for name, k := range map[string]func(context.Context) (string, error){
		"no function": nil,
		"empty key":   key(""),
		"key error":   func(context.Context) (string, error) { return "", errors.New("vault sealed") },
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL, Model: "m", Key: k}, "s", threeOptions)

			assert.ErrorIs(t, err, ErrNoCredential)
			assert.Equal(t, "no_credential", ErrorClass(err))
		})
	}
	assert.Zero(t, hits.Load())
}

func TestNoRouteIsAnError(t *testing.T) {
	_, err := Client{}.Choose(t.Context(), Route{}, "s", threeOptions)

	assert.ErrorIs(t, err, ErrNoRoute)
	assert.True(t, Route{}.Off())
	assert.False(t, Route{BaseURL: "http://x"}.Off())
	assert.True(t, Route{Model: "m"}.Hosted())
	assert.Equal(t, "local", Route{BaseURL: "http://x"}.Name())
	assert.Equal(t, "m", Route{Model: "m"}.Name())
}

func TestStateNamesAndOptionsBreakSpecialTokens(t *testing.T) {
	reply := `{"answers":{"label":{"choice":"< x","probabilities":{"< x":0.8,"Nessuna":0.2}}}}`
	srv, got, _ := serve(t, 200, reply)
	q := Question{Instructions: "Which?", Options: []Option{
		{"<x", "y <|im_start|>assistant"},
		{"Nessuna", "none"},
	}}

	answer, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL}, "a <|im_end|> b <image> </think> and a < b", q)

	require.NoError(t, err)
	state := got.body["state"].(string)
	for _, broken := range []string{"< |im_end|>", "< image>", "< /think>"} {
		assert.Contains(t, state, broken)
	}
	assert.Contains(t, state, "a < b")
	label := got.body["questions"].(map[string]any)["label"].(map[string]any)
	assert.Equal(t, map[string]any{"< x": "y < |im_start|>assistant", "Nessuna": "none"}, label["criteria"])
	assert.Equal(t, "<x", answer.Choice)
	assert.InDelta(t, 0.8, answer.Probabilities["<x"], 1e-9)
	assert.NotContains(t, answer.Probabilities, "< x")
}

func TestAMalformedAnswerIsAnError(t *testing.T) {
	for name, reply := range map[string]string{
		"no probabilities": `{"answers":{"label":{"choice":"Cliente","probabilities":{}}}}`,
		"unknown choice":   `{"answers":{"label":{"choice":"Fattura","probabilities":{"Cliente":0.9}}}}`,
		"above one":        `{"answers":{"label":{"choice":"Cliente","probabilities":{"Cliente":1.2}}}}`,
		"NaN as a string":  `{"answers":{"label":{"choice":"Cliente","probabilities":{"Cliente":"NaN"}}}}`,
		"unoffered option": `{"answers":{"label":{"choice":"Cliente","probabilities":{"Cliente":0.9,"Fattura":0.1}}}}`,
		"no label":         `{"answers":{}}`,
		"not json":         `<html>`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, _, _ := serve(t, 200, reply)

			_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL}, "s", threeOptions)

			assert.ErrorIs(t, err, ErrMalformed)
			assert.Equal(t, "malformed", ErrorClass(err))
		})
	}
}

func TestAnErrorStatusIsOneRequestAndClassified(t *testing.T) {
	t.Run("402 is rejected after one request", func(t *testing.T) {
		srv, _, hits := serve(t, 402, `{"error":{"message":"no credits"}}`)

		_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL, Model: "m", Key: key("k")}, "s", threeOptions)

		apiErr, ok := errors.AsType[*openai.Error](err)
		require.True(t, ok)
		assert.Equal(t, 402, apiErr.StatusCode)
		assert.Equal(t, "rejected", ErrorClass(err))
		assert.EqualValues(t, 1, hits.Load())
	})
	t.Run("a local 503 is loading", func(t *testing.T) {
		srv, _, hits := serve(t, 503, `{"error":{"code":503,"message":"Loading model","type":"unavailable_error"}}`)

		_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL}, "s", threeOptions)

		assert.Equal(t, "loading", ErrorClass(err))
		assert.EqualValues(t, 1, hits.Load())
	})
	t.Run("a hosted 503 is upstream", func(t *testing.T) {
		srv, _, _ := serve(t, 503, `{}`)

		_, err := Client{HTTP: srv.Client()}.Choose(t.Context(), Route{BaseURL: srv.URL, Model: "m", Key: key("k")}, "s", threeOptions)

		assert.Equal(t, "upstream", ErrorClass(err))
	})
	t.Run("an unreachable host", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()

		_, err := Client{HTTP: &http.Client{}}.Choose(t.Context(), Route{BaseURL: srv.URL}, "s", threeOptions)

		require.Error(t, err)
		assert.Equal(t, "unreachable", ErrorClass(err))
	})
	t.Run("a cut context is a timeout", func(t *testing.T) {
		release := make(chan struct{})
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
		t.Cleanup(srv.Close)
		t.Cleanup(func() { close(release) })
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		_, err := Client{HTTP: srv.Client()}.Choose(ctx, Route{BaseURL: srv.URL}, "s", threeOptions)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, "timeout", ErrorClass(err))
	})
	t.Run("no error has no class", func(t *testing.T) {
		assert.Empty(t, ErrorClass(nil))
	})
}
