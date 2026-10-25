package chatgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/llm"
)

const visionCatalog = `{"models":[{"slug":"vision","visibility":"list","supports_image":true}]}`

// describeFixture serves the account catalogue on GET /v1/models and hands POST /v1/responses
// to stream; responses counts the inference requests, which several cases require to be zero.
func describeFixture(t *testing.T, catalog string, stream func(w http.ResponseWriter, r *http.Request)) (*Client, *atomic.Int32) {
	t.Helper()
	var responses atomic.Int32
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			fmt.Fprint(w, catalog)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses":
			responses.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			stream(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	client.cfg.Model = "vision"
	return client, &responses
}

func describe(t *testing.T, client *Client, mimeType string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return client.Describe(ctx, []byte("png-bytes"), mimeType, "Read this.")
}

func TestDescribeSendsOneImageMessage(t *testing.T) {
	var body map[string]any
	var auth string
	client, _ := describeFixture(t, visionCatalog, func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		event(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"VERBALE "}`)
		event(w, "response.output_text.delta", `{"type":"response.output_text.delta","delta":"ITALIA-7391"}`)
		event(w, "response.completed", completed)
	})
	text, err := describe(t, client, "image/png")
	if err != nil || text != "VERBALE ITALIA-7391" {
		t.Fatalf("Describe = %q, %v", text, err)
	}
	if auth != "Bearer oauth-test-token" {
		t.Errorf("Authorization = %q", auth)
	}
	if body["model"] != "vision" || body["store"] != false {
		t.Errorf("model/store = %v/%v", body["model"], body["store"])
	}
	for _, absent := range []string{"tools", "reasoning", "instructions"} {
		if _, ok := body[absent]; ok {
			t.Errorf("request carries %q: %v", absent, body[absent])
		}
	}
	input, _ := body["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input = %#v, want one message", body["input"])
	}
	message, _ := input[0].(map[string]any)
	content, _ := message["content"].([]any)
	if message["role"] != "user" || len(content) != 2 {
		t.Fatalf("message = %#v", message)
	}
	text0, _ := content[0].(map[string]any)
	image, _ := content[1].(map[string]any)
	if text0["type"] != "input_text" || text0["text"] != "Read this." {
		t.Errorf("first part = %#v", text0)
	}
	if image["type"] != "input_image" || image["image_url"] != "data:image/png;base64,cG5nLWJ5dGVz" {
		t.Errorf("second part = %#v", image)
	}
}

func TestDescribeCannotReadWithAModelThatSeesNoImages(t *testing.T) {
	for name, catalog := range map[string]string{
		"text only": `{"models":[{"slug":"vision","visibility":"list","input_modalities":["text"]}]}`,
		"missing":   `{"models":[{"slug":"other","visibility":"list","supports_image":true}]}`,
		"hidden":    `{"models":[{"slug":"vision","visibility":"hide","supports_image":true}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			client, responses := describeFixture(t, catalog, func(http.ResponseWriter, *http.Request) {})
			_, err := describe(t, client, "image/png")
			if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
				t.Fatalf("Describe err = %v, want cannot read here", err)
			}
			if responses.Load() != 0 {
				t.Fatalf("%d inference requests for a model that reads no images", responses.Load())
			}
		})
	}
}

func TestDescribeCannotReadWithoutAConnectedAccount(t *testing.T) {
	for _, planErr := range []error{chatgptplan.ErrAuthorizationRequired, chatgptplan.ErrPlanDisabled} {
		t.Run(planErr.Error(), func(t *testing.T) {
			var requests atomic.Int32
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
			client.cfg.Model = "vision"
			client.tokens = tokenSource{err: planErr}
			_, err := describe(t, client, "image/png")
			if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
				t.Fatalf("Describe err = %v, want cannot read here", err)
			}
			if requests.Load() != 0 {
				t.Fatalf("%d requests without an account", requests.Load())
			}
		})
	}
}

func TestDescribeTreatsARefusalAsCannotRead(t *testing.T) {
	cases := map[string][]string{
		"refusal only": {
			`{"type":"response.refusal.delta","delta":"I can't help with that."}`,
		},
		"text, then refusal": {
			`{"type":"response.output_text.delta","delta":"partial"}`,
			`{"type":"response.refusal.delta","delta":"I can't help with that."}`,
		},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			client, _ := describeFixture(t, visionCatalog, func(w http.ResponseWriter, _ *http.Request) {
				for _, data := range events {
					event(w, "delta", data)
				}
				event(w, "response.completed", completed)
			})
			text, err := describe(t, client, "image/png")
			if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
				t.Fatalf("Describe = %q, %v, want cannot read here", text, err)
			}
			if text != "" || strings.Contains(err.Error(), "partial") {
				t.Fatalf("a refusal leaked text: %q, %v", text, err)
			}
		})
	}
}

func TestDescribeCannotReadANonImageType(t *testing.T) {
	client, responses := describeFixture(t, visionCatalog, func(http.ResponseWriter, *http.Request) {})
	_, err := describe(t, client, "application/octet-stream")
	if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
		t.Fatalf("Describe err = %v, want cannot read here", err)
	}
	if responses.Load() != 0 {
		t.Fatalf("%d inference requests for bytes that are not an image", responses.Load())
	}
}

func TestDescribeFailures(t *testing.T) {
	cases := []struct {
		name    string
		tokens  TokenSource
		catalog func(w http.ResponseWriter)
		stream  func(w http.ResponseWriter)
	}{
		{name: "no identity", tokens: tokenSource{err: chatgptplan.ErrIdentityRequired}},
		{name: "catalogue down", catalog: func(w http.ResponseWriter) { w.WriteHeader(500) }},
		{name: "rate limited", stream: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"oauth-test-token exhausted"}}`)
		}},
		{name: "plan limit", stream: func(w http.ResponseWriter) {
			event(w, "response.failed", `{"type":"response.failed","response":{"error":{"code":"usage_limit_reached","message":"plan exhausted"}}}`)
		}},
		{name: "cut short", stream: func(w http.ResponseWriter) {
			event(w, "delta", `{"type":"response.output_text.delta","delta":"partial"}`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/models" && tc.catalog != nil:
					tc.catalog(w)
				case r.URL.Path == "/v1/models":
					fmt.Fprint(w, visionCatalog)
				case tc.stream != nil:
					w.Header().Set("Content-Type", "text/event-stream")
					tc.stream(w)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
			})
			client.cfg.Model = "vision"
			if tc.tokens != nil {
				client.tokens = tc.tokens
			}
			_, err := describe(t, client, "image/png")
			if err == nil || errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
				t.Fatalf("Describe err = %v, want a failure that is retried", err)
			}
			if strings.Contains(err.Error(), "oauth-test-token") {
				t.Fatalf("error leaks the token: %v", err)
			}
		})
	}
}
