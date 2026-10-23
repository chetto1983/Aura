package embeddings

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/config"
)

// errOtherWidth is a model answering a width it was not asked for: a fact about the model, as a
// refusal is.
var errOtherWidth = errors.New("answers another width")

// AnswersAtWidth sends one short text the way a hosted route sends a passage, with the document
// prefix and `dimensions`, and errs unless the model answers with exactly width values. Measured
// 2026-10-20 (spikes/embedding-cloud-models): five OpenRouter models refuse the field with a 4xx,
// and the route preview, which asked only for the native width, let mistral-embed-2312 through.
func (c *Client) AnswersAtWidth(ctx context.Context, width int) error {
	payload := embeddingRequest{
		Input: []any{UntitledDocumentPrefix + probeTexts[0]}, Model: modelOrDefault(c.Model), Dimensions: width,
	}
	var decoded embeddingResponse
	if err := c.postJSON(ctx, endpoint(c.BaseURL), payload, &decoded); err != nil {
		return err
	}
	if len(decoded.Data) != 1 {
		return fmt.Errorf("endpoint returned %d embeddings for 1 input", len(decoded.Data))
	}
	if got := len(decoded.Data[0].Embedding); got != width {
		return fmt.Errorf("%w: %d values, not %d", errOtherWidth, got, width)
	}
	return nil
}

// compatibilityChecks bounds the requests in flight while a catalogue is checked.
const compatibilityChecks = 8

// Compatibility remembers which hosted models answer at Width, so the cockpit's list is checked
// once and again only on a refresh. A verdict about the model (it answers; it refuses the
// request; it answers another width; it is not served) is kept. A failure that says nothing
// about the model (a rate limit, a server error, the network) is not, and the model is asked
// again next time.
type Compatibility struct {
	Client *http.Client
	// CloudBaseURL is the route's cloud base; empty is OpenRouter, the same rule as the route.
	CloudBaseURL string
	Width        int
	Key          CredentialFunc

	mu    sync.Mutex
	known map[string]bool
}

// Check answers, for each model, whether it answers at Width. Without a key nothing is sent and
// the answer is ErrNoCredential; a key that cannot be read is that read's error.
func (c *Compatibility) Check(ctx context.Context, models []string, refresh bool) (map[string]bool, error) {
	key, err := c.Key(ctx)
	if err != nil {
		return nil, fmt.Errorf("embeddings: credential: %w", err)
	}
	if key = strings.TrimSpace(key); key == "" {
		return nil, ErrNoCredential
	}
	c.mu.Lock()
	if c.known == nil || refresh {
		c.known = make(map[string]bool, len(models))
	}
	pending := make([]string, 0, len(models))
	for _, model := range models {
		if _, ok := c.known[model]; !ok {
			pending = append(pending, model)
		}
	}
	c.mu.Unlock()

	answered := make(map[string]bool, len(pending))
	var answeredMu sync.Mutex
	slots := make(chan struct{}, compatibilityChecks)
	var wg sync.WaitGroup
	for _, model := range pending {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			err := c.client(model, key).AnswersAtWidth(ctx, c.Width)
			if !settled(err) {
				return
			}
			answeredMu.Lock()
			answered[model] = err == nil
			answeredMu.Unlock()
		})
	}
	wg.Wait()

	c.mu.Lock()
	defer c.mu.Unlock()
	maps.Copy(c.known, answered)
	out := make(map[string]bool, len(models))
	for _, model := range models {
		out[model] = c.known[model]
	}
	return out, nil
}

func (c *Compatibility) client(model, key string) *Client {
	base, credential, _ := config.ResolveEmbedRoute(config.EmbedConfig{CloudModel: model, CloudBaseURL: c.CloudBaseURL}, key)
	return &Client{BaseURL: base, Model: model, APIKey: credential, Client: c.Client}
}

// settled reports whether err, from AnswersAtWidth, is a verdict about the model.
func settled(err error) bool {
	if err == nil || errors.Is(err, errOtherWidth) || RejectsInput(err) {
		return true
	}
	var status *StatusError
	return errors.As(err, &status) && status.Code == http.StatusNotFound
}
